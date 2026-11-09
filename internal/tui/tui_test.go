package tui

import (
	"cmp"
	"context"
	"image/color"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"

	"github.com/fwilkerson/kiln/internal/claude"
)

// TestTrend holds the home screen's trend: the last 10 builds that ended, beside the 10 before them. Each number is
// the median of the builds that know it. A number worse than before is red, and a better one green.
func TestTrend(t *testing.T) {
	now := time.Now()
	ended := func(costs ...float64) []*Build {
		var builds []*Build
		for _, c := range costs {
			cost := &c
			if c < 0 {
				cost = nil
			}

			at := now.Add(time.Duration(len(builds)-100) * time.Hour)
			builds = append(builds, &Build{Spec: "x", Started: at, Size: 2, Outcome: "landed", Steps: []Step{
				&Run{Role: "build", Why: "first run", Start: at, End: at.Add(time.Minute),
					Totals: claude.Totals{CostUSD: cost}},
			}})
		}

		return builds
	}

	running := &Build{Spec: "x", Started: now, Size: 2, Steps: []Step{&Run{Role: "build", Start: now, To: -1}}}
	for _, tc := range []struct {
		name   string
		builds []*Build
		// w is the terminal's width, or 140.
		w int
		// row is a pattern the trend's cost row matches, and median the text that starts with its median.
		row, median string
		color       color.Color
	}{
		{"the window holds the last 10 that ended, beside the 10 before them",
			ended(9, 9, 9, 9, 9, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1), 0,
			`cost per build +✓ +\$1\.00 ██████████ +previous 10: \$3\.00 *│`, "$1.00 █", ansi.Green},
		{"a build that has not ended is outside it", append(ended(1, 1, 1), running), 0,
			`cost per build +\$1\.00 ███ +previous 0: — *│`, "$1.00 █", nil},
		{"a build whose number is unknown is left out of its median", ended(1, -1, 3), 0,
			`cost per build +\$2\.00 ▃·█ +previous 0: —  2 of 3 *│`, "$2.00 ▃", nil},
		{"a worse number is red", ended(1, 1, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2), 0,
			`cost per build +✗ +\$2\.00 ██████████ +previous 2: \$1\.00 *│`, "$2.00 █", ansi.Red},
		{"an 80-column terminal shows both medians",
			ended(3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1), 80,
			`│cost +✓ +\$1\.00 ██████████ +previous 10: \$3\.00 *│`, "$1.00 █", ansi.Green},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := source{specs: []Spec{{Name: "x", State: UAT, Builds: tc.builds}}}
			m := drive(t, src, "", cmp.Or(tc.w, 140), 30)
			if !regexp.MustCompile("(?m)" + tc.row).MatchString(screenOf(m)) {
				t.Errorf("no row matches\n%s\non the screen\n%s", tc.row, screenOf(m))
			}

			if got := colorOf(t, m, tc.median); !sameColor(got, tc.color) {
				t.Errorf("the median is drawn in %v, want %v", got, tc.color)
			}
		})
	}
}

// TestRunScreen holds how a run's screen shows where it stands, and its calls.
func TestRunScreen(t *testing.T) {
	now := time.Now()
	call := claude.Action{At: now.Add(-time.Minute), Tool: "Bash", Input: "go test ./...", Open: true}
	for _, tc := range []struct {
		name string
		run  Run
		// written is how long ago the session last wrote.
		written time.Duration
		calls   []claude.Action
		// shows is a text on screen, and color the color it starts in.
		shows string
		color color.Color
	}{
		{"an open call in a running run runs", Run{To: -1}, time.Second, []claude.Action{call},
			"▶ " + call.At.Local().Format("15:04:05") + " Bash", ansi.Yellow},
		{"an open call in an ended run was cut off", Run{End: now}, time.Second, []claude.Action{call}, "cut off",
			ansi.Red},
		{"a run whose kiln run is gone shows it", Run{Lost: true}, time.Second, []claude.Action{call},
			"! kiln stopped mid-run", ansi.Yellow},
		{"idle past a minute with no call open is marked", Run{To: -1}, 2 * time.Minute, nil, "idle 2:00",
			ansi.Yellow},
		{"idle in a call is not", Run{To: -1}, 2 * time.Minute, []claude.Action{call}, "idle 2:00", nil},
		{"a refusal is red", Run{End: now}, time.Second, []claude.Action{{At: now, Tool: "Write", Input: "specs/x.md",
			Failed: true, Refused: true, Cause: "no"}}, "refused: no", ansi.Red},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.run
			r.Role, r.Session, r.Why, r.Start = "build", "s", "first run", now.Add(-3*time.Minute)
			b := &Build{Spec: "x", Started: r.Start, Steps: []Step{&r}}
			src := source{specs: []Spec{{Name: "x", State: Running, Builds: []*Build{b}}},
				calls: map[string]claude.Recounted{"s": {Written: now.Add(-tc.written), Actions: tc.calls}}}
			m := drive(t, src, "x", 100, 20, key(tea.KeyEnter), key(tea.KeyEnter))
			if got := colorOf(t, m, tc.shows); !sameColor(got, tc.color) {
				t.Errorf("%q is drawn in %v, want %v:\n%s", tc.shows, got, tc.color, screenOf(m))
			}
		})
	}
}

