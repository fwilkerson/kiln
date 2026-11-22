package kiln

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
)

// A monitor's fix is a build with no spec, on kiln/<name>, in the monitor's worktree:
//
//	command, then commit and gate (+ check) ── pass ──────────────────────────┐
//	  │ fail, or no command                                                   │
//	  ▼                                                                       ▼
//	agent, then commit and gate (+ check), up to rounds ── pass ─▶ audit ─▶ unchanged, waits, or lands
//	  │ asks for the developer ─▶ needs you    │ fails ─▶ session failed    │ usage limit ─▶ usage limit
//
// Work that changes nothing ends unchanged, with no audit. Only an agent's work gets the audit.
//
// A gated check that couldn't run ends the fix couldn't run. A gauge in the gate whose command fails goes back to the
// agent, as a crossed reading does. One whose command ran past kiln.json's timeout stops the fix. Past the last
// round, or on a command alone that the gate fails, it stops. An accepted fix's landing sends each setback to the
// agent's session, through the same loop. Each ending but landed stops it.

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
	// side is what the fix's session that works now may write. Its role is verifyRole while a verify-first fix's
	// verify session works.
	side side
	// tested is what the fix session's first message says of a verify-first fix's proved tests.
	tested string
	// outcome, summary and report say how the fix ended, once it has.
	outcome, summary, report string
	// answering is true while the session answers a landing audit's findings. It ends once the work passes the gate,
	// or a landing's setback goes back.
	answering bool
	// since lists the files changed since kiln accept, for the landing commit's body.
	since []string
}

// newFixing is the fix of m, as c, kiln.json at main's tip when it started, says. found is what its check found. A
// verify-first fix's sides tell the tests apart by kiln.json's testFiles, its testHelpers and its acceptance paths.
func newFixing(c config, m monitor, found checked) *fixing {
	fixes := side{role: fixRole}
	switch {
	case m.Workflow == verifyFirst:
		fixes.tests, fixes.acceptance, fixes.helpers = c.TestFiles, c.fixedParts(), c.TestHelpers
	case !m.WritesTests:
		fixes.acceptance = c.fixedParts()
	}

	return &fixing{monitor: m, checked: found, side: fixes}
}

// end ends the fix with outcome.
func (f *fixing) end(outcome, summary, report string) {
	f.outcome, f.summary, f.report = outcome, summary, report
}

// fixOf reads the fix of rec: its monitor, as its directory stood when the fix started.
func (r *Repo) fixOf(ctx context.Context, rec record) (*fixing, error) {
	c, reads, err := r.monitors(ctx, rec.Approved)
	if err != nil {
		return nil, err
	}

	m, found, err := monitorNamed(reads, rec.Name)
	switch {
	case err != nil:
		return nil, err
	case !found:
		return nil, fmt.Errorf("%s at %s holds no monitor %s", monitorsDir, short(rec.Approved), rec.Name)
	}

	return newFixing(c, m, checked{}), nil
}

// checkFailed is a gated check that couldn't run. line is its last line of output.
type checkFailed struct {
	line string
}

func (c *checkFailed) Error() string { return "the monitor's check couldn't run: " + c.line }

// errLimitEnded is a fix's session that a usage limit ended. kiln never waits for the limit to reset, since it would
// hold the monitors' lane until then.
var errLimitEnded = errors.New("a usage limit ended the session")

