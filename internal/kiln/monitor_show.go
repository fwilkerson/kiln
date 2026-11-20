package kiln

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/tui"
)

// standing is where a monitor stands, as kiln status, kiln monitor show and kiln tui show it.
type standing struct {
	// result is what its last run came to, such as healthy, or held: stopped. It is "" for a monitor that never ran.
	result string
	// outcome is the outcome of the fix the result names, or "". fixed is whether the result names a fix.
	outcome string
	fixed   bool
	// rec is the book's record of the fix while the book says where it stands, or nil. That is while its accepted
	// landing is queued or building, or while the kiln that fixes it is gone.
	rec *record
	// summary is the check's summary, the report's summary, the session's error, or why the check couldn't run. A
	// landed fix's is its landing's title, but for a gauge's, which keeps the reading.
	summary, report string
	// last is when its last run started, and next when it is due again. Each is zero for a monitor that never ran.
	last, next time.Time
}

// neverRun is the state of a monitor that never ran.
const neverRun tui.State = "never run"

// standing is where m stands, from its history. rec is the record of its fix, if the book holds one. An accepted
// landing is worded from rec, since the ledger's last outcome still says the fix waits on its branch.
func (h *history) standing(m monitor, rec *record) standing {
	if h == nil || h.last.Kind == "" {
		return standing{}
	}

	s := standing{last: h.last.At, next: h.next(m)}
	e := h.last
	switch {
	case e.Past:
		s.result, s.summary = e.Found, e.Summary
	case e.Found == foundHeld && h.lastFix.Kind != "":
		s.outcome, s.fixed = h.result(h.lastFix), true
		s.result = foundHeld + ": " + fixResult(s.outcome)
		s.summary, s.report = h.fixSummary(h.lastFix), h.outcomes[h.lastFix.Build].Report
	case e.Found == foundFix:
		s.outcome, s.fixed = h.result(e), true
		s.result = fixResult(s.outcome)
		s.summary, s.report = h.fixSummary(e), h.outcomes[e.Build].Report
	default:
		s.result, s.summary = e.Found, e.Summary
	}

	// An older ledger line of a stopped fix repeats its summary as the first line of its report.
	if rest, ok := strings.CutPrefix(s.report, s.summary+"\n"); ok && s.summary != "" {
		s.report = rest
	}

	switch {
	case s.fixed && rec.fixHalted():
		s.result, s.rec = string(tui.Halted), rec
	case s.fixed && rec.fixLanding():
		s.result, s.rec = landingResult(rec.State), rec
		if run, found := h.running[rec.Build]; found && rec.State == building {
			s.result += ": " + run.reason.shown()
		}
	}

	return s
}

// fixLanding reports whether r, which may be nil, is a monitor's fix whose accepted landing is queued or building.
func (r *record) fixLanding() bool {
	if r == nil {
		return false
	}

	return r.Monitor && r.landing()
}

// fixHalted reports whether r, which may be nil, is a monitor's fix whose kiln is gone mid-fix or mid-landing.
func (r *record) fixHalted() bool {
	return r != nil && r.Monitor && r.halted()
}

// landingResult is the result of a monitor whose accepted fix's landing is in the state st.
func landingResult(st state) string {
	if st == building {
		return "accepted, landing"
	}

	return "accepted, lands at the next kiln monitor run"
}

// fixResult is a fix's outcome, in the words kiln status shows.
func fixResult(outcome string) string {
	switch outcome {
	case "":
		return "fix building"
	case outWaits:
		return "waiting on the developer"
	case outDropped:
		return "cut short"
	}

	return outcome
}

// monitorStatus shows each monitor main's tip holds. A line holds its result, when it last ran, and when it runs
// next. Its summary follows.
func (r *Repo) monitorStatus(ctx context.Context, out io.Writer) error {
	main, err := r.mainTip(ctx)
	if err != nil {
		return err
	}

	_, reads, err := r.monitors(ctx, main)
	if err != nil || len(reads) == 0 {
		return nil
	}

	entries, err := r.store.entries()
	if err != nil {
		return err
	}

	k, err := r.store.read()
	if err != nil {
		return err
	}

	all := histories(entries)
	now := r.now()
	fmt.Fprintln(out, "monitors:")
	for _, d := range reads {
		h, rec := all[d.name], k.find(d.name)
		s := refusedStanding(d)
		head := s.result
		if d.refused == nil {
			s = h.standing(d.monitor, rec)
			head = s.when(h.due(d.monitor, now))
			if s.result != "" {
				head = s.result + ", " + head
			}
		}

		fmt.Fprintf(out, "%s: %s\n", d.name, head)
		for _, line := range s.lines(d.name, rec, r.tree(d.name)) {
			fmt.Fprintln(out, indent(line))
		}
	}

	return nil
}