// TestSpecText holds that s opens the spec as its build's approval held it. It does so from a run as from a build.
func TestSpecText(t *testing.T) {
	now := time.Now()
	b := &Build{Spec: "x", Started: now, Steps: []Step{&Run{Role: "build", Session: "s", Start: now, End: now}}}
	src := source{specs: []Spec{{Name: "x", State: UAT, Builds: []*Build{b}}}, text: "# Spec\n\nThe approved text."}
	for name, keys := range map[string][]tea.Msg{
		"from a build": {key(tea.KeyEnter), key('s')},
		"from a run":   {key(tea.KeyEnter), key(tea.KeyEnter), key('s')},
	} {
		t.Run(name, func(t *testing.T) {
			if m := drive(t, src, "x", 100, 20, keys...); !strings.Contains(screenOf(m), "The approved text.") {
				t.Errorf("s did not open the spec:\n%s", screenOf(m))
			}
		})
	}
}

// colorOf answers the color the first cell of text is drawn in, as a terminal draws the view.
func colorOf(t *testing.T, m model, text string) color.Color {
	t.Helper()
	x, y := locate(t, m, text)
	emu := vt.NewEmulator(m.w, m.h)
	_, _ = emu.WriteString(strings.ReplaceAll(m.View().Content, "\n", "\r\n"))
	return emu.CellAt(x, y).Style.Fg
}

func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == b
	}

	r1, g1, b1, _ := a.RGBA()
	r2, g2, b2, _ := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2
}

// source is a Source that answers what a test gives it.
type source struct {
	specs []Spec
	// calls are each session's stretch of its stream.
	calls map[string]claude.Recounted
	text  string
}

func (s source) Specs(context.Context) ([]Spec, error)        { return s.specs, nil }
func (s source) Recount(r *Run) (claude.Recounted, error)     { return s.calls[r.Session], nil }
func (s source) Text(context.Context, *Build) (string, error) { return s.text, nil }

// drive starts a model on src, at w by h cells, and sends it each message in turn. It carries out each command the
// model answers.
func drive(t *testing.T, src Source, name string, w, h int, msgs ...tea.Msg) model {
	t.Helper()
	m := newModel(src, name, 0)
	m = send(m, m.Init())
	for _, msg := range append([]tea.Msg{tea.WindowSizeMsg{Width: w, Height: h}}, msgs...) {
		m = send(m, func() tea.Msg { return msg })
	}

	return m
}

func send(m model, cmd tea.Cmd) model {
	if cmd == nil {
		return m
	}

	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			m = send(m, c)
		}
	case nil:
	default:
		next, more := m.Update(msg)
		m = send(next.(model), more)
	}

	return m
}

func screenOf(m model) string { return ansi.Strip(m.View().Content) }

func locate(t *testing.T, m model, text string) (x, y int) {
	t.Helper()
	found := 0
	for i, line := range strings.Split(screenOf(m), "\n") {
		if before, _, ok := strings.Cut(line, text); ok {
			x, y = ansi.StringWidth(before), i
			found += strings.Count(line, text)
		}
	}

	if found != 1 {
		t.Fatalf("%q is on screen %d times, not once:\n%s", text, found, screenOf(m))
	}

	return x, y
}

func key(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }
