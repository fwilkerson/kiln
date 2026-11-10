package kiln

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/proc"
)

// config is kiln.json, read from main's tip, so a build cannot weaken the gate it is held to.
type config struct {
	// Gate holds the commands the gate runs, each with sh -c, at the top of a worktree.
	Gate []string `json:"gate"`
	// Rounds is how many times a failing gate goes back to the session before the build stops.
	Rounds int `json:"rounds"`
}

const configFile = "kiln.json"

// initial is the kiln.json that kiln init writes.
var initial = config{Gate: []string{"go vet ./...", "go test ./..."}, Rounds: 3}

func parseConfig(raw []byte) (config, error) {
	var c config
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("%s: %w", configFile, err)
	}

	if len(c.Gate) == 0 || c.Rounds < 0 {
		return c, fmt.Errorf("%s names no gate command, or fewer than 0 rounds", configFile)
	}

	return c, nil
}

func (r *Repo) config(ctx context.Context, at git.Rev) (config, error) {
	raw, found, err := r.git.Show(ctx, at, configFile)
	if err != nil {
		return config{}, err
	}

	if !found {
		return config{}, fmt.Errorf("main holds no %s: run kiln init, and commit it", configFile)
	}

	return parseConfig(raw)
}

// check is one gate command's run.
type check struct {
	Command string `json:"command"`
	Exit    int    `json:"exit"`
	// Output is the end of what the command printed.
	Output string `json:"output,omitempty"`
	// Failed names each Go test the output says failed, with its package.
	Failed []string `json:"failed,omitempty"`
	// Broken is true when a Go package failed with no test named. A timeout or a failed build does that.
	Broken bool `json:"broken,omitempty"`
}

// outputKept is how much of a command's output a check keeps, from its end.
const outputKept = 16 << 10

func runChecks(ctx context.Context, dir string, commands, env []string) ([]check, error) {
	var checks []check
	for _, command := range commands {
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
		cmd.Dir, cmd.Env = dir, proc.Shell.Env(env)
		proc.OwnGroup(cmd)
		out, err := cmd.CombinedOutput()
		c := check{Command: command}
		c.Failed, c.Broken = failedTests(string(out))
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			c.Exit = exit.ExitCode()
		} else if err != nil {
			return nil, fmt.Errorf("the gate's %q: %w", command, err)
		}

		c.Output = string(out[max(0, len(out)-outputKept):])
		checks = append(checks, c)
	}

	return checks, ctx.Err()
}

// failedTests reads go test's output for the top-level tests that failed, each named with its package. broken is true
// when a package failed with no test named.
func failedTests(output string) (failed []string, broken bool) {
	var pending []string
	for line := range strings.Lines(output) {
		line = strings.TrimRight(line, "\r\n")
		if name, ok := strings.CutPrefix(line, "--- FAIL: "); ok {
			name, _, _ = strings.Cut(name, " ")
			pending = append(pending, name)
		} else if rest, ok := strings.CutPrefix(line, "FAIL\t"); ok {
			pkg, _, _ := strings.Cut(rest, "\t")
			for _, name := range pending {
				failed = append(failed, name+" ("+pkg+")")
			}

			broken = broken || len(pending) == 0
			pending = nil
		}
	}

	return append(failed, pending...), broken
}

// verdict is what a gate found.
type verdict struct {
	passed bool
	// atMain names the Go tests that failed at main.
	atMain []string
	// report says what failed, for the session and for kiln status.
	report string
	// failing names each failure report counts. A Go test is named as check.Failed names it, and only if it passes
	// at main. A command that failed is named when it names no such test. A refused path is named too.
	failing []string
	// rounds is how many times kiln.json lets a failing gate go back to the session.
	rounds int
}

// judge passes a gate when each check at the tip exited 0, or failed on Go tests failing at main. A package that
// failed with no test named is a failure nothing at main excuses. refused says, by path, why the build may not change
// each path it holds.
func judge(tip, main []check, refused map[string]string) verdict {
	var v verdict
	atMain := map[string]bool{}
	for _, c := range main {
		for _, t := range c.Failed {
			atMain[c.Command+"\x00"+t] = true
			v.atMain = append(v.atMain, t)
		}
	}

	var failures []string
	for _, c := range tip {
		failedAtMain := func(t string) bool { return atMain[c.Command+"\x00"+t] }
		excused := !c.Broken && len(c.Failed) > 0 && !slices.ContainsFunc(c.Failed, not(failedAtMain))
		if c.Exit == 0 || excused {
			continue
		}

		failures = append(failures, fmt.Sprintf("`%s` exited %d:\n%s", c.Command, c.Exit, strings.TrimSpace(c.Output)))
		fresh := slices.DeleteFunc(slices.Clone(c.Failed), failedAtMain)
		v.failing = append(v.failing, fresh...)
		if c.Broken || len(fresh) == 0 {
			v.failing = append(v.failing, "`"+c.Command+"`")
		}
	}

	for _, p := range slices.Sorted(maps.Keys(refused)) {
		failures = append(failures, refused[p])
		v.failing = append(v.failing, p)
	}

	slices.Sort(v.atMain)
	v.atMain = slices.Compact(v.atMain)
	v.passed = len(failures) == 0
	v.report = strings.Join(failures, "\n\n")
	return v
}

