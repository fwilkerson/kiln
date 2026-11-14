package kiln

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/proc"
)

// A monitor's fix is a build with no spec, on kiln/<name>, in the monitor's worktree:
//
//	command, then commit and gate (+ check) ── pass ──────────────────────────┐
//	  │ fail, or no command                                                   │
//	  ▼                                                                       ▼
//	agent, then commit and gate (+ check), up to rounds ── pass ─▶ audit ─▶ unchanged, waits, or lands
//	  │ asks for the developer ─▶ needs you    │ fails ─▶ session failed    │ usage limit ─▶ usage limit
//
// A gated check that couldn't run ends the fix couldn't run. Past the last round, or on a command alone that the gate
// fails, it stops.

var (
	//go:embed contract/monitor.md
	monitorContract string
	//go:embed contract/monitor-audit.md
	monitorAuditContract string
)

// fixing is what a monitor's fix carries beside its build.
type fixing struct {
	monitor monitor
	// checked is what the run's first check found. A landing kiln accept started has none.
	checked checked
	// side is what the fix may write.
	side Side
	// outcome, summary and report say how the fix ended, once it has.
	outcome, summary, report string
}

// newFixing is the fix of m, as c, kiln.json at main's tip when it started, says. found is what its check found.
func newFixing(c config, m monitor, found checked) *fixing {
	side := Side{Fix: true}
	if !m.Tests {
		side.Role, side.Acceptance = buildRole, c.fixedPart()
	}

	return &fixing{monitor: m, checked: found, side: side}
}

// end ends the fix with outcome.
func (f *fixing) end(outcome, summary, report string) {
	f.outcome, f.summary, f.report = outcome, summary, report
}

// fixOf reads the fix of rec: its monitor, as kiln.json held it when the fix started.
func (r *Repo) fixOf(ctx context.Context, rec record) (*fixing, error) {
	c, err := r.config(ctx, rec.Approved)
	if err != nil {
		return nil, err
	}

	m, ok := c.monitorNamed(rec.Name)
	if !ok {
		return nil, fmt.Errorf("%s at %s names no monitor %s", configFile, short(rec.Approved), rec.Name)
	}

	return newFixing(c, m, checked{}), nil
}

// checkFailed is a gated check that couldn't run. line is its last line of output.
type checkFailed struct {
	line string
}

func (c *checkFailed) Error() string { return "the monitor's check couldn't run: " + c.line }

// errLimitEnded is a fix's session that a usage limit ended. kiln never waits for the limit to reset, since it would
// hold the run lock until then.
var errLimitEnded = errors.New("a usage limit ended the session")

// startFix starts the fix f, from main's tip as the monitor line says, and runs it until it ends. A stopped fix of
// its monitor gives way to it.
func (r *Repo) startFix(ctx context.Context, out io.Writer, f *fixing, line entry) (*build, error) {
	m := f.monitor
	rec := record{Name: m.Name, State: building, Approved: line.Main, Build: line.Build, Monitor: true,
		PID: os.Getpid()}
	err := r.store.update(func(b *book) error {
		b.retire(m.Name)
		b.Specs = append(b.Specs, &rec)
		return nil
	})
	if err != nil {
		return nil, err
	}

	b := &build{r: r, rec: rec, tree: r.tree(m.Name), out: out, fix: f}
	b.record(entry{Kind: builtKind, Spec: m.Name, Approved: line.Main, Monitor: m.Name})
	return b, b.fixSettle(ctx, b.fixWork(ctx))
}

// fixWork runs the fix: the command first, then the agent when the command left the gate failing.
func (b *build) fixWork(ctx context.Context) error {
	name := b.rec.Name
	if err := b.r.git.Remove(ctx, b.tree, name.branch()); err != nil {
		return err
	}

	if err := b.r.git.Checkout(ctx, b.tree, name.branch(), b.rec.Approved); err != nil {
		return err
	}

	b.say("fixing on %s, in %s", name.branch(), b.tree)
	m := b.fix.monitor
	if m.Command == "" {
		return b.fixAgent(ctx, "")
	}

	v, err := b.commandGate(ctx)
	switch {
	case err != nil:
		return err
	case v.passed:
		_, _, err := b.fixAfter(ctx)
		return err
	case m.role() == "":
		return &outOfRounds{why: "the gate failed after the monitor's command:\n" + v.report, failing: v.failing}
	}

	return b.fixAgent(ctx, v.report)
}

