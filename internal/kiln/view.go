package kiln

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"syscall"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/tui"
)

// TUI shows kiln's builds in a full-screen view, live and past, until the developer quits. It opens on the spec
// name, or on home when name is "". It reads what kiln run writes, takes no lock, and writes nothing. It refuses an
// out that is not a terminal.
func (r *Repo) TUI(ctx context.Context, name Spec, in, out *os.File) error {
	return tui.Show(ctx, view{r}, string(name), tui.Terminal{In: in, Out: out})
}

// view reads the book, the ledger and the sessions' streams for the TUI.
type view struct {
	r *Repo
}

// Specs answers each spec in the book. A spec the ledger holds and the book no longer does was accepted or
// rejected. Its last build's outcome says which. The newest build comes first, and a spec with no build yet before
// them all. A monitor's fixes are no spec's builds, and Monitors answers them.
func (v view) Specs(context.Context) ([]tui.Spec, error) {
	b, err := v.r.store.read()
	if err != nil {
		return nil, err
	}

	entries, err := v.r.store.entries()
	if err != nil {
		return nil, err
	}

	var l lineup
	l.records(b)
	for _, s := range collect(entries) {
		l.build(b, s)
	}

	shown := make([]tui.Spec, len(l.specs))
	for i, s := range l.specs {
		shown[i] = *s
	}

	slices.SortStableFunc(shown, newestFirst)
	return shown, nil
}

// lineup is the specs the view shows, in the order Specs met them.
type lineup struct {
	specs []*tui.Spec
	named map[Spec]*tui.Spec
}

// records adds each spec b holds a record of, where it stands. A monitor's fix is no spec.
func (l *lineup) records(b book) {
	for _, rec := range b.Specs {
		if !rec.Monitor {
			l.spec(rec.Name).State = stateOf(rec)
		}
	}
}

// build adds the build s to its spec. A spec b no longer holds was accepted or rejected. A fix is no spec's build.
func (l *lineup) build(b book, s *buildStats) {
	if s.Monitor != "" {
		return
	}

	spec := l.spec(s.Spec)
	if b.find(s.Spec) == nil {
		spec.State = s.gone()
	}

	spec.Builds = append(spec.Builds, s.shown())
}

// spec is the lineup's spec named name. It adds one, with no state and no build, if the lineup holds none.
func (l *lineup) spec(name Spec) *tui.Spec {
	if s := l.named[name]; s != nil {
		return s
	}

	s := &tui.Spec{Name: string(name)}
	if l.named == nil {
		l.named = map[Spec]*tui.Spec{}
	}

	l.named[name] = s
	l.specs = append(l.specs, s)
	return s
}

// newestFirst orders a spec with no build before every other, then the spec whose last build started last.
func newestFirst(a, b tui.Spec) int {
	if len(a.Builds) == 0 || len(b.Builds) == 0 {
		return len(a.Builds) - len(b.Builds)
	}

	return b.Builds[len(b.Builds)-1].Started.Compare(a.Builds[len(a.Builds)-1].Started)
}

// Recount reads the run's bytes of its session's stream.
func (v view) Recount(r *tui.Run) (claude.Recounted, error) {
	return v.r.claude.Recount(r.Session, r.From, r.To)
}

// Text reads the spec as the build's approval held it. A spec main no longer holds shows as the build saw it. Its
// amendments follow it. A fix has no spec, so its text is how it ended.
func (v view) Text(ctx context.Context, b *tui.Build) (string, error) {
	if b.Fix {
		return fixText(b), nil
	}

	spec, found, err := v.r.specAt(ctx, git.Rev(b.Approved), Spec(b.Spec))
	switch {
	case err != nil:
		return "", err
	case !found:
		return "", fmt.Errorf("the commit %s holds no %s", short(git.Rev(b.Approved)), Spec(b.Spec).file())
	case b.Amendments == "":
		return string(spec), nil
	}

	return string(spec) + "\n\n---\n\n" + b.Amendments, nil
}

// fixText is how the fix b ended, as Markdown.
func fixText(b *tui.Build) string {
	var text strings.Builder
	fmt.Fprintf(&text, "# The fix of the monitor %s\n\nFrom main at %s: %s.", b.Spec, short(git.Rev(b.Approved)),
		cmpOr(fixResult(b.Outcome), "fix building"))
	for _, part := range []string{b.Summary, b.Report} {
		if part != "" {
			text.WriteString("\n\n" + part)
		}
	}

	return text.String()
}

// Monitors answers each monitor kiln.json names at main's tip, where it stands, and its fixes, oldest first. A
// monitor that names a gauge holds its readings of the whole repository, oldest first.
func (v view) Monitors(ctx context.Context) ([]tui.Spec, error) {
	main, err := v.r.git.Tip(ctx, mainBranch)
	if err != nil {
		return nil, err
	}

	// A main with no kiln.json kiln can read names no monitor, and the specs still show.
	c, _ := v.r.config(ctx, main)
	entries, err := v.r.store.entries()
	if err != nil {
		return nil, err
	}

	fixes := map[Spec][]*tui.Build{}
	for _, s := range collect(entries) {
		if s.Monitor != "" {
			fixes[s.Monitor] = append(fixes[s.Monitor], s.shown())
		}
	}

	all, now := histories(entries), v.r.now()
	shown := make([]tui.Spec, len(c.Monitors))
	for i, m := range c.Monitors {
		h := all[m.Name]
		s := h.standing(m)
		shown[i] = tui.Spec{Name: string(m.Name), State: s.state(), Builds: fixes[m.Name],
			Monitor: &tui.Monitor{Result: s.result, Summary: s.summary, Last: s.last, Next: s.next, Due: h.due(m, now),
				Readings: repositoryReadings(entries, m)}}
	}

	return shown, nil
}

