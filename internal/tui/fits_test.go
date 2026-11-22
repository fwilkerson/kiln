package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fwilkerson/kiln/internal/claude"
)

// TestEveryScreenFits holds that each screen fits its terminal at every size, and after a resize. No line is wider
// than the terminal, and no screen has more lines than its rows. A terminal too small shows one line.
func TestEveryScreenFits(t *testing.T) {
	src := crowded(time.Now())
	monitor := []tea.Msg{press(tea.KeyTab), press('j')}
	screens := map[string][]tea.Msg{
		"home":            nil,
		"home, filtering": {press('/'), tea.KeyPressMsg{Code: 'l', Text: "l"}},
		"home, a monitor": monitor,
		"a fix":           append(monitor, press(tea.KeyEnter)),
		"build":           {press(tea.KeyEnter)},
		"run":             {press(tea.KeyEnter), press(tea.KeyEnter)},
		"run, scrolled":   {press(tea.KeyEnter), press(tea.KeyEnter), press('G'), press('l'), press('l')},
		"spec":            {press(tea.KeyEnter), press('s')},
		"home's keys":     {press('?')},
		"build's keys":    {press(tea.KeyEnter), press('?')},
		"run's keys":      {press(tea.KeyEnter), press(tea.KeyEnter), press('?')},
		"spec's keys":     {press(tea.KeyEnter), press('s'), press('?')},
	}

	for name, keys := range screens {
		for _, w := range []int{1, 20, 30, 35, 39, 40, 59, 80, 100, 120, 200} {
			for _, h := range []int{1, 4, 5, 6, 10, 24, 30, 60} {
				opened := drive(t, src, crowdedSpec, append([]tea.Msg{sized(w, h)}, keys...)...)
				fits(t, fmt.Sprintf("%s at %dx%d", name, w, h), opened)
				large := append([]tea.Msg{sized(200, 60)}, keys...)
				resized := drive(t, src, crowdedSpec, append(large, sized(w, h))...)
				fits(t, fmt.Sprintf("%s, resized to %dx%d", name, w, h), resized)
			}
		}
	}
}

// fits checks that m's view is no wider than m.w cells, and no taller than m.h rows. Too small, it is one line.
func fits(t *testing.T, at string, m model) {
	t.Helper()
	lines := strings.Split(m.View().Content, "\n")
	if small := m.w < m.minW(m.top().kind) || m.h < minH; small && len(lines) != 1 {
		t.Errorf("%s: %d lines in a terminal too small for the screen, want one:\n%s", at, len(lines), screenOf(m))
		return
	}

	if len(lines) > m.h {
		t.Errorf("%s: %d lines, over %d rows:\n%s", at, len(lines), m.h, screenOf(m))
		return
	}

	for i, line := range lines {
		if w := ansi.StringWidth(line); w > m.w {
			t.Errorf("%s: line %d is %d cells, over %d:\n%s", at, i, w, m.w, screenOf(m))
			return
		}
	}
}

// crowdedSpec is the spec of crowded whose build runs. It is selected, so Enter opens its newest build.
const crowdedSpec = "with-a-rather-long-spec-name"

// crowded is a source that fills each panel. It holds long names, many builds, and two runs at work. It holds monitors
// with long results and summaries, gauges, failed calls, and a long answer.
func crowded(now time.Time) source {
	v := func(f float64) *float64 { return &f }
	failed := []claude.Failure{{Tool: "Bash", Input: "go test ./... -run TestSomethingLong", Cause: "exit status 1"}}
	run := func(session string, at time.Time, running bool) *Run {
		r := &Run{Role: "build", Session: session, Why: "round 2 of 3, after the audit found five things", Round: 2,
			Rounds: 3, Start: at, End: at.Add(7 * time.Minute), Changed: []string{"/a/very/long/path/to/some/file.go"},
			Totals: claude.Totals{Turns: new(41), CostUSD: v(3.21), Failed: failed,
				Context: &claude.Context{Last: 123456}}}
		if running {
			r.End, r.To = time.Time{}, -1
		}

		return r
	}

	build := func(spec string, i int, running bool) *Build {
		at := now.Add(time.Duration(i-40) * time.Hour)
		b := &Build{Spec: spec, Approved: "0123456789abcdef", Started: at, Size: 12, Outcome: "landed",
			Amendments: "amended", Took: new(time.Hour), Cost: v(float64(i)), Turns: new(80), Failed: new(3),
			Rounds: new(2), Steps: []Step{run(spec+"-s", at, false), &Gate{Start: at, End: at.Add(time.Minute),
				Passed: true, Landing: true}, run(spec+"-s", at, running)},
			Readings: []Reading{{Gauge: "mutation score with a long name", At: at, Value: v(70.5), Line: "≥ 80",
				Crossed: true}, {Gauge: "files changed", At: at, Value: v(4), Line: "≤ 9"}}}
		if running {
			b.Outcome, b.Took, b.Cost = "", nil, nil
		}

		return b
	}

	var specs []Spec
	for i, name := range []string{crowdedSpec, "paused-one", "b", "an-even-longer-spec-name-than-that"} {
		s := Spec{Name: name, State: []State{Running, Paused, Accepted, UAT}[i]}
		for j := range 8 {
			s.Builds = append(s.Builds, build(name, j*4+i, i == 0 && j == 7))
		}

		specs = append(specs, s)
	}

	var monitors []Spec
	for i, name := range []string{"code-health", "a-monitor-with-a-long-name", "docs", "deps", "uat", "inspect"} {
		fixes := []*Build{build(name, i, i == 0)}
		fixes[0].Fix, fixes[0].Ended = true, Failed
		monitors = append(monitors, Spec{Name: name, State: []State{Running, NeedsYou, CouldNotRun, Healthy,
			CutShort, Limited}[i], Builds: fixes, Monitor: &Monitor{
			Result:   "held: waiting on the developer to accept the fix that it made earlier today",
			Summary:  strings.Repeat("a summary line that runs on and on past any panel\n", 5),
			Last:     now.Add(-time.Hour),
			Next:     now.Add(time.Hour),
			Readings: []Reading{{Gauge: "health", At: now, Value: v(9), Line: "≥ 10", Crossed: true}},
		}})
	}

	var actions []claude.Action
	for i := range 30 {
		actions = append(actions, claude.Action{At: now, Tool: "Bash", Input: strings.Repeat("go test ", 20),
			Took: time.Duration(i) * time.Second, Failed: i%3 == 0, Refused: i%5 == 0, Cause: "a cause that is long"})
	}

	answer := "# Built\n\n| a | table | row | that | runs | far | past | any | terminal | width | at | all |\n\n" +
		strings.Repeat("Prose that wraps. ", 40)
	calls := map[string]claude.Recounted{}
	for _, s := range append(slices.Clone(specs), monitors...) {
		calls[s.Name+"-s"] = claude.Recounted{Written: now.Add(-5 * time.Minute), Actions: actions, Answer: answer}
	}

	return source{specs: specs, monitors: monitors, calls: calls, text: answer}
}
