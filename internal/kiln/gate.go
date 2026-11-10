package kiln

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// config is .kiln/kiln.json, read from main's tip, so a build cannot weaken the gate it is held to.
type config struct {
	// Gate holds the commands the gate runs, each with sh -c, at the top of a worktree.
	Gate []string `json:"gate"`
	// Rounds is how many times a failing gate goes back to the session before the build stops.
	Rounds int `json:"rounds"`
	// Hosts are the only hosts a session's commands reach.
	Hosts []string `json:"hosts,omitempty"`
	// Writable holds commands, each run with sh -c. Each field one prints is a path a session's commands may write.
	Writable []string `json:"writable,omitempty"`
}

const configFile = kilnDir + "/kiln.json"

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
		return config{}, fmt.Errorf("main holds no %s: run kiln init, and commit what it writes", configFile)
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

func runChecks(ctx context.Context, dir git.Worktree, commands, env []string) ([]check, error) {
	var checks []check
	for _, command := range commands {
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
		cmd.Dir, cmd.Env = string(dir), proc.Shell.Env(env)
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
	var f failures
	for line := range strings.Lines(output) {
		f.read(strings.TrimRight(line, "\r\n"))
	}

	return append(f.failed, f.pending...), f.broken
}

// failures is what go test's output says failed, so far. pending names the tests that failed in a package whose
// FAIL line is still to come.
type failures struct {
	failed, pending []string
	broken          bool
}

// read reads one line of go test's output.
func (f *failures) read(line string) {
	if name, ok := strings.CutPrefix(line, "--- FAIL: "); ok {
		name, _, _ = strings.Cut(name, " ")
		f.pending = append(f.pending, name)
		return
	}

	rest, ok := strings.CutPrefix(line, "FAIL\t")
	if !ok {
		return
	}

	pkg, _, _ := strings.Cut(rest, "\t")
	for _, name := range f.pending {
		f.failed = append(f.failed, name+" ("+pkg+")")
	}

	if len(f.pending) == 0 {
		f.broken = true
	}

	f.pending = nil
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
	atMain, names := failedAt(main)
	v := verdict{atMain: names}
	var failures []string
	for _, c := range tip {
		if report, failing := c.judged(atMain); report != "" {
			failures = append(failures, report)
			v.failing = append(v.failing, failing...)
		}
	}

	for _, p := range slices.Sorted(maps.Keys(refused)) {
		failures = append(failures, refused[p])
		v.failing = append(v.failing, p)
	}

	v.passed = len(failures) == 0
	v.report = strings.Join(failures, "\n\n")
	return v
}

// failedAt reads the checks at main for the Go tests that failed there. It answers them by command, and their names,
// sorted, each once.
func failedAt(main []check) (byCommand map[string]bool, names []string) {
	byCommand = map[string]bool{}
	for _, c := range main {
		for _, t := range c.Failed {
			byCommand[c.Command+"\x00"+t] = true
			names = append(names, t)
		}
	}

	slices.Sort(names)
	return byCommand, slices.Compact(names)
}

func not[T any](f func(T) bool) func(T) bool {
	return func(t T) bool { return !f(t) }
}

// judged says what failed in c, at the tip, given atMain, the tests that failed at main by command. It answers "" when
// c passed, or failed only on tests failing at main. failing names what failed, as verdict.failing does.
func (c check) judged(atMain map[string]bool) (report string, failing []string) {
	failedAtMain := func(t string) bool { return atMain[c.Command+"\x00"+t] }
	if c.Exit == 0 || c.excused(failedAtMain) {
		return "", nil
	}

	report = fmt.Sprintf("`%s` exited %d:\n%s", c.Command, c.Exit, strings.TrimSpace(c.Output))
	failing = slices.DeleteFunc(slices.Clone(c.Failed), failedAtMain)
	if c.Broken || len(failing) == 0 {
		failing = append(failing, "`"+c.Command+"`")
	}

	return report, failing
}

// excused is whether c failed only on Go tests that failedAtMain. A package that failed with no test named is never
// excused.
func (c check) excused(failedAtMain func(test string) bool) bool {
	switch {
	case c.Broken, len(c.Failed) == 0:
		return false
	}

	return !slices.ContainsFunc(c.Failed, not(failedAtMain))
}

// gate runs the gate on the commit checked out at g's worktree, a worktree of a build of g's spec. It runs kiln.json's
// commands there, and at main's tip. It checks the paths the work changed since it left main, as g says.
func (r *Repo) gate(ctx context.Context, g guard) (verdict, error) {
	main, err := r.git.Tip(ctx, mainBranch)
	if err != nil {
		return verdict{}, err
	}

	c, err := r.config(ctx, main)
	if err != nil {
		return verdict{}, err
	}

	refused, err := r.refused(ctx, g, main)
	if err != nil {
		return verdict{}, err
	}

	tip, err := runChecks(ctx, g.place, c.Gate, r.env)
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

// refused says, by path, why the build may not change each path its work changed since it left main.
func (r *Repo) refused(ctx context.Context, g guard, main git.Rev) (map[string]string, error) {
	head, err := r.git.Head(ctx, g.place)
	if err != nil {
		return nil, err
	}

	base, err := r.git.MergeBase(ctx, main, head)
	if err != nil {
		return nil, err
	}

	changed, err := r.git.Changed(ctx, base, head)
	if err != nil {
		return nil, err
	}

	refused := map[string]string{}
	for _, p := range changed {
		if why := g.guarded(p); why != "" {
			refused[p] = why
		}
	}

	return refused, nil
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

	tree := git.Worktree(filepath.Join(r.trees, mainTree))
	if err := r.git.Detached(ctx, tree, main); err != nil {
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

// Precommit refuses a commit to main when one of .kiln/kiln.json's commands fails. It runs them in dir, the working
// tree, unstaged edits included, and names each Go test that failed. A pre-commit hook in dir calls it, with git's env.
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

	checks, err := runChecks(ctx, git.Worktree(dir), c.Gate, env)
	if err != nil {
		return err
	}

	return refuseCommit(checks, out)
}

// refuseCommit refuses the commit the gate's checks fail, and says why on out.
func refuseCommit(checks []check, out io.Writer) error {
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
