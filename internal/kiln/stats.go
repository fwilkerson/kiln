package kiln

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/fwilkerson/kiln/internal/claude"
)

// Stats prints what each build cost, from the ledger alone. With name "", it prints one line per build, oldest
// first. Given a spec's name, it prints each build of that spec in full, by role and by session.
func (r *Repo) Stats(_ context.Context, name string, out io.Writer) error {
	entries, err := r.store.entries()
	if err != nil {
		return err
	}

	all := collect(entries)
	if name != "" {
		all = slices.DeleteFunc(all, func(s *buildStats) bool { return s.Spec != name })
	}

	switch {
	case len(all) == 0 && name == "":
		fmt.Fprintln(out, "the ledger holds no build")
	case len(all) == 0:
		fmt.Fprintf(out, "the ledger holds no build of %s\n", name)
	}

	for i, s := range all {
		if name == "" {
			s.line(out)
			continue
		}

		if i > 0 {
			fmt.Fprintln(out)
		}

		s.full(out)
	}

	return nil
}

// buildStats is one build, as the ledger holds it.
type buildStats struct {
	entry
	outcome    string
	amendments string
	runs       []run
	gates      []entry
}

// run is one session run. Its end is zero while it runs.
type run struct {
	role, session string
	start, end    time.Time
	ended         string
	totals        claude.Totals
	why           reason
	// pid is the kiln run's that started it.
	pid int
	// from and to are its bytes of the session's stream, and to is 0 until it ends.
	from, to int64
	changed  []string
	// gates counts the build's gates that ran before it.
	gates int
}

// collect groups the ledger's lines by build, in the order the builds started.
func collect(entries []entry) []*buildStats {
	var all []*buildStats
	byID := map[string]*buildStats{}
	for _, e := range entries {
		// kiln writes a build line again if it died before the build's session started.
		if e.Kind == builtKind && byID[e.Build] == nil {
			byID[e.Build] = &buildStats{entry: e}
			all = append(all, byID[e.Build])
		}

		s := byID[e.Build]
		if s == nil || e.Kind == builtKind {
			continue
		}

		switch e.Kind {
		case startKind:
			s.runs = append(s.runs, run{role: e.Role, session: e.Session, start: e.At, why: e.reason, pid: e.PID,
				from: e.Streamed, gates: len(s.gates)})
		case endKind:
			i := slices.IndexFunc(s.runs, func(r run) bool { return r.session == e.Session && r.end.IsZero() })
			if i < 0 {
				continue
			}

			s.runs[i].end, s.runs[i].ended, s.runs[i].to, s.runs[i].changed = e.At, e.Ended, e.Streamed, e.Changed
			if e.Totals != nil {
				s.runs[i].totals = *e.Totals
			}
		case gateKind:
			s.gates = append(s.gates, e)
		case outcomeKind:
			s.outcome = e.Outcome
			s.amendments = cmpOr(e.Amendments, s.amendments)
		}
	}

	return all
}

// line prints the build as one line of the trend.
func (s *buildStats) line(out io.Writer) {
	t := total(s.runs, s.gates)
	cost := t.cost.dollars("cost")
	if s.Size > 0 && t.cost.lacks < t.cost.of {
		perTest := sum[float64]{known: t.cost.known / float64(s.Size), lacks: t.cost.lacks, of: t.cost.of}
		cost += ", " + perTest.dollars("cost") + " per test"
	}

	fmt.Fprintf(out, "%s  %s  %s · %s · %s · %s waiting on tools · %s in gates · %s · %s · %s\n",
		s.At.Local().Format("2006-01-02 15:04"), s.Spec, s.ended(), tests(s.Size), t.time.duration("time"),
		minutes(t.waiting), minutes(t.gates), cost, t.turns.count("turn"), failedCalls(len(t.failed)))
}

// brief is the build's time, cost, turns and failed calls, for its landing commit.
func (s *buildStats) brief() string {
	t := total(s.runs, s.gates)
	return fmt.Sprintf("%s · %s · %s · %s", t.time.duration("time"), t.cost.dollars("cost"), t.turns.count("turn"),
		failedCalls(len(t.failed)))
}