// startFix keeps the record of the fix f, and answers its build. The fix builds from main's tip, as the monitor line
// says. A stopped fix of its monitor gives way to it.
func (r *Repo) startFix(out io.Writer, f *fixing, line entry) (*build, error) {
	m := f.monitor
	rec := record{Name: m.Name, State: building, Approved: line.Main, Build: line.Build, Monitor: true,
		PID: os.Getpid()}
	err := r.store.update(func(b *book) error {
		if stopped := b.find(m.Name); stopped != nil {
			*stopped = rec
			return nil
		}

		b.Specs = append(b.Specs, &rec)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &build{r: r, rec: rec, tree: r.tree(m.Name), out: out, fix: f}, nil
}

// fixRun runs the fix of b, whose record the book holds, until it ends.
func (b *build) fixRun(ctx context.Context) error {
	m := b.fix.monitor
	b.record(entry{Kind: builtKind, Spec: m.Name, Approved: b.rec.Approved, Monitor: m.Name})
	return b.fixSettle(ctx, b.fixWork(ctx))
}

// fixWork runs the fix: the command first, then the agent when the command left the gate failing. A verify-first fix
// runs its verify session first, and its agent from the tests kiln proved.
func (b *build) fixWork(ctx context.Context) error {
	name, m, from := b.rec.Name, b.fix.monitor, b.rec.Approved
	if m.Workflow == verifyFirst {
		tested, err := b.writeTests(ctx)
		if err != nil || tested == "" {
			return err
		}

		from = tested
	}

	if err := b.r.git.Checkout(ctx, b.tree, name.branch(), from); err != nil {
		return err
	}

	b.say("fixing on %s, in %s", name.branch(), b.tree)
	if !m.command {
		return b.fixAgent(ctx, "")
	}

	v, err := b.commandGate(ctx)
	switch {
	case err != nil:
		return err
	case v.passed:
		_, _, err := b.fixAfter(ctx)
		return err
	case !m.agent:
		return &outOfRounds{stop{Why: "the gate failed after the monitor's command:\n" + v.report, Failing: v.failing}}
	}

	return b.fixAgent(ctx, v.report)
}

// commandGate runs the monitor's command in the fix's worktree, outside the sandbox, then commits and gates its work.
// A command that ran past kiln.json's timeout stops the fix. Its half-done work is no work to gate.
func (b *build) commandGate(ctx context.Context) (verdict, error) {
	_, c, err := b.r.mainConfig(ctx)
	if err != nil {
		return verdict{}, err
	}

	command := b.fix.monitor.path(commandScript)
	out, err := c.shell(b.r.env).run(ctx, b.tree, command)
	if ctx.Err() != nil {
		return verdict{}, ctx.Err()
	}

	if _, past := errors.AsType[*ranPast](err); past {
		return verdict{}, fmt.Errorf("the monitor's command %q %w", command, err)
	}

	if err != nil {
		b.say("the command failed, %v; the gate judges its work:\n%s", err, indent(lastLines(out.both, 10)))
	}

	head, err := b.r.git.Commit(ctx, b.tree, b.message("the command's work"))
	if err != nil {
		return verdict{}, err
	}

	return b.gate(ctx, head)
}

// lastLines is the last n lines of text.
func lastLines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}

// fixAgent runs the monitor's agent until the fix ends. gateReport is what the gate printed after the command, or "".
func (b *build) fixAgent(ctx context.Context, gateReport string) error {
	b.set(func(r *record) { r.Session = claude.NewID() })
	return b.fixTurns(ctx, b.fix.prompt(gateReport), reason{Why: whyFirst})
}

// fixTurns tells the agent's session prompt, for why. kiln commits and gates each turn's work, and tells the session
// what goes back, until nothing does. A prompt of "" tells it nothing.
func (b *build) fixTurns(ctx context.Context, prompt string, why reason) error {
	for prompt != "" {
		l, err := b.fixLaunch(ctx, prompt)
		if err != nil {
			return err
		}

		ended, err := b.fixTalk(ctx, l, why)
		if err != nil || b.fix.heard(ended, b.r.claude.Stderr(l.Session)) {
			return err
		}

		// A landing keeps the title of the fix the developer accepted.
		if title := titleOf(ended.Text); title != "" && !b.rec.Accepted {
			b.set(func(r *record) { r.Title = title })
		}

		if prompt, why, err = b.fixGate(ctx); err != nil {
			return err
		}
	}

	return nil
}

// fixTalk runs the session l launches, as talk does. In a landing, the record marks the session's turn running
// until the turn ends, with what l tells it. An interrupt, a killed kiln or a usage limit leaves the mark, for landFix.
func (b *build) fixTalk(ctx context.Context, l claude.Launch, why reason) (claude.Ended, error) {
	if !b.rec.Accepted {
		return b.talk(ctx, l, why)
	}

	from, _ := b.r.claude.Streamed(l.Session)
	b.set(func(r *record) { r.Running, r.Telling, r.From = []string{l.Role}, l.Prompt, from })
	ended, err := b.talk(ctx, l, why)
	if err == nil {
		b.set(func(r *record) { r.Running, r.Telling, r.From = nil, "", 0 })
	}

	return ended, err
}

// prompt is the agent's first message. It holds what the check or the gauge printed, and what the gate printed after
// the command. A line that sends the agent to its instructions follows.
func (f *fixing) prompt(gateReport string) string {
	m := f.monitor
	var parts []string
	if m.finds() {
		found := cmpOr(f.checked.found, "(nothing)")
		parts = append(parts, fmt.Sprintf(foundBy[m.finder], m.path(m.finder), f.checked.summary, found))
	}

	if gateReport != "" {
		parts = append(parts, fmt.Sprintf(commandFailed, m.path(commandScript), gateReport))
	}

	if f.tested != "" {
		parts = append(parts, f.tested)
	}

	parts = append(parts, fmt.Sprintf(fixWhat, m.Name))
	return strings.Join(parts, "\n\n")
}

// foundBy is how the agent's first message says what the monitor's check or gauge found.
var foundBy = map[string]string{
	checkScript: "The monitor's check, `%s`, asks for a fix: %s\n\nIt printed:\n\n%s",
	gaugeScript: "The monitor's gauge, `%s`, crossed its line over the whole repository: %s\n\nIt printed:\n\n%s",
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

// titleOf reads the title an agent's reply gives the commit that lands its fix. Its last line is {"title": "..."}.
// It answers "" unless the title is one line, type(scope): summary, of at most titleMax characters.
func titleOf(text string) string {
	last, _ := lastOf(text)
	var t struct {
		Title string `json:"title"`
	}
	if !jsonLine(last, &t) || !conventionalTitle(t.Title) {
		return ""
	}

	return t.Title
}

// conventionalTitle is true of a title of at most titleMax characters, as Conventional Commits writes one.
func conventionalTitle(title string) bool {
	return utf8.RuneCountInString(title) <= titleMax && conventional.MatchString(title)
}

// titleMax is the longest title titleOf takes, the limit Conventional Commits' common tooling keeps.
const titleMax = 72

// conventional matches a Conventional Commits title: type(scope): summary, where the scope and a ! are optional.
var conventional = regexp.MustCompile(`^[a-z]+(\([a-z0-9][a-z0-9./_-]*\))?!?: \S[^\n]*$`)

// jsonLine reads line into v, if it is a JSON object.
func jsonLine(line string, v any) bool {
	return strings.HasPrefix(line, "{") && json.Unmarshal([]byte(line), v) == nil
}

// fixGate commits what the agent's session left, gates it, and takes the fix on from the verdict. It answers what to
// tell the session next, and why, or "" once the fix has ended. A landing whose work changes nothing on main stops
// before the gate, which could only send it back.
func (b *build) fixGate(ctx context.Context) (string, reason, error) {
	if _, back, err := b.sessionWork(ctx); err != nil || back.why != "" {
		return b.roundFor(back, err)
	}

	if b.rec.Accepted {
		if _, changed, err := b.fixChanged(ctx); err != nil || !changed {
			return "", reason{}, cmpOr(err, error(&outOfRounds{stop{Why: nothingToLand}}))
		}
	}

	v, err := b.gate(ctx, "")
	switch {
	case err != nil:
		return "", reason{}, err
	case !v.passed:
		return b.round(setback{why: whyGate, report: v.report, failing: v.failing})
	}

	return b.fixAfter(ctx)
}

// fixAfter takes a fix on once its gate passed. Work that changes nothing ends unchanged. An agent's work gets the
// audit. Then a fix of a monitor with "mustAccept": true waits on its branch, until the developer accepts it. Any other
// lands, as does an accepted fix's landing. A setback goes back to the session, or stops a fix with none.
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
		findings, why, err := b.fixFindings(ctx)
		switch {
		case err != nil:
			return "", reason{}, err
		case findings != "":
			return b.sendFindings(ctx, findings, why)
		}
	}

	if b.fix.monitor.MustAccept && !b.rec.Accepted {
		b.fix.end(outWaits, "", "")
		return "", reason{}, nil
	}

	return b.fixLand(ctx, head)
}

