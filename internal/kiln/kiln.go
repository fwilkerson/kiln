// Package kiln builds the specs a developer approved, and lands them on main.
//
//	r, err := kiln.Open(ctx, ".", os.Environ())
//	err = r.Init(ctx, os.Stdout)          // kiln.json, and the pre-commit hook
//	err = r.Queue(ctx, "x", os.Stdout)    // queues specs/x.md, as main holds it
//	err = r.Run(ctx, os.Stdout)           // builds each queued spec, one at a time
//	err = r.Status(ctx, os.Stdout)
//	err = r.TUI(ctx, "", os.Stdin, os.Stdout)  // every build, live and past, in a full-screen view
//	err = r.Accept(ctx, "x", os.Stdout)   // takes the work: lands its amendments, and retires the spec
//	err = r.Reject(ctx, "x", os.Stdout)   // turns the amendments away: drops the build, and its branch
//	names, err := r.Takes(ctx, "queue")   // the specs kiln queue would not refuse
//
// Where each command moves a spec:
//
//	(no record) ─queue─▶ queued ─run─▶ building
//	                                   ├─ lands ──▶ waiting for UAT
//	                                   │            ├─ accept ──▶ retired
//	                                   │            └─ queue, once its spec on main changed ──▶ queued
//	                                   ├─ stops ──▶ stopped
//	                                   │            └─ queue ──▶ queued
//	                                   └─ amends ─▶ waiting on its branch with amendments
//	                                                ├─ reject ──▶ (no record)
//	                                                └─ accept ──▶ landing accepted amendments
//	                                                              ├─ lands ──▶ retired
//	                                                              └─ stops ──▶ landing accepted amendments, stopped
//	                                                                           ├─ reject ──▶ (no record)
//	                                                                           └─ accept ──▶ landing again
//
// A spec landing accepted amendments is queued, building or stopped, as the build that lands it stands. Every other
// command refuses. kiln queue takes only a spec whose Status, at main's tip, is Approved.
//
// A build runs one build session of Claude Code, on its own branch, kiln/<name>, in a worktree kiln makes. kiln commits
// what the session leaves, then gates it. A failing gate goes back to the same session, at most kiln.json's rounds
// times; past that, the build stops. Once the gate passes, the audit agent reads the changed packages, in a session
// of its own. It runs in a worktree of its own at the build's commit, so nothing it writes reaches the build. An audit
// that ends with no result runs again in a new session, up to 3 tries. Its findings go back to the build session. A
// build with no amendment then lands. It is rebased onto main as one commit, and gated again. Then main moves to it,
// only if main has not moved since. A build that amended its spec waits on its branch instead. A landing that doesn't
// move main leaves the branch as it stood before the landing began. Work that changes nothing on main never lands. It
// goes back to the session, as a failed gate does.
//
// The gate runs kiln.json's commands, read from main's tip, at the build's commit and at main. It passes when every
// command exits 0, but for Go tests that fail at main too. It fails on any path the work changed under specs/ but
// specs/<name>.amend.md, however it was written. A session's hook refuses an Edit or a Write there before it happens.
//
// A usage limit pauses the build until the limit resets, and then resumes the same session. A limit with no reset
// pauses it for 5 minutes.
//
// Each queue is a new build, with its own ID. kiln's ledger records every build, session run and gate, and how
// each build ended. A build's time is its session runs' time plus its gates'. A pause for a usage limit adds nothing.
// Its turns, cost and failed calls sum its runs. A number kiln could not read shows as unknown, never as zero. A
// landed build's commit holds its time, cost, turns and failed calls.
//
// A build that ends waiting on the developer notifies them, in kiln run's output or kiln accept's. It ends stopped,
// waiting for UAT, or waiting on its branch. kiln prints an OSC 9 sequence, which a terminal such as Ghostty shows as
// a notification. Its body is "kiln: <name> <state>", with the state as kiln status words it. kiln prints it whether
// or not the output is a terminal. A summary of the build follows, from kiln's record, its ledger and git:
//
//	x: stopped: the gate still failed after its last round:
//	  12 min · $3.40 · 41 turns · 2 failed calls
//	  3 of 3 rounds · the audit had not run
//	  failing: TestAT2 (example.com/scratch)
//	  the work is on kiln/x, in /Users/me/Library/Caches/kiln/scratch-1f2e3d4c/x
//	  next: cd /Users/me/Library/Caches/kiln/scratch-1f2e3d4c/x && claude --resume <session>, to ask the session why;
//	        then fix main or the spec, and run kiln queue x
//
// A landed build shows the files its commit changed, and a build on its branch those it changed since main. That
// build shows its amendments too. The last line is the next command, never one the spec's state refuses.
//
// kiln keeps each spec's state and the ledger in kiln/ under the git common directory, so every worktree shares
// them. The worktrees are in the user's cache directory, under kiln/. So is build.lock, which every kiln that drives a
// build session shares, and [HoldBuilds] takes alone. Main is the branch named main.
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
	"syscall"

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

	cache, err := cacheDir()
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
		trees:  filepath.Join(cache, fmt.Sprintf("%s-%x", filepath.Base(top), sum[:4])),
		env:    env,
		exe:    exe,
		claude: claude.New(claude.Config{Program: "claude", Env: env, Sessions: filepath.Join(kept, "sessions")}),
	}, nil
}