// repositoryReadings are the readings of m's gauge in m's monitor lines, as kiln wrote them. A line from before m
// named that gauge names none, and is no reading.
func repositoryReadings(entries []entry, m monitor) []tui.Reading {
	if m.Gauge == "" {
		return nil
	}

	var readings []tui.Reading
	for _, e := range entries {
		if e.Kind != monitorKind || e.Monitor != m.Name {
			continue
		}

		if e.Gauge == m.Gauge {
			readings = append(readings, e.tuiReading())
		}
	}

	return readings
}

// tuiReading is the gauge's reading of the line e, as the TUI shows it.
func (e entry) tuiReading() tui.Reading {
	return tui.Reading{Gauge: e.Gauge, At: e.At, Value: e.Value, Line: e.text(), Crossed: e.crossed(e.Value)}
}

// fixStates are a monitor's states in the view, by its last fix's outcome. A fix with no outcome yet is running.
var fixStates = map[string]tui.State{outLanded: tui.Landed, outStopped: tui.Stopped, outWaits: tui.Waits,
	outNeedsYou: tui.NeedsYou, outFailed: tui.Failed, outLimit: "usage limit", outCouldNot: foundCouldNot,
	outUnchanged: outUnchanged, outRejected: tui.Rejected, outDropped: "cut short", "": tui.Running}

// state is where the monitor of s stands, as the view words it. It stands as its last fix ended, or as its check
// found. A held monitor stands where the fix that holds it ended.
func (s standing) state() tui.State {
	if s.fixed {
		return fixStates[s.outcome]
	}

	return tui.State(s.result)
}

// stateOf is where a spec stands. A build is building only while the kiln run that marked it so lives. A kiln run
// stopped by an interrupt, or killed, leaves the book saying building. A queued build waits for a kiln run, which may
// be running already.
func stateOf(rec *record) tui.State {
	switch {
	case rec.State == building && !alive(rec.PID):
		return tui.Halted
	case rec.State == building && !rec.Paused.IsZero():
		return tui.Paused
	}

	return map[state]tui.State{queued: tui.Queued, building: tui.Running, stopped: tui.Stopped, landed: tui.UAT,
		amended: tui.Waits}[rec.State]
}

// gone is the state of a spec the book no longer holds. Its last build's outcome says whether it was accepted.
func (s *buildStats) gone() tui.State {
	if s.outcome == outAccepted {
		return tui.Accepted
	}

	return tui.Rejected
}

// shown is the build as the TUI shows it: its runs and gates in the order they ran.
func (s *buildStats) shown() *tui.Build {
	b := &tui.Build{Spec: string(s.Spec), Approved: string(s.Approved), Started: s.At, Size: s.Size,
		Outcome: s.outcome, Amendments: s.amendments, Fix: s.Monitor != "", Summary: s.summary, Report: s.report}
	gates := 0
	for _, r := range s.runs {
		before := max(gates, min(r.gates, len(s.gates)))
		b.Steps = append(append(b.Steps, gateSteps(s.gates[gates:before])...), r.tuiRun())
		gates = before
	}

	b.Steps = append(b.Steps, gateSteps(s.gates[gates:])...)
	for _, e := range s.readings {
		b.Readings = append(b.Readings, e.tuiReading())
	}

	return b
}

// tuiRun is the run as the TUI shows it. A run with no end line reads to its stream's end.
func (r run) tuiRun() *tui.Run {
	shown := &tui.Run{Role: r.role, Session: r.session, Why: r.why.shown(), Round: r.why.Round,
		Rounds: r.why.Rounds, Start: r.start, End: r.end, Totals: r.totals, Changed: r.changed, From: r.from,
		To: r.to}
	if r.end.IsZero() {
		shown.To, shown.Lost = -1, !alive(r.pid)
	}

	return shown
}

func gateSteps(gates []entry) []tui.Step {
	steps := make([]tui.Step, len(gates))
	for i, e := range gates {
		steps[i] = &tui.Gate{Start: e.At, End: e.End, Passed: e.Passed, Landing: e.Landing}
	}

	return steps
}

// alive reports whether the process pid lives. A run's start line names the kiln run that started it.
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return pid > 0 && (err == nil || errors.Is(err, syscall.EPERM))
}

// shown is why a run started, in the words the view shows. It is "" when its start line does not say.
func (w reason) shown() string {
	round := fmt.Sprintf("round %d of %d", w.Round, w.Rounds)
	if w.Rounds == 0 {
		round = fmt.Sprintf("round %d", w.Round)
	}

	return map[string]string{
		whyFirst:       "first run",
		whyGate:        round,
		whyAudit:       fmt.Sprintf("audit, try %d", w.Try),
		whyFindings:    "audit's findings",
		whyConflict:    "conflict with main, " + round,
		whyLanding:     "landing gate failed, " + round,
		whyUnchanged:   "work changed nothing on main, " + round,
		whyLimit:       "after a usage limit",
		whyInterrupted: "after kiln stopped",
		whyStack:       "stack's gate failed, " + round,
		whyBug:         "a bug the verify session found, " + round,
		whyCannotHold:  "tests the build can't meet",
		whyNoResult:    fmt.Sprintf("after no result, try %d", w.Try),
		whyBugsLine:    "asked for its bugs line",
	}[w.Why]
}