// nothingToLand is why a landing stopped, once the fix's work changes nothing on main.
const nothingToLand = "the work changes nothing on main, so nothing lands"

// fixFindings runs the audit of the fix, as findings does. A landing's audit reads what changed since kiln accept. It
// runs once the work's own change outside .kiln/ differs from what the audit last read. It spends no round. The
// session's answer to its findings moves Audited with no audit, but for a path kiln.json's reaudit names.
func (b *build) fixFindings(ctx context.Context) (string, reason, error) {
	if !b.rec.Accepted {
		return b.findings(ctx)
	}

	head, err := b.r.git.Head(ctx, b.tree)
	if err != nil {
		return "", reason{}, err
	}

	answer := b.fix.answering
	b.fix.answering = false
	unread, err := b.unread(ctx, head)
	findings, why := "", reason{Why: whyFindings}
	switch {
	case err != nil:
		return "", reason{}, err
	case answer:
		// The guide says not to audit the fix for an audit finding.
		findings, why, err = b.findings(ctx)
	case unread:
		findings, err = b.audit(ctx)
	}

	if err != nil {
		return "", reason{}, err
	}

	if answer {
		b.set(func(r *record) { r.Audited = head })
	}

	b.fix.answering = findings != ""
	return findings, why, nil
}

// unread is whether the work at head changed a file outside .kiln/ since the commit the audit last read.
func (b *build) unread(ctx context.Context, head git.Rev) (bool, error) {
	changed, err := b.r.changedSince(ctx, b.rec.Audited, head)
	return slices.ContainsFunc(changed, outsideKiln), err
}

// outsideKiln is whether the path p lies outside .kiln/.
func outsideKiln(p string) bool {
	return !strings.HasPrefix(p, kilnDir+"/")
}

// fixLand lands the fix at head. A setback goes back to the session, or stops a fix with none. A failed landing
// gate goes back with main merged in, as fixMergeMain says. Work that changes nothing on main stops a landing.
func (b *build) fixLand(ctx context.Context, head git.Rev) (string, reason, error) {
	if err := b.sinceAccept(ctx, head); err != nil {
		return "", reason{}, err
	}

	landed, back, err := b.land(ctx, head, nil)
	switch {
	case err != nil:
		return "", reason{}, err
	case back.report == "":
		b.landed = landed
		b.fix.end(outLanded, "", "")
		return "", reason{}, nil
	case b.rec.Session == "":
		return "", reason{}, aloneStops(back)
	case back.why == whyUnchanged && b.rec.Accepted:
		return "", reason{}, &outOfRounds{stop{Why: nothingToLand}}
	case back.why == whyLanding:
		if back, err = b.fixMergeMain(ctx, back); err != nil {
			return "", reason{}, err
		}
	}

	b.fix.answering = false
	return b.round(back)
}

// aloneStops is the stop of a fix with no session, which back kept from landing. No session resolves a conflict.
func aloneStops(back setback) error {
	if back.why == whyConflict {
		return conflictStop(fixConflicts, back.failing)
	}

	return &outOfRounds{stop{Why: "the landing failed:\n" + back.report, Failing: back.failing}}
}

// conflictStop stops a fix on a conflict no session may resolve. why starts its words, before files, the files in
// conflict. kiln status then names where the work is, and what to run next.
func conflictStop(why string, files []string) error {
	return &outOfRounds{stop{Why: why + strings.Join(files, "\n"), Failing: files, Kind: stopConflict}}
}

// fixMergeMain merges main into the fix's branch, before back, a failed landing gate, goes back to the session. land
// put the branch back on main as it was. Without the merge, only the landing's gate would see what main gained. A
// merge that conflicts goes back as a conflict instead.
func (b *build) fixMergeMain(ctx context.Context, back setback) (setback, error) {
	conflict, err := b.r.git.Merge(ctx, b.tree, mainBranch, b.message("merge main"))
	if err != nil || conflict == nil {
		return back, err
	}

	return setback{whyConflict, fmt.Sprintf(conflicts, strings.Join(conflict.Files, "\n")), conflict.Files}, nil
}

// sinceAccept reads the files changed since kiln accept into the fix, for the landing commit's body, in a landing.
func (b *build) sinceAccept(ctx context.Context, head git.Rev) error {
	if !b.rec.Accepted {
		return nil
	}

	since, err := b.r.changedSince(ctx, b.rec.AcceptedAt, head)
	b.fix.since = since
	return err
}

// fixChanged answers the fix's commit, and whether it changes anything since it left main.
func (b *build) fixChanged(ctx context.Context) (head git.Rev, changed bool, err error) {
	if head, err = b.r.git.Head(ctx, b.tree); err != nil {
		return "", false, err
	}

	main, err := b.r.mainTip(ctx)
	if err != nil {
		return "", false, err
	}

	paths, err := b.r.git.Branched(ctx, main, head)
	return head, len(paths) > 0, err
}