// commandGate runs the monitor's command in the fix's worktree, outside the sandbox, then commits and gates its work.
func (b *build) commandGate(ctx context.Context) (verdict, error) {
	command := b.fix.monitor.Command
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Dir, cmd.Env = string(b.tree), proc.Shell.Env(b.r.env)
	proc.OwnGroup(cmd)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return verdict{}, ctx.Err()
	}

	if err != nil {
		b.say("the command failed, %v; the gate judges its work:\n%s", err, indent(lastLines(string(out), 10)))
	}

	if _, err := b.r.git.Commit(ctx, b.tree, b.message("the command's work")); err != nil {
		return verdict{}, err
	}

	return b.gate(ctx, false)
}

// lastLines is the last n lines of text.
func lastLines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}

// fixAgent runs the monitor's agent until the fix ends. gateReport is what the gate printed after the command, or "".
func (b *build) fixAgent(ctx context.Context, gateReport string) error {
	b.set(func(r *record) { r.Session = newSession() })
	l, err := b.fixLaunch(ctx, false, b.fix.prompt(gateReport))
	why := reason{Why: whyFirst}
	for err == nil {
		var ended claude.Ended
		if ended, err = b.talk(ctx, l, why); err != nil || b.fix.heard(ended, b.r.claude.Stderr(l.Session)) {
			return err
		}

		var next string
		if next, why, err = b.fixGate(ctx); err != nil || next == "" {
			return err
		}

		l, err = b.fixLaunch(ctx, true, next)
	}

	return err
}

// prompt is the agent's first message. It holds what the check printed, and what the gate printed after the command.
// The monitor's prompt follows.
func (f *fixing) prompt(gateReport string) string {
	m := f.monitor
	var parts []string
	if m.Check != "" {
		parts = append(parts, fmt.Sprintf(checkFound, m.Check, f.checked.summary, cmpOr(f.checked.found, "(nothing)")))
	}

	if gateReport != "" {
		parts = append(parts, fmt.Sprintf(commandFailed, m.Command, gateReport))
	}

	parts = append(parts, cmpOr(m.Prompt, fmt.Sprintf(noPrompt, m.Name)))
	return strings.Join(parts, "\n\n")
}

// heard reads how a turn of the agent's session ended. It answers true once that ends the fix. The session failed,
// or asked for the developer. stderr is what the session wrote to its standard error.
func (f *fixing) heard(ended claude.Ended, stderr string) bool {
	if ended.Failed {
		last, _ := lastOf(stderr)
		why := cmpOr(strings.TrimSpace(ended.Text), last)
		f.end(outFailed, cmpOr(why, "the session ended with no result"), "")
		return true
	}

	summary, report, asked := askOf(ended.Text)
	if asked {
		f.end(outNeedsYou, summary, report)
	}

	return asked
}

// askOf reads an agent's reply that asks for the developer. Its last line is {"ask": true, "summary": "..."}, and
// the rest is its report.
func askOf(text string) (summary, report string, asked bool) {
	last, above := lastOf(text)
	var ask struct {
		Ask     bool   `json:"ask"`
		Summary string `json:"summary"`
	}
	if !jsonLine(last, &ask) {
		return "", "", false
	}

	return ask.Summary, strings.TrimSpace(above), ask.Ask
}

// jsonLine reads line into v, if it is a JSON object.
func jsonLine(line string, v any) bool {
	return strings.HasPrefix(line, "{") && json.Unmarshal([]byte(line), v) == nil
}

// fixGate commits what the agent's session left, gates it, and takes the fix on from the verdict. It answers what to
// tell the session next, and why, or "" once the fix has ended.
func (b *build) fixGate(ctx context.Context) (string, reason, error) {
	if _, err := b.r.git.Commit(ctx, b.tree, b.message("the session's work")); err != nil {
		return "", reason{}, err
	}

	v, err := b.gate(ctx, false)
	switch {
	case err != nil:
		return "", reason{}, err
	case !v.passed:
		return b.round(setback{why: whyGate, report: v.report, failing: v.failing})
	}

	return b.fixAfter(ctx)
}