// full prints the build as a whole, then each role. A role with more than one session shows each of them too.
func (s *buildStats) full(out io.Writer) {
	fmt.Fprintf(out, "%s, approved at %s, started %s     size: %s · %s\n", s.Spec, short(s.Approved),
		s.At.Local().Format("2006-01-02 15:04"), tests(s.Size), s.ended())
	total(s.runs, s.gates).print(out, "")
	var roles []string
	for _, r := range s.runs {
		if !slices.Contains(roles, r.role) {
			roles = append(roles, r.role)
		}
	}

	for _, role := range roles {
		runs := slices.DeleteFunc(slices.Clone(s.runs), func(r run) bool { return r.role != role })
		var sessions []string
		for _, r := range runs {
			if !slices.Contains(sessions, r.session) {
				sessions = append(sessions, r.session)
			}
		}

		if len(sessions) == 1 {
			fmt.Fprintf(out, "\n  %s role, session %s\n", role, sessions[0])
			total(runs, nil).print(out, "  ")
			continue
		}

		fmt.Fprintf(out, "\n  %s role, %d sessions\n", role, len(sessions))
		total(runs, nil).print(out, "  ")
		for _, session := range sessions {
			fmt.Fprintf(out, "\n    session %s\n", session)
			own := slices.DeleteFunc(slices.Clone(runs), func(r run) bool { return r.session != session })
			total(own, nil).print(out, "    ")
		}
	}
}

func (s *buildStats) ended() string {
	return cmp.Or(s.outcome, "not ended")
}

// sum is a number summed over runs, of which lacks could not be read.
type sum[T int | float64 | time.Duration] struct {
	known T
	lacks int
	of    int
}

func (s *sum[T]) add(v *T) {
	s.of++
	if v == nil {
		s.lacks++
		return
	}

	s.known += *v
}

// shown shows a sum that holds an unknown as at least its known part. It counts the runs the sum lacks.
func (s sum[T]) shown(what, known string) string {
	switch {
	case s.lacks == 0:
		return known
	case s.lacks == s.of:
		return what + " unknown"
	}

	return fmt.Sprintf("at least %s, %d of %d runs unknown", known, s.lacks, s.of)
}

func (s sum[T]) dollars(what string) string {
	return s.shown(what, fmt.Sprintf("$%.2f", float64(s.known)))
}

func (s sum[T]) count(what string) string {
	return s.shown(what+"s", plural(int(s.known), what))
}

func (s sum[T]) duration(what string) string {
	return s.shown(what, minutes(time.Duration(s.known)))
}

// slowestShown is how many of the slowest calls kiln stats shows for each build, role and session.
const slowestShown = 3

// totals is what a set of runs, and a build's gates, add up to.
type totals struct {
	runs               int
	time               sum[time.Duration]
	turns              sum[int]
	cost               sum[float64]
	waiting, subagents time.Duration
	gates              time.Duration
	gated              bool
	paused             int
	context            *claude.Context
	sevenDay           *claude.Use
	compactions        int
	tools              map[string]int
	slowest            []claude.Call
	failed             []claude.Failure
}

// total adds up runs and gates. Context is per session. Over several, it is the first session's first message,
// the peak of them all, and the last session's last message.
func total(runs []run, gates []entry) totals {
	t := totals{runs: len(runs), tools: map[string]int{}, gated: gates != nil}
	for _, g := range gates {
		t.gates += g.End.Sub(g.At)
	}

	for _, r := range runs {
		var took *time.Duration
		if !r.end.IsZero() {
			d := r.end.Sub(r.start)
			took = &d
		}

		t.time.add(took)
		t.turns.add(r.totals.Turns)
		t.cost.add(r.totals.CostUSD)
		t.waiting += r.totals.Waiting
		t.subagents += r.totals.Subagents
		if r.ended == endedLimit {
			t.paused++
		}

		if c := r.totals.Context; c != nil {
			if t.context == nil {
				t.context = &claude.Context{First: c.First}
			}

			t.context.Peak, t.context.Last = max(t.context.Peak, c.Peak), c.Last
		}

		if u := r.totals.SevenDay; u != nil {
			if t.sevenDay == nil {
				t.sevenDay = &claude.Use{First: u.First}
			}

			t.sevenDay.Last = u.Last
		}

		t.compactions += r.totals.Compactions
		for tool, n := range r.totals.Tools {
			t.tools[tool] += n
		}

		t.slowest = append(t.slowest, r.totals.Slowest...)

		t.failed = append(t.failed, r.totals.Failed...)
	}

	slices.SortStableFunc(t.slowest, func(a, b claude.Call) int { return cmp.Compare(b.Took, a.Took) })
	t.slowest = t.slowest[:min(slowestShown, len(t.slowest))]
	t.time.known += t.gates
	return t
}

