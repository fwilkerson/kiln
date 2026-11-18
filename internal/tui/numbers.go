package tui

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"github.com/fwilkerson/kiln/internal/claude"
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
	return sum(b.runs(), func(t claude.Totals) *float64 { return t.CostUSD })
}

// turns sums its runs' turns. It is unknown when a run's are.
func (b *Build) turns() (int, bool) {
	return sum(b.runs(), func(t claude.Totals) *int { return t.Turns })
}

// sum adds up the number of each run's totals. It is unknown when a run's is.
func sum[N int | float64](runs []*Run, of func(claude.Totals) *N) (N, bool) {
	var total N
	for _, r := range runs {
		n := of(r.Totals)
		if n == nil {
			return 0, false
		}

		total += *n
	}

	return total, true
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

		failed += len(r.Totals.Failed)
		refused += refusals(r.Totals.Failed)
	}

	return failed, refused, true
}

// refusals counts the failures that were refused.
func refusals(failed []claude.Failure) int {
	n := 0
	for _, f := range failed {
		if f.Refused {
			n++
		}
	}

	return n
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
	// short names it.
	short string
	of    func(*Build) (float64, bool)
	shown func(float64) string
	// judged is a number that is worse as it grows. One that is not is told in no color.
	judged bool
}

var measures = []measure{
	{"cost", (*Build).cost, func(v float64) string { return fmt.Sprintf("$%.2f", v) }, true},
	{"minutes", func(b *Build) (float64, bool) {
		d, ok := b.took()
		return d.Minutes(), ok
	}, decimal, true},
	{"failed calls", func(b *Build) (float64, bool) {
		n, _, ok := b.failed()
		return float64(n), ok
	}, decimal, true},
	{"rounds", func(b *Build) (float64, bool) {
		n, ok := b.rounds()
		return float64(n), ok
	}, decimal, true},
	{"size, tests", func(b *Build) (float64, bool) { return float64(b.Size), true }, decimal, false},
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

// values are m for each build that knows it, in the builds' order.
func (m measure) values(builds []*Build) []float64 {
	var values []float64
	for _, b := range builds {
		if v, ok := m.of(b); ok {
			values = append(values, v)
		}
	}

	return values
}

// median is the median of m over the builds that know it, and how many do.
func (m measure) median(builds []*Build) (median float64, known int) {
	values := m.values(builds)
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

// change says how the recent median moved from the previous one: +1 worse, -1 better. It is 0 when they show the
// same, or one is unknown.
func (m measure) change(recent, previous []*Build) int {
	if !m.judged {
		return 0
	}

	now, known := m.median(recent)
	before, knownBefore := m.median(previous)
	switch {
	case known == 0, knownBefore == 0, m.shown(now) == m.shown(before):
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
	if s := d.Round(time.Second); s < time.Minute {
		return fmt.Sprintf("%ds", int(s/time.Second))
	}

	m := d.Round(time.Minute)
	if m < time.Hour {
		return fmt.Sprintf("%dm", int(m/time.Minute))
	}

	return fmt.Sprintf("%dh%02dm", int(m/time.Hour), int(m%time.Hour/time.Minute))
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
