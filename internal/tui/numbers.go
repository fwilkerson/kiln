package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
)

// runs are the build's session runs, in order.
func (b *Build) runs() []*Run {
	var runs []*Run
	for _, s := range b.Steps {
		if r, ok := s.(*Run); ok {
			runs = append(runs, r)
		}
	}

	return runs
}

// cost sums its runs' cost. It is unknown when a run's is.
func (b *Build) cost() (float64, bool) {
	var sum float64
	for _, r := range b.runs() {
		if r.Totals.CostUSD == nil {
			return 0, false
		}

		sum += *r.Totals.CostUSD
	}

	return sum, true
}

func (b *Build) turns() (int, bool) {
	var sum int
	for _, r := range b.runs() {
		if r.Totals.Turns == nil {
			return 0, false
		}

		sum += *r.Totals.Turns
	}

	return sum, true
}

// took is its runs' time and its gates' time. It is unknown while a run has no end.
func (b *Build) took() (time.Duration, bool) {
	var sum time.Duration
	for _, s := range b.Steps {
		switch s := s.(type) {
		case *Run:
			if s.End.IsZero() {
				return 0, false
			}

			sum += s.End.Sub(s.Start)
		case *Gate:
			sum += s.End.Sub(s.Start)
		}
	}

	return sum, true
}

// failed counts its runs' failed calls, and how many of them were refused. It is unknown while a run has no end.
func (b *Build) failed() (failed, refused int, known bool) {
	for _, r := range b.runs() {
		if r.End.IsZero() {
			return 0, 0, false
		}

		for _, f := range r.Totals.Failed {
			failed++
			if f.Refused {
				refused++
			}
		}
	}

	return failed, refused, true
}

// rounds is the highest round any of its runs ran in, or 0 when kiln never sent the work back. It is unknown when a
// run does not say why it started, since its round is unknown too.
func (b *Build) rounds() (int, bool) {
	most := 0
	for _, r := range b.runs() {
		if r.Why == "" {
			return 0, false
		}

		most = max(most, r.Round)
	}

	return most, true
}

// measure is one number of a build that the trend shows.
type measure struct {
	// label names it, and short names it in a narrow terminal.
	label, short string
	of           func(*Build) (float64, bool)
	shown        func(float64) string
	// judged is a number that is worse as it grows. One that is not is told in no color.
	judged bool
}

var measures = []measure{
	{"cost per build", "cost", (*Build).cost, func(v float64) string { return fmt.Sprintf("$%.2f", v) }, true},
	{"minutes per build", "minutes", func(b *Build) (float64, bool) {
		d, ok := b.took()
		return d.Minutes(), ok
	}, decimal, true},
	{"failed calls per build", "failed calls", func(b *Build) (float64, bool) {
		n, _, ok := b.failed()
		return float64(n), ok
	}, decimal, true},
	{"rounds per build", "rounds", func(b *Build) (float64, bool) {
		n, ok := b.rounds()
		return float64(n), ok
	}, decimal, true},
	{"size, in tests", "size, tests", func(b *Build) (float64, bool) { return float64(b.Size), true }, decimal, false},
}

// window is how many builds the trend holds, and the previous builds beside it.
const window = 10

// trend splits the builds that ended into the last window of them, oldest first, and the window before.
func trend(specs []Spec) (recent, previous []*Build) {
	var ended []*Build
	for _, s := range specs {
		for _, b := range s.Builds {
			if b.Outcome != "" {
				ended = append(ended, b)
			}
		}
	}

	slices.SortStableFunc(ended, func(a, b *Build) int { return a.Started.Compare(b.Started) })
	cut := max(0, len(ended)-window)
	return ended[cut:], ended[max(0, cut-window):cut]
}

// median is the median of m over the builds that know it, and how many do.
func (m measure) median(builds []*Build) (median float64, known int) {
	var values []float64
	for _, b := range builds {
		if v, ok := m.of(b); ok {
			values = append(values, v)
		}
	}

	if len(values) == 0 {
		return 0, 0
	}

	slices.Sort(values)
	n := len(values)
	if n%2 == 1 {
		return values[n/2], n
	}

	return (values[n/2-1] + values[n/2]) / 2, n
}

// spark draws m for each build, oldest first, one cell each, as high as its share of the largest. A build that
// does not know m draws a dot.
func (m measure) spark(builds []*Build) string {
	const levels = "▁▂▃▄▅▆▇█"
	steps := []rune(levels)
	most := 0.0
	for _, b := range builds {
		if v, ok := m.of(b); ok {
			most = max(most, v)
		}
	}

	var s strings.Builder
	for _, b := range builds {
		v, ok := m.of(b)
		switch {
		case !ok:
			s.WriteRune('·')
		case most == 0:
			s.WriteRune(steps[0])
		default:
			s.WriteRune(steps[min(len(steps)-1, int(v/most*float64(len(steps)-1)+0.5))])
		}
	}

	return s.String()
}

// change says how the recent median moved from the previous one: +1 worse, -1 better. It is 0 when they show the
// same, or one is unknown.
func (m measure) change(recent, previous []*Build) int {
	now, known := m.median(recent)
	before, knownBefore := m.median(previous)
	if !m.judged || known == 0 || knownBefore == 0 || m.shown(now) == m.shown(before) {
		return 0
	}

	return cmp.Compare(now, before)
}

func decimal(v float64) string {
	if v == float64(int(v)) {
		return fmt.Sprint(int(v))
	}

	return fmt.Sprintf("%.1f", v)
}

// span shows a build's or a run's time: seconds under a minute, minutes under an hour, then hours and minutes.
func span(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Round(time.Second)/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Round(time.Minute)/time.Minute))
	}

	return fmt.Sprintf("%dh%02dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
}

// clock shows how long something has gone on, as minutes and seconds, such as 0:42.
func clock(d time.Duration) string {
	d = max(0, d.Round(time.Second))
	return fmt.Sprintf("%d:%02d", int(d/time.Minute), int(d%time.Minute/time.Second))
}

func dollars(v float64, known bool) string {
	if !known {
		return "?"
	}

	return fmt.Sprintf("$%.2f", v)
}

func tokens(n int) string {
	if n < 1000 {
		return fmt.Sprint(n)
	}

	return fmt.Sprintf("%dk", (n+500)/1000)
}

func count(n int, known bool) string {
	if !known {
		return "?"
	}

	return fmt.Sprint(n)
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}

	return fmt.Sprintf("%d %ss", n, what)
}