// print prints t's lines, each after indent.
func (t totals) print(out io.Writer, indent string) {
	p := func(label, format string, args ...any) {
		if label != "" {
			label += ":"
		}

		fmt.Fprintf(out, "%s%-9s%s\n", indent, label, fmt.Sprintf(format, args...))
	}

	when := []string{t.time.duration("time"), minutes(t.waiting) + " waiting on tools"}
	if t.gated {
		when = append(when, minutes(t.gates)+" in gates")
	}

	when = append(when, minutes(t.subagents)+" in subagents", t.pauses())
	p("time", "%s", strings.Join(when, " · "))
	p("runs", "%s · %s · %s · %s · %s · %s", plural(t.runs, "run"), t.turns.count("turn"), t.cost.dollars("cost"),
		t.contexts(), plural(t.compactions, "compaction"), t.limit())
	p("tools", "%s", t.toolCalls())
	label := "slowest"
	for _, c := range t.slowest {
		p(label, "%s  %s", minutes(c.Took), clipped(c.Input))
		label = ""
	}

	p("failed", "%s", failedCalls(len(t.failed)))
	for _, c := range claude.Causes(t.failed) {
		fmt.Fprintf(out, "%s  %4d  %s\n%s        %s\n", indent, c.Count, c.Text, indent, clipped(c.Tool+": "+c.Input))
	}
}

func (t totals) pauses() string {
	switch t.paused {
	case 0:
		return "never paused"
	case 1:
		return "paused for a usage limit"
	}

	return fmt.Sprintf("paused %d times for a usage limit", t.paused)
}

func (t totals) contexts() string {
	if t.context == nil {
		return "context unknown"
	}

	return fmt.Sprintf("context %s → %s peak → %s", tokens(t.context.First), tokens(t.context.Peak),
		tokens(t.context.Last))
}

func (t totals) limit() string {
	if t.sevenDay == nil {
		return "7-day limit unknown"
	}

	return fmt.Sprintf("7-day limit %.0f%% → %.0f%%", t.sevenDay.First*100, t.sevenDay.Last*100)
}

func (t totals) toolCalls() string {
	names := slices.SortedFunc(maps.Keys(t.tools), func(a, b string) int {
		return cmp.Or(cmp.Compare(t.tools[b], t.tools[a]), cmp.Compare(a, b))
	})
	if len(names) == 0 {
		return "none"
	}

	calls := make([]string, len(names))
	for i, name := range names {
		calls[i] = fmt.Sprintf("%d %s", t.tools[name], name)
	}

	return strings.Join(calls, " · ")
}

// minutes shows a span of time in seconds below a minute, and in minutes above.
func minutes(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Round(time.Second)/time.Second))
	case d < 10*time.Minute:
		return fmt.Sprintf("%.1f min", d.Minutes())
	}

	return fmt.Sprintf("%.0f min", d.Minutes())
}

func tokens(n int) string {
	if n < 1000 {
		return fmt.Sprint(n)
	}

	return fmt.Sprintf("%dk", (n+500)/1000)
}

func tests(n int) string {
	return plural(n, "acceptance test")
}

func failedCalls(n int) string {
	return plural(n, "failed call")
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}

	return fmt.Sprintf("%d %ss", n, what)
}

// clipped keeps a line short enough to read beside its label.
func clipped(s string) string {
	const kept = 100
	if r := []rune(s); len(r) > kept {
		return string(r[:kept]) + " ..."
	}

	return s
}