// fixAfter takes a fix on once its gate passed. Work that changes nothing ends unchanged. An agent's work gets the
// audit. Then the fix waits on its branch, or lands. A setback goes back to the session, or stops a fix with none.
func (b *build) fixAfter(ctx context.Context) (string, reason, error) {
	head, changed, err := b.fixChanged(ctx)
	switch {
	case err != nil:
		return "", reason{}, err
	case !changed:
		b.fix.end(outUnchanged, "", "")
		return "", reason{}, nil
	}

	if b.rec.Session != "" {
		if findings, why, err := b.findings(ctx); err != nil || findings != "" {
			return fmt.Sprintf(fixAudited, findings), why, err
		}
	}

	if b.fix.monitor.Accept {
		b.fix.end(outWaits, "", "")
		return "", reason{}, nil
	}

	return b.fixLand(ctx, head)
}

// fixLand lands the fix at head. A setback goes back to the session, or stops a fix with none.
func (b *build) fixLand(ctx context.Context, head git.Rev) (string, reason, error) {
	landed, back, err := b.land(ctx, head, nil)
	switch {
	case err != nil:
		return "", reason{}, err
	case back.report == "":
		b.landed = landed
		b.fix.end(outLanded, "", "")
		return "", reason{}, nil
	case b.rec.Session == "":
		return "", reason{}, &outOfRounds{why: "the landing failed:\n" + back.report, failing: back.failing}
	}

	return b.round(back)
}

// fixChanged answers the fix's commit, and whether it changes anything since it left main.
func (b *build) fixChanged(ctx context.Context) (head git.Rev, changed bool, err error) {
	if head, err = b.r.git.Head(ctx, b.tree); err != nil {
		return "", false, err
	}

	main, err := b.r.git.Tip(ctx, mainBranch)
	if err != nil {
		return "", false, err
	}

	base, err := b.r.git.MergeBase(ctx, main, head)
	if err != nil {
		return "", false, err
	}

	paths, err := b.r.git.Changed(ctx, base, head)
	return head, len(paths) > 0, err
}

// landFix lands a fix the developer accepted. It is rebased onto main, and gated again, check included. Then main
// moves onto it if main hasn't moved. A setback stops it, and kiln accept tries it again.
func (b *build) landFix(ctx context.Context) error {
	f, err := b.r.fixOf(ctx, b.rec)
	if err != nil {
		return err
	}

	b.fix = f
	if err := b.unland(ctx); err != nil {
		return err
	}

	head, err := b.r.git.Head(ctx, b.tree)
	if err != nil {
		return err
	}

	landed, back, err := b.land(ctx, head, nil)
	switch {
	case err != nil:
		return err
	case back.report != "":
		return &outOfRounds{why: "the landing failed:\n" + back.report, failing: back.failing}
	}

	b.landed = landed
	f.end(outLanded, "", "")
	return nil
}

// fixSettle ends the fix for err, the error its work ended with, as fixEnded records it. It answers the context's
// error once kiln is interrupted, which leaves the fix building, cut short.
func (b *build) fixSettle(ctx context.Context, err error) error {
	if b.fix == nil {
		b.fix = &fixing{}
	}

	f := b.fix
	err = b.landingStops(ctx, err)
	stopped, isStop := errors.AsType[*outOfRounds](err)
	failed, isCheck := errors.AsType[*checkFailed](err)
	switch {
	case isStop:
		f.end(outStopped, "", "")
		b.set(func(r *record) { r.Why, r.Failing = stopped.why, stopped.failing })
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, errLimitEnded):
		f.end(outLimit, errLimitEnded.Error(), "")
	case isCheck:
		f.end(outCouldNot, failed.line, "")
	case err != nil:
		f.end(outStopped, "", "")
		b.set(func(r *record) { r.Why, r.Failing = couldNotGoOn+err.Error(), nil })
	}

	return b.fixEnded(ctx)
}

// landingStops answers err as a stop for an accepted fix's landing, which only lands or stops. So no fix the
// developer accepted is lost, as one that couldn't run would be. An interrupt stays as it is.
func (b *build) landingStops(ctx context.Context, err error) error {
	if !b.now().Accepted || ctx.Err() != nil {
		return err
	}

	if _, isStop := errors.AsType[*outOfRounds](err); isStop || err == nil {
		return err
	}

	return &outOfRounds{why: "the landing failed: " + err.Error()}
}

