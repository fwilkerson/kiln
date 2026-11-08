// Package kiln builds the specs a developer approved, and lands them on main.
//
//	r, err := kiln.Open(ctx, ".", os.Environ())
//	err = r.Init(ctx, os.Stdout)          // kiln.json, and the pre-commit hook
//	err = r.Approve(ctx, "x", os.Stdout)  // queues specs/x.md, as main holds it
//	err = r.Run(ctx, os.Stdout)           // builds each queued spec, one at a time
//	err = r.Status(ctx, os.Stdout)
//	err = r.Stats(ctx, "", os.Stdout)     // what each build cost, one line each; "x" shows each build of x in full
//	err = r.Accept(ctx, "x", os.Stdout)   // ends UAT, and retires the spec
//
// A build runs one build session of Claude Code, on its own branch, kiln/<name>, in a worktree kiln makes. kiln commits
// what the session leaves, then gates it. A failing gate goes back to the same session, at most kiln.json's rounds
// times; past that, the build stops. Once the gate passes, the audit agent reads the changed packages, in a session
// of its own. An audit that ends with no result runs again in a new session, up to 3 tries. Its findings go back to
// the build session. A build with no amendment then lands. It is rebased onto main as one commit, and gated
// again. Then main moves to it, only if main has not moved since. A build that amended its spec waits on its branch
// instead.
//
// The gate runs kiln.json's commands, read from main's tip, at the build's commit and at main. It passes when every
// command exits 0, but for Go tests that fail at main too. It fails on any path the work changed under specs/ but
// specs/<name>.amend.md, however it was written. A session's hook refuses an Edit or a Write there before it happens.
//
// A usage limit pauses the build until the limit resets, and then resumes the same session. A limit with no reset
// pauses it for 5 minutes.
//
// Each approval is a new build, with its own ID. kiln's ledger records every build, session run and gate, and how
// each build ended. A build's time is its session runs' time plus its gates'. A pause for a usage limit adds nothing.
// Its turns, cost and failed calls sum its runs. A number kiln could not read shows as unknown, never as zero. A
// landed build's commit holds its time, cost, turns and failed calls.
//
// kiln keeps each spec's state and the ledger in kiln/ under the git common directory, so every worktree shares
// them. The worktrees are in the user's cache directory, under kiln/. Main is the branch named main.
package kiln

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/proc"
)

const (
	mainBranch = "main"
	mainTree   = ".main"
	product    = "PRODUCT.md"
)

// Repo is one repository kiln builds, opened from any of its checkouts.
type Repo struct {
	top    string
	common string
	git    *git.Git
	store  store
	trees  string
	env    []string
	exe    string
	claude claude.Claude

	cachesOnce sync.Once
	caches     []string
}

// Open opens the repository that holds dir. env is the environment of every process kiln starts, less what each
// withholds.
func Open(ctx context.Context, dir string, env []string) (*Repo, error) {
	top, err := plainGit(ctx, dir, proc.Git.Env(env), "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s is in no git repository: %w", dir, err)
	}

	g, err := git.New(top, env)
	if err != nil {
		return nil, err
	}

	if err := g.Check(ctx); err != nil {
		return nil, err
	}

	common, err := g.CommonDir(ctx)
	if err != nil {
		return nil, err
	}

	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}

	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}

	kept := filepath.Join(common, "kiln")
	sum := sha256.Sum256([]byte(common))
	return &Repo{
		top:    top,
		common: common,
		git:    g,
		store:  store{dir: kept},
		trees:  filepath.Join(cache, "kiln", fmt.Sprintf("%s-%x", filepath.Base(top), sum[:4])),
		env:    env,
		exe:    exe,
		claude: claude.New(claude.Config{Program: "claude", Env: env, Sessions: filepath.Join(kept, "sessions")}),
	}, nil
}

// hookMark marks a pre-commit hook as kiln's own, which kiln init may write over.
const hookMark = "# kiln's pre-commit hook"

// Init writes a starting kiln.json at the top of the checkout, unless one is there. It installs the pre-commit hook,
// which refuses a commit to main that fails kiln.json's commands. It refuses to write over another pre-commit hook.
func (r *Repo) Init(ctx context.Context, out io.Writer) error {
	configPath := filepath.Join(r.top, configFile)
	if _, err := os.Stat(configPath); errors.Is(err, fs.ErrNotExist) {
		raw, err := jsonIndent(initial)
		if err != nil {
			return err
		}

		if err := os.WriteFile(configPath, raw, 0o644); err != nil {
			return err
		}

		fmt.Fprintf(out, "wrote %s: commit it, since kiln reads it from main\n", configFile)
	}

	args := []string{"rev-parse", "--path-format=absolute", "--git-path", "hooks"}
	hooks, err := plainGit(ctx, r.top, proc.Git.Env(r.env), args...)
	if err != nil {
		return err
	}

	hook := filepath.Join(hooks, "pre-commit")
	if old, err := os.ReadFile(hook); err == nil && !bytes.Contains(old, []byte(hookMark)) {
		return fmt.Errorf("%w: %s is another pre-commit hook; call %s precommit from it", errRefused, hook, r.exe)
	}

	if err := os.MkdirAll(hooks, 0o755); err != nil {
		return err
	}

	script := fmt.Sprintf("#!/bin/sh\n%s\nexec %s precommit\n", hookMark, shellQuote(r.exe))
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		return err
	}

	fmt.Fprintf(out, "installed %s\n", hook)
	return nil
}

