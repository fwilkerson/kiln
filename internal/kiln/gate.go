package kiln

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/fwilkerson/kiln/internal/git"
)

// check is one gate command's run.
type check struct {
	Command string `json:"command"`
	Exit    int    `json:"exit"`
	// Output is the end of what the command printed.
	Output string `json:"output,omitempty"`
	// Cut is how many bytes the command printed before Output, which the check dropped.
	Cut int `json:"cut,omitempty"`
	// Failed names each test that failed, as the output of go test or the command's JUnit XML names it.
	Failed tests `json:"failed,omitempty"`
	// Ran names each test the command's JUnit XML lists. The output of go test lists none.
	Ran tests `json:"ran,omitempty"`
	// Broken is true when the command failed in a way no test names. A Go package that fails to build does that. So do
	// JUnit XML that will not parse, and a test module that will not import.
	Broken bool `json:"broken,omitempty"`
	// CutShort names each Go package whose test binary ended before it reported, as os.Exit, a panic or a timeout does.
	// Such a check is broken too.
	CutShort []string `json:"cut_short,omitempty"`
}

// tests names tests, each as a check names it, such as "TestAT1 (example.com/acceptance/x)".
type tests []string

// outputKept is how much of a command's output a check keeps, from its end.
const outputKept = 16 << 10

// junitVar names the file a gate command may write JUnit XML to, so kiln learns the tests that failed.
const junitVar = "KILN_JUNIT"

// runChecks runs each of commands in dir, in turn, as s runs them. A command that ran past s's limit is no check: it
// answers a *ranPast, which stops the work.
func runChecks(ctx context.Context, dir git.Worktree, commands []string, s projectShell) ([]check, error) {
	var checks []check
	for _, command := range commands {
		c, err := runCheck(ctx, dir, command, s)
		if err != nil {
			return nil, err
		}

		checks = append(checks, c)
	}

	return checks, ctx.Err()
}

// runCheck runs one gate command in dir. Its JUnit XML goes to a new directory, so no earlier run's file is read.
func runCheck(ctx context.Context, dir git.Worktree, command string, s projectShell) (check, error) {
	tmp, err := os.MkdirTemp("", "kiln-junit-")
	if err != nil {
		return check{}, err
	}

	defer os.RemoveAll(tmp)
	junit := filepath.Join(tmp, "junit.xml")
	printed, err := s.with(junitVar+"="+junit).run(ctx, dir, command)
	out := []byte(printed.both)
	cut := max(0, len(out)-outputKept)
	c := check{Command: command, Output: string(out[cut:]), Cut: cut}
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		c.Exit = exit.ExitCode()
	} else if err != nil {
		return check{}, fmt.Errorf("the gate's %q: %w", command, err)
	}

	goFailed, cutShort, goBroken := failedTests(string(out))
	failed, ran, broken := readJUnit(junit)
	c.Failed = slices.Compact(slices.Sorted(slices.Values(append(goFailed, failed...))))
	c.Ran, c.CutShort, c.Broken = ran, cutShort, goBroken || broken
	return c, nil
}

// failedTests reads the output of go test for the top-level tests that failed, each named with its package. cutShort
// names each package that named a failed test, but whose test binary ended before it reported. broken is true when a
// package failed with no test named, or is cut short.
func failedTests(output string) (failed, cutShort []string, broken bool) {
	var f failures
	for line := range strings.Lines(output) {
		f.read(strings.TrimRight(line, "\r\n"))
	}

	return append(f.failed, f.pending...), f.cutShort, f.broken || len(f.cutShort) > 0
}

// failures holds what the output of go test says failed, so far. pending names the tests that failed in a package whose
// FAIL line is still to come. finished is whether a lone FAIL line followed the last failed test's line. A test binary
// prints that line when it reports.
type failures struct {
	failed, pending, cutShort []string
	broken, finished          bool
}

// read reads one line of the output of go test.
func (f *failures) read(line string) {
	if line == "FAIL" {
		f.finished = true
		return
	}

	if name, ok := strings.CutPrefix(line, "--- FAIL: "); ok {
		name, _, _ = strings.Cut(name, " ")
		f.pending, f.finished = append(f.pending, name), false
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
	} else if !f.finished {
		// A binary that os.Exit, a stray panic or the timeout ended prints no lone FAIL, and may have skipped tests.
		f.cutShort = append(f.cutShort, pkg)
	}

	f.pending, f.finished = nil, false
}

