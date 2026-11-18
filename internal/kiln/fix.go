package kiln

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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
// A gated check that couldn't run ends the fix couldn't run. A gauge in the gate whose command fails goes back to the
// agent, as a crossed reading does. Past the last round, or on a command alone that the gate fails, it stops.

var (
	//go:embed contract/monitor.md
	monitorContract string
	//go:embed contract/monitor-audit.md
	monitorAuditContract string
)

// fixing is what a monitor's fix carries beside its build.
type fixing struct {
	monitor monitor
	// checked is what the run's first check, or its gauge, found. A landing kiln accept started has none.
	checked checked
	// side is what the fix may write.
	side side
	// outcome, summary and report say how the fix ended, once it has.
	outcome, summary, report string
}

// newFixing is the fix of m, as c, kiln.json at main's tip when it started, says. found is what its check found.
func newFixing(c config, m monitor, found checked) *fixing {
	fixes := side{role: fixRole}
	if !m.Tests {
		fixes.acceptance = c.fixedParts()
	}

	return &fixing{monitor: m, checked: found, side: fixes}
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
	out, err := runShell(ctx, b.tree, command, proc.Shell.Env(b.r.env))
	if ctx.Err() != nil {
		return verdict{}, ctx.Err()
	}

	if err != nil {
		b.say("the command failed, %v; the gate judges its work:\n%s", err, indent(lastLines(out.both, 10)))
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

// prompt is the agent's first message. It holds what the check or the gauge printed, and what the gate printed after
// the command. The monitor's prompt follows.
func (f *fixing) prompt(gateReport string) string {
	m := f.monitor
	var parts []string
	found := cmpOr(f.checked.found, "(nothing)")
	switch {
	case m.Gauge != "":
		parts = append(parts, fmt.Sprintf(gaugeFound, m.Gauge, f.checked.summary, found))
	case m.Check != "":
		parts = append(parts, fmt.Sprintf(checkFound, m.Check, f.checked.summary, found))
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
	head, base, err := b.fixBase(ctx)
	if err != nil {
		return "", false, err
	}

	paths, err := b.r.git.Changed(ctx, base, head)
	return head, len(paths) > 0, err
}

// fixBase answers the fix's commit, and base, where it left main: its merge base with main's tip. Once the fix is
// rebased onto main, base is main's tip.
func (b *build) fixBase(ctx context.Context) (head, base git.Rev, err error) {
	if head, err = b.r.git.Head(ctx, b.tree); err != nil {
		return "", "", err
	}

	main, err := b.r.mainTip(ctx)
	if err != nil {
		return "", "", err
	}

	base, err = b.r.git.MergeBase(ctx, main, head)
	return head, base, err
}

// landFix lands a fix the developer accepted. It is rebased onto main, and gated again, check included. Then main
// moves onto it if main hasn't moved. A setback stops it. kiln accept tries it again after the gate, and kiln reject
// drops it after a conflict with main.
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
	case back.why == whyConflict:
		// The conflict's report asks a session to merge, and none runs on a landing.
		return &outOfRounds{why: fixConflicts + strings.Join(back.failing, "\n"), failing: back.failing}
	case back.report != "":
		return &outOfRounds{why: "the landing failed:\n" + back.report, failing: back.failing}
	}

	b.landed = landed
	f.end(outLanded, "", "")
	return nil
}

// fixConflicts starts why an accepted fix's landing stopped on a conflict with main, before the files in conflict.
const fixConflicts = "main changed what the fix changed, in these files:\n"

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
	role := f.monitor.role()
	a, err := b.r.agent(ctx, b.rec.Approved, role, "commit it to main, or name another agent in the monitor's entry")
	if err != nil {
		return claude.Launch{}, err
	}

	l := claude.Launch{Session: b.now().Session, Resume: resume, Prompt: prompt, Dir: string(b.tree),
		Hook: b.treeGuard().hookCommand(b.r.exe), Role: role, System: strings.TrimSpace(a.body + "\n\n" + f.contract(name))}
	l.Tools, l.Model, l.Effort, l.Advisor = a.tools, a.model, a.effort, a.advisor
	return b.sandboxed(ctx, l)
}