// fixEnded records how the fix ended. A stopped fix keeps its record and its worktree, and a fix that passed waits
// on its branch. Any other leaves no record, branch or worktree.
func (b *build) fixEnded(ctx context.Context) error {
	f := b.fix
	if f.outcome == outStopped {
		f.summary = cmpOr(f.checked.summary, strings.SplitN(b.now().Why, "\n", 2)[0])
		f.report = b.now().Why
	}

	b.record(entry{Kind: outcomeKind, Outcome: f.outcome, Summary: f.summary, Report: f.report})
	b.say("%s", cmpOr(strings.TrimSuffix(f.outcome+": "+f.summary, ": "), "ended"))
	switch f.outcome {
	case outStopped:
		b.set(func(r *record) { r.State, r.runner = stopped, runner{} })
		return nil
	case outWaits:
		b.set(func(r *record) { r.State, r.runner = waiting, runner{} })
		return nil
	}

	if err := b.r.store.update(func(k *book) error { k.retire(b.rec.Name); return nil }); err != nil {
		return err
	}

	return b.r.dropCheckouts(context.WithoutCancel(ctx), b.rec.Name)
}

// fixLaunch is a run of the monitor's agent's session, in the fix's worktree. Its hook keeps it off .kiln/, and off
// the acceptance tests unless the monitor says it may write them.
func (b *build) fixLaunch(ctx context.Context, resume bool, prompt string) (claude.Launch, error) {
	f, name := b.fix, b.rec.Name
	flags := []string{shellQuote(b.r.exe), "hook", "--spec", string(name), "--place", shellQuote(string(b.tree)),
		"--fix"}
	if f.side.Acceptance != "" {
		flags = append(flags, "--role", buildRole, "--acceptance", shellQuote(f.side.Acceptance))
	}

	role := f.monitor.role()
	a, err := b.r.agent(ctx, b.rec.Approved, role, "commit it to main, or name another agent in the monitor's entry")
	if err != nil {
		return claude.Launch{}, err
	}

	l := claude.Launch{Session: b.now().Session, Resume: resume, Prompt: prompt, Dir: string(b.tree),
		Hook: strings.Join(flags, " "), Role: role, System: strings.TrimSpace(a.body + "\n\n" + f.contract(name))}
	l.Tools, l.Model, l.Effort, l.Advisor = a.tools, a.model, a.effort, a.advisor
	return b.sandboxed(ctx, l)
}

// kept is the forbidden action that keeps the fix's session off what it may not write.
func (f *fixing) kept() string {
	if f.side.Acceptance == "" {
		return "Changing anything under .kiln/, by a file tool or a shell command."
	}

	return fmt.Sprintf("Changing anything under .kiln/, or under %s, by a file tool or a shell command.",
		f.side.Acceptance)
}

// contract is kiln's contract with the agent of the monitor name.
func (f *fixing) contract(name Spec) string {
	tests := "The monitor says its fixes may write the acceptance tests."
	if f.side.Acceptance != "" {
		tests = fmt.Sprintf("Nor is anything under `%s`, the acceptance tests, which judge your fix.",
			f.side.Acceptance)
	}

	return fmt.Sprintf(monitorContract, name, tests)
}

const (
	checkFound    = "The monitor's check, `%s`, asks for a fix: %s\n\nIt printed:\n\n%s"
	commandFailed = "The monitor's command, `%s`, ran, and the gate failed after it:\n\n%s"
	noPrompt      = "Fix what the monitor %s found, and end your turn."
	auditingFix   = "Audit these directories: %s. They hold the fix of the monitor %s, which has no spec. The fix " +
		"changed these files: %s."
	fixAudited = "The audit read the directories you changed. Fix each finding, or say in your reply why it is " +
		"wrong. Then end your turn.\n\n%s"
)

// gatedCheck adds a fix's gated check to the gate's verdict v. The gate fails while the check asks for a fix,
// whatever it exits with. A check that couldn't run is a checkFailed.
func (b *build) gatedCheck(ctx context.Context, v verdict) (verdict, error) {
	if b.fix == nil || !b.fix.monitor.gated() {
		return v, nil
	}

	command := b.fix.monitor.Check
	c, err := b.r.runMonitorCheck(ctx, b.tree, command)
	switch {
	case err != nil:
		return v, err
	case !c.ran:
		return v, &checkFailed{line: c.summary}
	case !c.fix:
		return v, nil
	}

	report := fmt.Sprintf("The monitor's check, `%s`, still asks for a fix: %s", command, c.summary)
	if c.found != "" {
		report += "\n\n" + c.found
	}

	v.passed = false
	v.report = strings.TrimSpace(v.report + "\n\n" + report)
	v.failing = append(v.failing, "`"+command+"`")
	return v, nil
}