func not[T any](f func(T) bool) func(T) bool {
	return func(t T) bool { return !f(t) }
}

// gate runs the gate on the commit checked out at tree, a worktree of a build of spec. It runs kiln.json's commands
// there, and at main's tip, and checks the paths the work changed since it left main. A path in allowed may change,
// though guarded names it.
func (r *Repo) gate(ctx context.Context, tree git.Worktree, spec string, allowed ...string) (verdict, error) {
	main, err := r.git.Tip(ctx, mainBranch)
	if err != nil {
		return verdict{}, err
	}

	c, err := r.config(ctx, main)
	if err != nil {
		return verdict{}, err
	}

	head, err := r.git.Head(ctx, tree)
	if err != nil {
		return verdict{}, err
	}

	base, err := r.git.MergeBase(ctx, main, head)
	if err != nil {
		return verdict{}, err
	}

	changed, err := r.git.Changed(ctx, base, head)
	if err != nil {
		return verdict{}, err
	}

	refused := map[string]string{}
	for _, p := range changed {
		if why := guarded(p, spec); why != "" && !slices.Contains(allowed, p) {
			refused[p] = why
		}
	}

	tip, err := runChecks(ctx, string(tree), c.Gate, r.env)
	if err != nil {
		return verdict{}, err
	}

	atMain, err := r.atMain(ctx, main, c.Gate)
	if err != nil {
		return verdict{}, err
	}

	v := judge(tip, atMain, refused)
	v.rounds = c.Rounds
	return v, nil
}

// atMain runs commands at main's tip, in a worktree of its own. It keeps the checks, and runs them again only once main
// or the commands change. kiln run and kiln accept share the worktree, one at a time.
func (r *Repo) atMain(ctx context.Context, main git.Rev, commands []string) ([]check, error) {
	unlock, err := r.store.lock(mainLock, syscall.LOCK_EX)
	if err != nil {
		return nil, err
	}

	defer unlock()

	key := string(main) + "\x00" + strings.Join(commands, "\x00")
	b, err := r.store.read()
	if err != nil {
		return nil, err
	}

	if checks, ok := b.Main[key]; ok {
		return checks, nil
	}

	tree := filepath.Join(r.trees, mainTree)
	if err := r.git.Detached(ctx, git.Worktree(tree), main); err != nil {
		return nil, err
	}

	checks, err := runChecks(ctx, tree, commands, r.env)
	if err != nil {
		return nil, err
	}

	return checks, r.store.update(func(b *book) error {
		b.Main = map[string][]check{key: checks}
		return nil
	})
}

// Precommit refuses a commit to main when one of kiln.json's commands fails. It runs them in dir, the working tree,
// unstaged edits included, and names each Go test that failed. A pre-commit hook in dir calls it, with git's env.
func Precommit(ctx context.Context, dir string, env []string, out *os.File) error {
	branch, err := plainGit(ctx, dir, env, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || branch != mainBranch {
		return nil
	}

	raw, err := os.ReadFile(filepath.Join(dir, configFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	c, err := parseConfig(raw)
	if err != nil {
		return err
	}

	checks, err := runChecks(ctx, dir, c.Gate, env)
	if err != nil {
		return err
	}

	v := judge(checks, nil, nil)
	if v.passed {
		return nil
	}

	var named []string
	for _, c := range checks {
		named = append(named, c.Failed...)
	}

	fmt.Fprintf(out, "%s\n\nkiln: the commit to main is refused, since the gate fails", v.report)
	if len(named) > 0 {
		fmt.Fprintf(out, " on %s", strings.Join(named, ", "))
	}

	fmt.Fprintln(out, ".")
	return fmt.Errorf("%w: the gate fails on what the commit holds", errRefused)
}

// plainGit runs git as the developer would, with no setting pinned, and with env's GIT_ variables.
func plainGit(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.Output()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, exit.Stderr)
	}

	return strings.TrimSpace(string(out)), err
}