var specName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Approve records main's tip as the commit of the approved spec, specs/<name>.md, and queues a new build of it. A
// build of an earlier approval that waits, landed or on its branch, is dropped: its branch never reaches main.
func (r *Repo) Approve(ctx context.Context, name string, out io.Writer) error {
	if !specName.MatchString(name) {
		return fmt.Errorf("%w: %q is not a spec's name, such as checkout-flow", errRefused, name)
	}

	main, err := r.git.Tip(ctx, mainBranch)
	if err != nil {
		return err
	}

	if _, found, err := r.git.Show(ctx, main, specPath(name)); err != nil {
		return err
	} else if !found {
		return fmt.Errorf("%w: main holds no %s: commit it there first", errRefused, specPath(name))
	}

	var drop bool
	var dropped string
	err = r.store.update(func(b *book) error {
		drop, dropped, err = b.approve(name, main)
		return err
	})
	if err != nil {
		return err
	}

	if dropped != "" {
		if err := r.store.write(entry{Kind: outcomeKind, Build: dropped, Outcome: outDropped}); err != nil {
			fmt.Fprintf(out, "%s: kiln could not write its ledger: %v\n", name, err)
		}
	}

	if drop {
		if err := r.git.Remove(ctx, r.tree(name), branchOf(name)); err != nil {
			return err
		}
	}

	fmt.Fprintf(out, "%s: %s, approved at %s\n", name, queued, short(main))
	return nil
}

// Status shows each spec kiln holds, and where it stands.
func (r *Repo) Status(_ context.Context, out io.Writer) error {
	b, err := r.store.read()
	if err != nil {
		return err
	}

	if len(b.Specs) == 0 {
		fmt.Fprintln(out, "kiln holds no approved spec")
	}

	for _, s := range b.Specs {
		fmt.Fprintf(out, "%s: %s", s.Name, s.State)
		switch {
		case s.State == building && !s.Paused.IsZero():
			fmt.Fprintf(out, ", paused for a usage limit until %s", s.Paused.Local().Format("15:04:05 on Jan 2"))
		case s.State == amended:
			fmt.Fprintf(out, ", %s", branchOf(s.Name))
		}

		fmt.Fprintln(out)
		if len(s.FailingAtMain) > 0 {
			fmt.Fprintf(out, "  failing at main before the build: %s\n", strings.Join(s.FailingAtMain, ", "))
		}

		for _, text := range []string{s.Why, s.Amendments} {
			if text != "" {
				fmt.Fprintln(out, indent(text))
			}
		}
	}

	return nil
}

// Accept ends the developer's acceptance of a spec. The spec is deleted from main, and PRODUCT.md gains a line that
// names the commit that last held it. The commit that moves main is gated first, on main as it stands.
//
// A spec waiting on its branch first folds its amendments into the spec, and lands as a build does. A conflict or a
// failing gate goes back to its build's session. While another kiln run builds, that run lands it instead.
func (r *Repo) Accept(ctx context.Context, name string, out io.Writer) error {
	var rec record
	err := r.store.update(func(b *book) error {
		found, err := b.accept(name)
		if err == nil {
			rec = *found
		}

		return err
	})
	if err != nil {
		return err
	}

	b := &build{r: r, rec: rec, tree: r.tree(name), out: out}
	if rec.State == amended {
		return r.acceptAmended(ctx, b)
	}

	if err := b.checkoutMain(ctx); err != nil {
		return err
	}

	landed, report, err := b.land(ctx, "", b.retire, specPath(name), amendPath(name))
	if err == nil && report != "" {
		err = fmt.Errorf("%w: the gate failed on main as it stands, so %s is not accepted:\n%s", errRefused, name,
			report)
	}

	if err != nil {
		return err
	}

	if err := r.git.Remove(ctx, b.tree, branchOf(name)); err != nil {
		return err
	}

	if err := r.store.update(func(b *book) error { b.retire(name); return nil }); err != nil {
		return err
	}

	b.record(entry{Kind: outcomeKind, Outcome: outAccepted})
	fmt.Fprintf(out, "%s: accepted, and main is at %s\n", name, short(landed))
	return nil
}

// acceptAmended folds the amendments of b's spec into the spec, on its branch, and queues its landing.
func (r *Repo) acceptAmended(ctx context.Context, b *build) error {
	name := b.rec.Name
	tip, err := r.git.Head(ctx, b.tree)
	if err != nil {
		return err
	}

	if _, err := b.foldAmendments(ctx); err != nil {
		return errors.Join(err, r.git.Reset(ctx, b.tree, tip))
	}

	err = r.store.set(name, func(s *record) { s.State, s.Accepted, s.Rounds, s.Amendments = queued, true, 0, "" })
	if err != nil {
		return err
	}

	err = r.run(ctx, b.out, name)
	if errors.Is(err, errRunning) {
		fmt.Fprintf(b.out, "%s: accepted; the kiln run that is building lands it\n", name)
		return nil
	}

	return err
}

func (r *Repo) tree(name string) string { return filepath.Join(r.trees, name) }

func branchOf(name string) string { return "kiln/" + name }

func short(commit string) string { return commit[:min(12, len(commit))] }

func indent(text string) string {
	return "  " + strings.ReplaceAll(strings.TrimRight(text, "\n"), "\n", "\n  ")
}

// goCaches are Go's build and module caches, which a session's commands write.
func (r *Repo) goCaches(ctx context.Context) []string {
	r.cachesOnce.Do(func() {
		cmd := exec.CommandContext(ctx, "go", "env", "GOCACHE", "GOMODCACHE")
		cmd.Env = r.env
		if out, err := cmd.Output(); err == nil {
			r.caches = strings.Fields(string(out))
		}
	})

	return r.caches
}

// newSession returns a new session ID, a random UUID.
func newSession() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// shellQuote quotes s for /bin/sh, with which Claude Code and git run a hook's command.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-+,:@%=") == "" {
		return s
	}

	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