// refusedStanding is where the monitor d stands while kiln refuses it at main's tip: refused, with why.
func refusedStanding(d monitorRead) standing {
	if d.refused == nil {
		return standing{}
	}

	return standing{result: foundRefused, summary: d.refused.Error()}
}

// lines are what kiln status shows below the monitor name's line. They are its summary, why its fix stopped, and the
// command to run next. rec is the record of its fix, if it has one, and tree its worktree.
func (s standing) lines(name Spec, rec *record, tree git.Worktree) []string {
	var lines []string
	if s.summary != "" {
		lines = append(lines, s.summary)
	}

	// A report is long, so status shows only a stopped fix's, which says what failed.
	if s.outcome == outStopped && s.report != "" {
		lines = append(lines, s.report)
	}

	if next := s.step(name, rec, tree); next != "" {
		lines = append(lines, "next: "+next)
	}

	return lines
}

// step is the command the developer runs next on the monitor name. rec is the record of its fix, if the book holds
// one, and tree its worktree. It says where the fix stands now, such as an accepted landing that stopped, which the
// ledger's outcome can't.
func (s standing) step(name Spec, rec *record, tree git.Worktree) string {
	if rec != nil && rec.Monitor {
		return fixStep(*rec, tree)
	}

	return fixNext(name, s.outcome)
}

// when says when the monitor last ran, and when it runs next. due is whether it is due now.
func (s standing) when(due bool) string {
	last := "never ran"
	if !s.last.IsZero() {
		last = "last ran " + shownTime(s.last.Local())
	}

	if due {
		return last + ", runs at the next kiln monitor run"
	}

	return last + ", runs next after " + shownTime(s.next.Local())
}

// fixNext is the command the developer runs next on the fix of the monitor name, which ended with outcome. It is ""
// when the developer has nothing to run.
func fixNext(name Spec, outcome string) string {
	switch outcome {
	case outStopped:
		return fmt.Sprintf("read why it stopped, fix main, then run kiln monitor run %s", name)
	case outFailed:
		return fmt.Sprintf("fix what the error names, then run kiln monitor run %s", name)
	case outNeedsYou:
		return fmt.Sprintf("read the report, see to it, then run kiln monitor run %s", name)
	case outWaits:
		// A fix holds a commit for each round, so git show would print the last round alone.
		return fmt.Sprintf("read it with git diff main...kiln/%s, then run kiln accept %s, or kiln reject %s", name,
			name, name)
	}

	return ""
}

// MonitorShow shows how the last run of the monitor name ended, such as in a tab a notify program opens. It writes
// an OSC 9 sequence first, so a terminal such as Ghostty notifies from that tab. The result follows, with its
// summary, report or error, and the command to run next. Then it waits for return on in. It refuses a monitor the
// ledger holds no run of.
func (r *Repo) MonitorShow(ctx context.Context, name Spec, in io.Reader, out io.Writer) error {
	s, next, err := r.lastStanding(name)
	if err != nil {
		return err
	}

	// The body starts with kiln:, so a monitor named 4 can't read as OSC 9;4, a progress bar Ghostty draws.
	fmt.Fprintf(out, "\x1b]9;kiln: %s %s\a", name, s.result)
	fmt.Fprintf(out, "%s: %s, at %s\n", name, s.result, shownTime(s.last.Local()))
	for _, text := range []string{s.summary, s.report} {
		if text != "" {
			fmt.Fprintln(out, indent(text))
		}
	}

	if next != "" {
		fmt.Fprintln(out, indent("next: "+next))
	}

	fmt.Fprint(out, "\nPress return to close.")
	_, _ = bufio.NewReader(in).ReadString('\n')
	return nil
}

// lastStanding is where the monitor name stands after its last run, and the command the developer runs next, or "".
// It refuses a monitor the ledger holds no run of.
func (r *Repo) lastStanding(name Spec) (standing, string, error) {
	entries, err := r.store.entries()
	if err != nil {
		return standing{}, "", err
	}

	h := histories(entries)[name]
	if h == nil {
		return standing{}, "", fmt.Errorf("%w: the ledger holds no run of a monitor %s", errRefused, name)
	}

	k, err := r.store.read()
	if err != nil {
		return standing{}, "", err
	}

	rec := k.find(name)
	s := h.standing(monitor{Name: name}, rec)
	return s, s.step(name, rec, r.tree(name)), nil
}