// landFix lands a fix the developer accepted. It is rebased onto main, and gated again, check included. Then main
// moves onto it if main hasn't moved. A conflict or a failed gate goes back to the fix's session, as fixLand says.
// It stops a fix with none. kiln accept tries a stopped landing again, and kiln reject drops it.
func (b *build) landFix(ctx context.Context) error {
	f, err := b.r.fixOf(ctx, b.rec)
	if err != nil {
		return err
	}

	b.fix = f
	if err := b.unland(ctx); err != nil {
		return err
	}

	if err := b.acceptedBefore(ctx); err != nil {
		return err
	}

	rec := b.now()
	if rec.Session != "" && len(rec.Running) > 0 {
		// A turn kiln cut short may have left a merge half done. kiln never commits that as the session's work.
		return b.fixTurns(ctx, b.cutShort(rec), reason{Why: whyInterrupted, Round: rec.Rounds})
	}

	next, why, err := b.fixLanding(ctx)
	if err != nil {
		return err
	}

	return b.fixTurns(ctx, next, why)
}

// cutShort is what kiln tells the session of rec, a landing whose turn kiln cut short. A turn that never began gets
// the message it was told, since the session never read it. Any other goes on where it stopped.
func (b *build) cutShort(rec record) string {
	streamed, err := b.r.claude.Streamed(rec.Session)
	if err == nil && streamed == rec.From {
		return rec.Telling
	}

	return interrupted
}

// acceptedBefore gives an accepted commit to a landing that stopped before kiln kept one. It takes the branch's tip,
// where the stopped landing left it. The audit then last read that commit too. So the audit and the commit's line
// both read what changed since kiln accept from that one commit.
func (b *build) acceptedBefore(ctx context.Context) error {
	if b.rec.AcceptedAt != "" {
		return nil
	}

	tip, err := b.r.git.Head(ctx, b.tree)
	if err != nil {
		return err
	}

	b.set(func(r *record) { r.AcceptedAt, r.Audited = tip, tip })
	return nil
}

// fixLanding commits what the fix's worktree holds, which a session the developer resumed there may have left. Work
// the audit has read lands. Any other is gated and audited first. It answers what goes back to the session, and why.
func (b *build) fixLanding(ctx context.Context) (string, reason, error) {
	head, back, err := b.sessionWork(ctx)
	if err != nil || back.why != "" {
		return b.roundFor(back, err)
	}

	unread, err := b.unread(ctx, head)
	switch {
	case err != nil:
		return "", reason{}, err
	case unread && b.rec.Session != "":
		return b.fixGate(ctx)
	}

	return b.fixLand(ctx, head)
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
		b.set(func(r *record) { r.stop = stopped.stop })
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, errLimitEnded):
		f.end(outLimit, errLimitEnded.Error(), "")
	case isCheck:
		f.end(outCouldNot, failed.line, "")
	case err != nil:
		f.end(outStopped, "", "")
		b.set(func(r *record) { r.stop = kilnStop(err) })
	}

	return b.fixEnded(ctx)
}

// landingStops answers err as a stop for an accepted fix's landing, which only lands or stops. So no fix the
// developer accepted is lost, as one that couldn't run would be. A session that asked for the developer, or failed,
// stops it with its summary and report. An interrupt stays as it is.
func (b *build) landingStops(ctx context.Context, err error) error {
	if !b.now().Accepted || ctx.Err() != nil {
		return err
	}

	_, isStop := errors.AsType[*outOfRounds](err)
	switch {
	case isStop:
		return err
	case err != nil:
		return &outOfRounds{stop{Why: "the landing failed: " + err.Error()}}
	case b.fix.sessionEnded():
		return &outOfRounds{stop{Why: strings.TrimSpace(b.fix.summary + "\n" + b.fix.report)}}
	}

	return nil
}

// sessionEnded reports whether the agent's session ended the fix f, by asking for the developer or failing. f may be
// nil.
func (f *fixing) sessionEnded() bool {
	return f != nil && slices.Contains([]string{outNeedsYou, outFailed}, f.outcome)
}