// kept is the forbidden action that keeps the fix's session off what it may not write.
func (f *fixing) kept() string {
	if len(f.side.acceptance) == 0 {
		return "Changing anything under .kiln/, by a file tool or a shell command."
	}

	return fmt.Sprintf("Changing anything under .kiln/, or under %s, by a file tool or a shell command.",
		strings.Join(f.side.acceptance, ", "))
}

// contract is kiln's contract with the agent of the monitor name.
func (f *fixing) contract(name Spec) string {
	tests := "The monitor says its fixes may write the acceptance tests."
	if len(f.side.acceptance) > 0 {
		tests = fmt.Sprintf("Nor is anything under %s, the acceptance tests, which judge your fix.",
			quoted(f.side.acceptance))
	}

	return fmt.Sprintf(monitorContract, name, tests)
}

const (
	checkFound    = "The monitor's check, `%s`, asks for a fix: %s\n\nIt printed:\n\n%s"
	gaugeFound    = "The monitor's gauge, %s, crossed its line over the whole repository: %s\n\nIt printed:\n\n%s"
	gaugeCrossed  = "The monitor's gauge, %s, still crosses its line over what the fix changed: %s"
	gaugeFailed   = "The monitor's gauge, %s, failed over what the fix changed. Its command, `%s`, wrote:\n\n%s"
	commandFailed = "The monitor's command, `%s`, ran, and the gate failed after it:\n\n%s"
	noPrompt      = "Fix what the monitor %s found, and end your turn."
	auditingFix   = "Audit these directories: %s. They hold the fix of the monitor %s, which has no spec. The fix " +
		"changed these files: %s."
	fixAudited = "The audit read the directories you changed. Fix each finding, or say in your reply why it is " +
		"wrong. Then end your turn.\n\n%s"
)

// gatedCheck adds a fix's gated check, or its gauge, to the gate's verdict v. The gate fails while the check asks for a
// fix, whatever it exits with. A check that couldn't run is a checkFailed.
func (b *build) gatedCheck(ctx context.Context, v verdict) (verdict, error) {
	switch {
	case b.fix == nil || !b.fix.monitor.gated():
		return v, nil
	case b.fix.monitor.Gauge != "":
		return b.gatedGauge(ctx, v)
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

// gatedGauge adds the reading of a fix's gauge, over what the fix changed, to the gate's verdict v. The gate fails
// while the reading crosses the gauge's line, or its command fails. A reading of no number never crossed. The gauge's
// command and line are kiln.json's at main's tip, which may no longer name the gauge.
func (b *build) gatedGauge(ctx context.Context, v verdict) (verdict, error) {
	main, err := b.r.mainTip(ctx)
	if err != nil {
		return v, err
	}

	c, err := b.r.config(ctx, main)
	if err != nil {
		return v, err
	}

	g, found := c.gaugeNamed(b.fix.monitor.Gauge)
	if !found {
		return v, nil
	}

	rd, err := b.readFix(ctx, g)
	if err != nil {
		return v, err
	}

	e := entry{Gauge: g.Name, Value: rd.value, gaugeLine: g.gaugeLine}
	var report string
	switch {
	case rd.failed:
		report = fmt.Sprintf(gaugeFailed, g.Name, g.Command, lastLines(rd.both, 10))
	case !g.crossed(rd.value):
		return v, nil
	default:
		report = strings.TrimSpace(fmt.Sprintf(gaugeCrossed, g.Name, shownReading(e)) + "\n\n" + rd.found)
	}

	v.passed = false
	v.report = strings.TrimSpace(v.report + "\n\n" + report)
	v.failing = append(v.failing, "`"+g.Command+"`")
	return v, nil
}

// readFix reads g over what the fix changed, at its commit. It reads in the monitor's check's worktree, never the
// fix's, so a file the gauge leaves never lands. It answers an error only once kiln is interrupted, or git fails.
func (b *build) readFix(ctx context.Context, g gauge) (gaugeRead, error) {
	head, base, err := b.fixBase(ctx)
	if err != nil {
		return gaugeRead{}, err
	}

	tree, err := b.r.scratchTree(ctx, checkTree, head)
	if err != nil {
		return gaugeRead{}, err
	}

	return b.r.readGauge(ctx, tree, g.Command, base)
}