// cacheDir is kiln's directory under the user's cache directory.
func cacheDir() (string, error) {
	cache, err := os.UserCacheDir()
	return filepath.Join(cache, "kiln"), err
}

// HoldBuilds keeps every kiln on the machine from starting to drive a build session, until release. It refuses while
// one drives one: a kiln run, or a kiln accept that lands. A build's session runs kiln hook by its path, so the
// reinstall holds it while it swaps the binary.
func HoldBuilds() (release func(), err error) {
	release, err = lockBuilds(syscall.LOCK_EX | syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return nil, fmt.Errorf("%w: a kiln drives a build session, in this repository or another; reinstall once "+
			"it ends", errRefused)
	}

	return release, err
}

// shareBuilds takes build.lock shared, as every kiln that drives a build session does. The reinstall then swaps no
// binary under one. It refuses while the reinstall holds it.
func shareBuilds() (release func(), err error) {
	release, err = lockBuilds(syscall.LOCK_SH | syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return nil, fmt.Errorf("%w: kiln is being reinstalled; run it again once the reinstall ends", errRefused)
	}

	return release, err
}

// lockBuilds takes build.lock, in kiln's directory under the user's cache directory, as how says.
func lockBuilds(how int) (release func(), err error) {
	dir, err := cacheDir()
	if err != nil {
		return nil, err
	}

	return store{dir: dir}.lock(buildLock, how)
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

var (
	specName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	approved = regexp.MustCompile(`(?m)^Status: Approved\b`)
)

// Queue records main's tip as the commit of specs/<name>.md, and queues a new build of it. It refuses a name that is
// not a spec's, and a spec main does not hold. It refuses one whose Status is not Approved. It refuses by the state
// the spec is in, as the package doc shows. A stopped build, or one waiting for UAT, is dropped for the new one.
func (r *Repo) Queue(ctx context.Context, name string, out io.Writer) error {
	main, err := r.git.Tip(ctx, mainBranch)
	if err != nil {
		return err
	}

	spec, err := r.approvedSpec(ctx, main, name)
	if err != nil {
		return err
	}

	var drop bool
	var dropped string
	err = r.store.update(func(b *book) error {
		drop, dropped, err = b.queue(name, main, r.changed(ctx, name, spec))
		return err
	})
	if err != nil {
		return err
	}

	if dropped != "" {
		r.dropped(name, dropped, out)
	}

	if drop {
		if err := r.git.Remove(ctx, r.tree(name), branchOf(name)); err != nil {
			return err
		}
	}

	fmt.Fprintf(out, "%s: %s, as main holds it at %s\n", name, queued, short(main))
	return nil
}

// approvedSpec reads specs/<name>.md at main, a spec whose Status is Approved.
func (r *Repo) approvedSpec(ctx context.Context, main, name string) ([]byte, error) {
	if !specName.MatchString(name) {
		return nil, fmt.Errorf("%w: %q is not a spec's name, such as checkout-flow", errRefused, name)
	}

	spec, found, err := r.git.Show(ctx, main, specPath(name))
	switch {
	case err != nil:
		return nil, err
	case !found:
		return nil, fmt.Errorf("%w: main holds no %s: commit it there first", errRefused, specPath(name))
	case !approved.Match(spec):
		return nil, fmt.Errorf("%w: %s is not Approved: finish it with /slice, and commit it, first", errRefused,
			specPath(name))
	}

	return spec, nil
}

// changed answers whether specs/<name>.md at a commit differs from spec.
func (r *Repo) changed(ctx context.Context, name string, spec []byte) func(since string) (bool, error) {
	return func(since string) (bool, error) {
		old, _, err := r.git.Show(ctx, since, specPath(name))
		return !bytes.Equal(old, spec), err
	}
}

// dropped writes to the ledger that the build of name ended, dropped.
func (r *Repo) dropped(name, build string, out io.Writer) {
	if err := r.store.write(entry{Kind: outcomeKind, Build: build, Outcome: outDropped}); err != nil {
		fmt.Fprintf(out, "%s: kiln could not write its ledger: %v\n", name, err)
	}
}

// Reject turns away the amendments of a spec waiting on its branch, or of its stopped landing. It drops the build, its
// worktree and its branch, and kiln holds no record of the spec. Main is as it was.
func (r *Repo) Reject(ctx context.Context, name string, out io.Writer) error {
	var rec record
	err := r.store.update(func(b *book) (err error) {
		rec, err = b.reject(name)
		return err
	})
	if err != nil {
		return err
	}

	r.dropped(name, rec.Build, out)
	if err := r.git.Remove(ctx, r.tree(name), branchOf(name)); err != nil {
		return err
	}

	fmt.Fprintf(out, "%s: rejected, and %s is gone\n", name, branchOf(name))
	return nil
}

// Takes answers the specs command would not refuse, for the shell to complete. For tui, they are the specs it opens
// on: each kiln holds a record of, or its ledger holds. Other commands take none.
func (r *Repo) Takes(ctx context.Context, command string) ([]string, error) {
	var names []string
	switch command {
	case "tui":
		specs, err := view{r}.Specs(ctx)
		for _, s := range specs {
			names = append(names, s.Name)
		}

		return names, err
	case acceptCommand, rejectCommand:
		b, err := r.store.read()
		for _, s := range b.Specs {
			if b.may(command, s.Name, nil) == nil {
				names = append(names, s.Name)
			}
		}

		return names, err
	case queueCommand:
		return r.queueable(ctx)
	}

	return nil, nil
}

// queueable answers the specs on main that kiln queue would not refuse.
func (r *Repo) queueable(ctx context.Context) ([]string, error) {
	b, err := r.store.read()
	if err != nil {
		return nil, err
	}

	main, err := r.git.Tip(ctx, mainBranch)
	if err != nil {
		return nil, err
	}

	files, err := r.git.Files(ctx, main, "specs")
	if err != nil {
		return nil, err
	}

	var names []string
	for _, f := range files {
		name, ok := strings.CutSuffix(f, ".md")
		if !ok || !specName.MatchString(name) {
			continue
		}

		spec, err := r.approvedSpec(ctx, main, name)
		if err == nil {
			err = b.may(queueCommand, name, r.changed(ctx, name, spec))
		}

		switch {
		case err == nil:
			names = append(names, name)
		case !errors.Is(err, errRefused):
			return nil, err
		}
	}

	return names, nil
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
		fmt.Fprintf(out, "%s: %s", s.Name, shownState(s))
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

// Accept takes the developer's work on a spec. The spec is deleted from main, and PRODUCT.md gains a line that names
// the commit that last held it. The commit that moves main is gated first, on main as it stands.
//
// A spec waiting on its branch first folds its amendments into the spec, and lands as a build does. A conflict or a
// failing gate goes back to its build's session. While another kiln run builds, that run lands it instead. A stopped
// landing lands again afresh, its rounds at 0, with no second fold. An accepted landing that stops fails Accept.
func (r *Repo) Accept(ctx context.Context, name string, out io.Writer) error {
	// Accept takes the lock before it changes anything, so a reinstall that refuses it leaves the spec as it was.
	unshare, err := shareBuilds()
	if err != nil {
		return err
	}

	defer unshare()
	var rec record
	err = r.store.update(func(b *book) (err error) {
		rec, err = b.accept(name)
		return err
	})
	if err != nil {
		return err
	}

	b := &build{r: r, rec: rec, tree: r.tree(name), out: out}
	switch {
	case rec.Accepted:
		return r.landAccepted(ctx, b)
	case rec.State == amended:
		return r.acceptAmended(ctx, b)
	}

	if err := b.checkoutMain(ctx); err != nil {
		return err
	}

	landed, back, err := b.land(ctx, "", b.retire, specPath(name), amendPath(name))
	if err == nil && back.report != "" {
		err = fmt.Errorf("%w: the gate failed on main as it stands, so %s is not accepted:\n%s", errRefused, name,
			back.report)
	}

	// Before the landing, the spec had no branch: its build's was gone once it landed.
	if err != nil {
		return errors.Join(err, r.git.Remove(context.WithoutCancel(ctx), b.tree, branchOf(name)))
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

// acceptAmended folds the amendments of b's spec into the spec, on its branch, and lands it.
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

	return r.landAccepted(ctx, b)
}

// landAccepted lands the queued landing of b's accepted spec, unless another kiln run builds, which lands it then.
func (r *Repo) landAccepted(ctx context.Context, b *build) error {
	name := b.rec.Name
	err := r.run(ctx, b.out, name)
	if errors.Is(err, errRunning) {
		fmt.Fprintf(b.out, "%s: accepted; the kiln run that is building lands it\n", name)
		return nil
	}

	if err != nil {
		return err
	}

	k, err := r.store.read()
	if s := k.find(name); err == nil && s != nil && s.State == stopped {
		err = fmt.Errorf("the landing of %s stopped: kiln accept %s lands it again, and kiln reject %s drops it", name,
			name, name)
	}

	return err
}

func (r *Repo) tree(name string) string { return filepath.Join(r.trees, name) }

// auditTree is the worktree the audit of name's build reads, detached at the build's commit. A spec's name never
// starts with a dot, so it is no build's tree.
func (r *Repo) auditTree(name string) string { return filepath.Join(r.trees, ".audit-"+name) }

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