// fixEnded records how the fix ended, and prints it with the fix's time, cost, turns and failed calls. A stopped fix
// keeps its record and its worktree, and a fix that passed waits on its branch. Any other leaves no record, branch or
// worktree.
func (b *build) fixEnded(ctx context.Context) error {
	f := b.fix
	switch f.outcome {
	case outLanded:
		// A check's finding no longer holds once the fix landed, so the landing's title sums the fix up. A gauge's
		// reading of main stays the summary, as Monitors says.
		if f.monitor.finder != gaugeScript {
			f.summary = b.landingTitle()
		}
	case outStopped:
		f.summary, f.report = f.checked.summary, b.now().Why
		if f.summary == "" {
			// The summary takes the first line of why, so the report keeps the rest, and no line shows twice.
			f.summary, f.report, _ = strings.Cut(f.report, "\n")
		}
	}

	e := entry{Kind: outcomeKind, Outcome: f.outcome, Summary: f.summary, Report: f.report}
	var err error
	switch f.outcome {
	case outStopped:
		b.record(e)
		b.set(func(r *record) { r.State, r.runner = stopped, runner{} })
	case outWaits:
		b.record(e)
		b.set(func(r *record) { r.State, r.runner = waiting, runner{} })
	default:
		err = b.r.end(ctx, b.now(), e, b.out)
	}

	b.say("%s", cmpOr(strings.TrimSuffix(f.outcome+": "+f.summary, ": "), "ended"))
	// A spec's build prints the same line once it ends, so a fix's time and cost read alike.
	fmt.Fprintln(b.out, indent(b.r.summary(b.rec.Build)))
	return err
}

// fixLaunch is a run of the monitor's agent's session, in the fix's worktree. Its hook keeps it off .kiln/, and off
// the tests the monitor's fix may not write.
func (b *build) fixLaunch(ctx context.Context, prompt string) (claude.Launch, error) {
	l := claude.Launch{Session: b.now().Session, Prompt: prompt, System: b.fix.contract(b.rec.Name)}
	return b.monitorLaunch(ctx, monitorAgent, b.treeGuard(), l)
}

// monitorLaunch is l, a run of a session from file, an agent's file of the fix's monitor, in g's place. Its system
// prompt is the file's body, as the fix's start read it, then l's own, kiln's contract. Its hook asks g.
func (b *build) monitorLaunch(ctx context.Context, file string, g guard, l claude.Launch) (claude.Launch, error) {
	a, err := b.r.filesAt(b.rec.Approved).agent(ctx, b.fix.monitor.path(file), "commit it to main")
	if err != nil {
		return claude.Launch{}, err
	}

	l.Dir, l.Hook, l.Role = string(g.place), g.hookCommand(b.r.exe), g.side.role
	l.System = strings.TrimSpace(a.body + "\n\n" + l.System)
	l.Tools, l.Model, l.Effort, l.Advisor = a.tools, a.model, a.effort, a.advisor
	return b.sandboxed(ctx, l)
}

// kept is the forbidden action that keeps role's session of the fix off what it may not write.
func (f *fixing) kept(role string) string {
	switch {
	case role == verifyRole:
		return "Changing anything under .kiln/, or any file but a test file, by a file tool or a shell command."
	case f.side.verifiesFirst():
		return "Changing anything under .kiln/, or any test file, by a file tool or a shell command."
	case len(f.side.acceptance) == 0:
		return "Changing anything under .kiln/, by a file tool or a shell command."
	}

	return fmt.Sprintf("Changing anything under .kiln/, or under %s, by a file tool or a shell command.",
		strings.Join(f.side.acceptance, ", "))
}

// contract is kiln's contract with the agent of the monitor name.
func (f *fixing) contract(name Spec) string {
	tests := "The monitor says its fixes may write the acceptance tests."
	switch {
	case f.side.verifiesFirst():
		tests = fmt.Sprintf("Nor is any test file: %s. A verify session wrote the tests that judge your fix. It "+
			"updated each old test that held the behavior your fix changes.", f.side.testWords())
	case len(f.side.acceptance) > 0:
		tests = fmt.Sprintf("Nor is anything under %s, the acceptance tests, which judge your fix.",
			quoted(f.side.acceptance))
	}

	gauge := ""
	if f.monitor.finder == gaugeScript && f.monitor.gated() {
		gauge = " The gate also reads the monitor's gauge over what the fix changed, and fails while that reading " +
			"crosses the gauge's line."
	}

	return fmt.Sprintf(monitorContract, name, tests, gauge)
}