// verdict is what a gate found.
type verdict struct {
	passed bool
	// atMain names the tests that failed at main.
	atMain []string
	// report says what failed, for the session and for kiln status.
	report string
	// failing names each failure report counts. A test is named as check.Failed names it, and only if it passes at
	// main. A command that failed is named when it names no such test. A refused path is named too.
	failing []string
	// rounds is how many times kiln.json lets a failing gate go back to the session.
	rounds int
	// ranMain is whether the gate ran the commands at main, rather than reading the checks kept from an earlier gate.
	ranMain bool
	// head is the commit the gate ran on, at its first guard's place.
	head git.Rev
}

// judge passes a gate when each check at the tip exited 0, or failed on tests failing at main. A check that is broken
// is a failure nothing at main excuses. refused says, by path, why the build may not change each path it holds.
func judge(tip, main []check, refused map[string]string) verdict {
	atMain, names := failedAt(main)
	v := verdict{atMain: names}
	var failures []string
	for _, c := range tip {
		if report, failing := c.judged(atMain[c.Command]); report != "" {
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

// failedAt reads the checks at main, as the output of go test and JUnit XML name their tests. It answers each check by
// command. It answers the names of the tests that failed too, sorted, each once. A command run twice answers one
// check, which holds what both failed and ran.
func failedAt(main []check) (byCommand map[string]check, names []string) {
	byCommand = map[string]check{}
	for _, c := range main {
		if seen, ok := byCommand[c.Command]; ok {
			c.Exit = cmpOr(c.Exit, seen.Exit)
			c.Failed = append(slices.Clone(seen.Failed), c.Failed...)
			c.Ran = append(slices.Clone(seen.Ran), c.Ran...)
			c.Broken = c.Broken || seen.Broken
		}

		byCommand[c.Command] = c
		names = append(names, c.Failed...)
	}

	slices.Sort(names)
	return byCommand, slices.Compact(names)
}

// unnamed is what a report adds for a command that failed at the tip and at main. It named no test at either.
const unnamed = "The command named no failed test, so kiln could not compare it with main.\n" +
	"A test runner names its failed tests by writing JUnit XML to $" + junitVar + "."

// judged says what failed in c, at the tip, given main, the check of c's command at main. It answers "" when c passed,
// or failed only on tests failing at main. failing names what failed, as verdict.failing does. c is never excused
// when it is broken, or names no test. Nor is a run cut short: every test main ran must have run in c too.
func (c check) judged(main check) (report string, failing []string) {
	if c.Exit == 0 {
		return "", nil
	}

	failing, notRun := c.against(main)
	if c.excused(failing, notRun) {
		return "", nil
	}

	if c.Broken || len(failing) == 0 {
		failing = append(failing, "`"+c.Command+"`")
	}

	return c.report(main, notRun), failing
}

// excused is whether c failed only on tests that failed at main, and ran every test main ran. failing and notRun are
// what against answers.
func (c check) excused(failing, notRun tests) bool {
	return !c.Broken && len(c.Failed) > 0 && len(failing) == 0 && len(notRun) == 0
}

// report says what failed in c. main is the check of c's command at main, and notRun the tests main ran but c did not.
// It names each test that failed in c but not at main. The output c kept may have lost a failure.
func (c check) report(main check, notRun tests) string {
	report := fmt.Sprintf("`%s` exited %d.", c.Command, c.Exit)
	if failed, _ := c.against(main); len(failed) > 0 {
		report += " These tests failed: " + strings.Join(failed, ", ") + "."
	}

	if c.Cut > 0 {
		report += fmt.Sprintf(" The output below lacks its first %d bytes, so it may not show each failure.", c.Cut)
	}

	report += "\n" + strings.TrimSpace(c.Output)
	if len(c.CutShort) > 0 {
		report += "\n\nThe run was cut short. These packages ended before their tests reported: " +
			strings.Join(c.CutShort, ", ") + ". A test there may call os.Exit, or panic in a goroutine."
	}

	if len(notRun) > 0 {
		report += "\n\nThe run was cut short. These tests ran at main, but not here: " +
			strings.Join(notRun, ", ") + "."
	}

	if c.namesNothing() && main.namesNothing() {
		report += "\n\n" + unnamed
	}

	return report
}

// against answers the tests that failed in c but not in main. It answers the tests main ran that c did not, too.
func (c check) against(main check) (failedHere, notRun tests) {
	return c.Failed.without(main.Failed), main.Ran.without(c.Ran)
}

// without is each test in t that is not in drop.
func (t tests) without(drop tests) tests {
	return slices.DeleteFunc(slices.Clone(t), func(n string) bool { return slices.Contains(drop, n) })
}

// namesNothing is whether c failed, is not broken, and names no failed test.
func (c check) namesNothing() bool {
	return c.Exit != 0 && !c.Broken && len(c.Failed) == 0
}

// gate runs the gate on the commit checked out at the first guard's checkout, a build's checkout. It runs
// kiln.json's commands there, and at main's tip. It checks the paths each guard's work changed since it left
// main, as that guard says. without holds the paths kiln removed from the work. Main is gated without them too.
// Their removal is no write of the work's.
func (held commands) gate(ctx context.Context, without []string, guards ...guard) (verdict, error) {
	r := held.r
	main, c, err := r.mainConfig(ctx)
	if err != nil {
		return verdict{}, err
	}

	refused, heads := map[string]string{}, make([]git.Rev, len(guards))
	for i, g := range guards {
		found, head, err := r.refused(ctx, g, main, without)
		if err != nil {
			return verdict{}, err
		}

		maps.Copy(refused, found)
		heads[i] = head
	}

	// No session runs while the gate does, so the first guard's checkout still holds the commit refused read.
	tip, err := r.atCommit(ctx, guards[0].spec, heads[0], c)
	if err != nil {
		return verdict{}, err
	}

	atMain, ran, err := held.atMain(ctx, main, c, without)
	if err != nil {
		return verdict{}, err
	}

	v := judge(tip, atMain, refused)
	v.rounds, v.ranMain, v.head = c.Rounds, ran, heads[0]
	return v, nil
}

// atCommit runs c's gate on head, the commit checked out at spec's checkout, in the gate's own worktree. The gate
// then judges what lands. It never sees a file the session left that the commit lacks. The session's worktree stays
// as it was.
func (r *Repo) atCommit(ctx context.Context, spec Spec, head git.Rev, c config) ([]check, error) {
	tree := r.gateTree(spec)
	if err := r.git.Detached(ctx, tree, head); err != nil {
		return nil, err
	}

	return runChecks(ctx, tree, c.Gate, c.shell(r.env))
}

// refused says, by path, why the build may not change each path its work changed since it left main. A path under
// one of without that the work lacks is kiln's removal, not the work's. It answers head, the commit it read at g's
// place.
func (r *Repo) refused(ctx context.Context, g guard, main git.Rev, without []string) (map[string]string, git.Rev,
	error) {
	head := g.head
	if head == "" {
		var err error
		if head, err = r.git.Head(ctx, g.place); err != nil {
			return nil, "", err
		}
	}

	changed, err := r.git.Branched(ctx, main, head)
	if err != nil {
		return nil, "", err
	}

	refused := map[string]string{}
	for _, p := range changed {
		why := g.guarded(p)
		if why == "" {
			continue
		}

		removed, err := r.removed(ctx, head, p, without)
		if err != nil {
			return nil, "", err
		}

		if !removed {
			refused[p] = why
		}
	}

	return refused, head, nil
}

// removed is whether p is under one of without, and head lacks it. That is kiln's removal, not the work's.
func (r *Repo) removed(ctx context.Context, head git.Rev, p string, without []string) (bool, error) {
	if !slices.ContainsFunc(without, func(root string) bool { return within(fold(p), root) }) {
		return false, nil
	}

	_, held, err := r.git.Show(ctx, head, p)
	return !held, err
}

// atMain runs c's gate at main's tip, less the paths without, in a worktree of its own. It keeps the checks, and
// runs them again only once main, without or the commands change. Its bool is whether it ran them. It runs under
// held, so every kiln shares the worktree, one at a time.
func (held commands) atMain(ctx context.Context, main git.Rev, c config, without []string) ([]check, bool, error) {
	r, gate := held.r, c.Gate
	// The count of without tells where its paths end and the commands begin.
	parts := append([]string{string(main), fmt.Sprint(len(without))}, without...)
	key := strings.Join(append(parts, gate...), "\x00")
	b, err := r.store.read()
	if err != nil {
		return nil, false, err
	}

	if checks, ok := b.Main[key]; ok {
		return checks, false, nil
	}

	tree := git.Worktree(filepath.Join(r.trees, mainTree))
	if err := r.git.Detached(ctx, tree, main); err != nil {
		return nil, false, err
	}

	// The next Detached puts back what this removes.
	for _, p := range without {
		if err := os.RemoveAll(filepath.Join(string(tree), p)); err != nil {
			return nil, false, err
		}
	}

	checks, err := runChecks(ctx, tree, gate, c.shell(r.env))
	if err != nil {
		return nil, false, err
	}

	return checks, true, r.store.update(func(b *book) error {
		b.Main = map[string][]check{key: checks}
		return nil
	})
}

// Precommit refuses a commit to main when one of .kiln/kiln.json's commands fails. It runs them in dir, the working
// tree, unstaged edits included, and names each test that failed. A pre-commit hook in dir calls it, with git's env.
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

	checks, err := runChecks(ctx, git.Worktree(dir), c.Gate, c.shell(env))
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

	fmt.Fprintf(out, ". Make the gate's commands pass, or change gate in %s.\n", configFile)
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
