package tui

import (
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

// known is the number n points to, and whether it is known.
func known[N any](n *N) (N, bool) {
	if n == nil {
		var zero N
		return zero, false
	}

	return *n, true
}

// failed counts the build's failed calls, and how many of those calls were refused. Both are unknown while Failed
// is nil.
func (b *Build) failed() (failed, refused int, known bool) {
	if b.Failed == nil {
		return 0, 0, false
	}

	for _, r := range b.runs() {
		refused += refusals(r.Totals.Failed)
	}

	return *b.Failed, refused, true
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

// measure is one number of a build that the trend shows.
type measure struct {
	// short names it.
	short string
	of    func(*Build) (float64, bool)
	shown func(float64) string
	// judged is a number that is worse as it grows. One that is not is told in no color.
	judged bool
	// specOnly is a number only a spec's build has. A monitor's fix has no tests, and no rounds.
	specOnly bool
}

var measures = []measure{
	{"cost", func(b *Build) (float64, bool) { return known(b.Cost) }, func(v float64) string { return fmt.Sprintf("$%.2f", v) }, true, false},
	{"minutes", func(b *Build) (float64, bool) {
		d, ok := known(b.Took)
		return d.Minutes(), ok
	}, decimal, true, false},
	{"failed calls", func(b *Build) (float64, bool) {
		n, _, ok := b.failed()
		return float64(n), ok
	}, decimal, true, false},
	{"rounds", func(b *Build) (float64, bool) {
		n, ok := known(b.Rounds)
		return float64(n), ok
	}, decimal, true, true},
	{"size, tests", func(b *Build) (float64, bool) { return float64(b.Size), true }, decimal, false, true},
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