const (
	gaugeCrossed  = "The monitor's gauge, %s, still crosses its line over what the fix changed: %s"
	gaugeFailed   = "The monitor's gauge, %s, failed over what the fix changed. Its command, `%s`, wrote:\n\n%s"
	commandFailed = "The monitor's command, `%s`, ran, and the gate failed after it:\n\n%s"
	fixWhat       = "Fix what the monitor %s found, as your instructions say, and end your turn."
	auditingFix   = "Audit these directories: %s. They hold the fix of the monitor %s, which has no spec. The fix " +
		"changed these files: %s."
	auditingLanding = "Audit these directories: %s. They hold the fix of the monitor %s, which the developer " +
		"accepted. Main moved since, and these files changed after kiln accept: %s."
	fixAudited = "The audit read the directories you changed. Fix each finding, or say in your reply why it is " +
		"wrong. Then end your turn.\n\n%s"
)

// gatedCheck adds a fix's gated check, or its gauge, to the gate's verdict v. The gate fails while the check asks for a
// fix, whatever it exits with. A check that couldn't run is a checkFailed. Each runs as kiln.json at main's tip says.
// Each runs in checkTree, at the commit the gate ran on.
func (b *build) gatedCheck(ctx context.Context, held commands, v verdict) (verdict, error) {
	switch {
	case b.fix == nil || !b.fix.monitor.gated():
		return v, nil
	case b.fix.verifying():
		// A verify-first fix's tests must fail the gate before any fix, so its check, which asks for one, waits.
		return v, nil
	case b.fix.monitor.finder == gaugeScript:
		return b.gatedGauge(ctx, held, v)
	}

	_, c, err := b.r.mainConfig(ctx)
	if err != nil {
		return v, err
	}

	tree, err := held.scratchTree(ctx, checkTree, v.head)
	if err != nil {
		return v, err
	}

	command := b.fix.monitor.path(checkScript)
	found, err := c.shell(b.r.env).runMonitorCheck(ctx, tree, command)
	switch {
	case err != nil:
		return v, err
	case !found.ran:
		return v, &checkFailed{line: found.summary}
	case !found.fix:
		return v, nil
	}

	report := fmt.Sprintf("The monitor's check, `%s`, still asks for a fix: %s", command, found.summary)
	if found.found != "" {
		report += "\n\n" + found.found
	}

	v.passed = false
	v.report = strings.TrimSpace(v.report + "\n\n" + report)
	v.failing = append(v.failing, "`"+command+"`")
	return v, nil
}

// gatedGauge adds the reading of a fix's gauge, over what the fix changed, to the gate's verdict v. The gate fails
// while the reading crosses the gauge's line, or the gauge fails. A reading of no number never crossed. The gauge's
// line is its monitor's at main's tip, where the monitor may hold the gauge no longer. The gauge is the fix commit's,
// and runs as kiln.json at main's tip says. A gauge that ran past kiln.json's timeout stops the fix, as the gate's own
// commands do.
func (b *build) gatedGauge(ctx context.Context, held commands, v verdict) (verdict, error) {
	main, c, reads, err := b.r.mainMonitors(ctx)
	if err != nil {
		return v, err
	}

	m, found, err := monitorNamed(reads, b.fix.monitor.Name)
	switch {
	case err != nil:
		return v, err
	case !found || m.finder != gaugeScript:
		return v, nil
	}

	// The gauge reads the fix's change in the check's worktree, so a file the gauge leaves never lands.
	base, err := b.r.git.MergeBase(ctx, main, v.head)
	if err != nil {
		return v, err
	}

	tree, err := held.scratchTree(ctx, checkTree, v.head)
	if err != nil {
		return v, err
	}

	rd, err := c.shell(b.r.env).readGauge(ctx, tree, m, base)
	switch {
	case err != nil:
		return v, err
	case rd.past != nil:
		return v, fmt.Errorf("the monitor's gauge %s: its command %w", m.Name, rd.past)
	}

	command := m.path(gaugeScript)
	var report string
	switch {
	case rd.failed:
		report = fmt.Sprintf(gaugeFailed, m.Name, command, lastLines(rd.both, 10))
	case !m.line().crossed(rd.value):
		return v, nil
	default:
		report = strings.TrimSpace(fmt.Sprintf(gaugeCrossed, m.Name, shownReading(m.reading(rd.value))) + "\n\n" +
			rd.found)
	}

	v.passed = false
	v.report = strings.TrimSpace(v.report + "\n\n" + report)
	v.failing = append(v.failing, "`"+command+"`")
	return v, nil
}
