package tui

import (
	"cmp"
	"context"
	"fmt"
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
		{"a trend too narrow for its sparklines does not label them", ended(1, 1), 74,
			`│ +median *│`, "$1.00  previous", nil},
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

// TestRoundsUnknown holds that a build's rounds are unknown when a run does not say why it started. The trend
// leaves it out of the median, and its spec's screen shows its rounds as unknown, never as 0.
func TestRoundsUnknown(t *testing.T) {
	now := time.Now()
	b := &Build{Spec: "x", Started: now, Outcome: "landed", Steps: []Step{&Run{Role: "build", Start: now, End: now}}}
	src := source{specs: []Spec{{Name: "x", State: UAT, Builds: []*Build{b}}}}
	if m := drive(t, src, "", 140, 30); !regexp.MustCompile(`rounds per build +— .*  0 of 1 *│`).MatchString(
		screenOf(m)) {
		t.Errorf("the trend counts the rounds:\n%s", screenOf(m))
	}

	if m := drive(t, src, "x", 140, 30); !regexp.MustCompile(`(?m)^1 .* landed .* \?$`).MatchString(screenOf(m)) {
		t.Errorf("the spec's screen shows the rounds:\n%s", screenOf(m))
	}
}

// TestHomeWheel holds that the wheel on home scrolls the pane under the pointer, which then takes the keys.
func TestHomeWheel(t *testing.T) {
	now := time.Now().Truncate(time.Hour)
	var builds []*Build
	for i := range 14 {
		at := now.Add(time.Duration(i-20) * time.Hour)
		builds = append(builds, &Build{Spec: "x", Started: at, Outcome: "landed", Steps: []Step{
			&Run{Role: "build", Why: "first run", Start: at, End: at.Add(time.Minute)},
		}})
	}

	src := source{specs: []Spec{{Name: "x", State: UAT, Builds: builds}, {Name: "y", State: Queued}}}
	m := drive(t, src, "", 120, 24)
	newest := builds[13].Started.Local().Format("Jan 02 15:04")
	x, y := locate(t, m, newest)
	for range 12 {
		m = send(m, func() tea.Msg { return tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown} })
	}

	if strings.Contains(screenOf(m), newest) {
		t.Errorf("the wheel did not scroll the recent builds:\n%s", screenOf(m))
	}

	if m.focus != recentPane || m.sel[specsPane] != 0 {
		t.Errorf("the wheel moved pane %d, and the specs' selection to %d", m.focus, m.sel[specsPane])
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
			m := drive(t, src, "x", 100, 20, press(tea.KeyEnter), press(tea.KeyEnter))
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
		"from a build": {press(tea.KeyEnter), press('s')},
		"from a run":   {press(tea.KeyEnter), press(tea.KeyEnter), press('s')},
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
	return sendAll(send(m, m.Init()), append([]tea.Msg{tea.WindowSizeMsg{Width: w, Height: h}}, msgs...)...)
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

func press(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

// TestMarkdown holds how the view draws Markdown: as written. A line of prose wider than the view wraps under its own
// text. A table's row and a fenced line are cut at the view's edge instead. Then h and l scroll to the rest.
func TestMarkdown(t *testing.T) {
	long := strings.Repeat("word ", 12)
	for _, tc := range []struct {
		name, text string
		// keys are pressed on the spec text, and shows is what the screen's body then shows.
		keys  []tea.Msg
		shows string
		// colored is a text whose first cell has a color, and plain one whose first cell has none. Either may be "".
		colored, plain string
	}{
		{"prose wraps under its list item's text", "1. " + long, nil,
			"1. word word word word word word\n   word word word word word word", "", ""},
		{"a wrapped row keeps its line's color", "`" + long + "`", nil,
			"`word word word word word word word\nword word word word word `", "word word word word word `", ""},
		{"a table's row is cut", "| " + long + "|", nil, "| word word word word word word wo…", "", ""},
		{"a fenced line is cut", "```\n" + long + "\n```", nil, "```\nword word word word word word word…\n```", "", ""},
		{"l scrolls sideways", "| " + long + "|", []tea.Msg{press('l')}, " word word word word word word word", "", ""},
		{"a fence in a list item is colored by its language", "- An item:\n\n  ```go\n  func X() {}\n  ```", nil,
			"- An item:\n\n  ```go\n  func X() {}\n  ```", "func", ""},
		{"no line's syntax reaches the next", "It fails when w < 0.\n\nThe plain prose after it.", nil,
			"It fails when w < 0.\n\nThe plain prose after it.", "", "The plain prose"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &Build{Spec: "x", Steps: []Step{&Run{Role: "build", Session: "s"}}}
			src := source{specs: []Spec{{Name: "x", State: UAT, Builds: []*Build{b}}}, text: tc.text}
			m := drive(t, src, "x", 35, 20, append([]tea.Msg{press('s')}, tc.keys...)...)
			body := strings.Split(screenOf(m), "\n")[2:]
			if got := strings.Join(body, "\n"); !strings.HasPrefix(got, tc.shows+"\n") {
				t.Errorf("the body shows\n%s\nwant\n%s", got, tc.shows)
			}

			if tc.colored != "" && colorOf(t, m, tc.colored) == nil {
				t.Errorf("%q is drawn in the terminal's own color", tc.colored)
			}

			if tc.plain == "" {
				return
			}

			if got := colorOf(t, m, tc.plain); got != nil {
				t.Errorf("%q is drawn in %v, not the terminal's own color", tc.plain, got)
			}
		})
	}
}

// TestLiveRun holds how a running run's screen takes what its session writes while the developer reads it. At the
// top, the view stays there, where a new call comes in. Scrolled down, the call at the top stays there, even for a
// read that arrives late. An answer that arrives is drawn above the calls.
func TestLiveRun(t *testing.T) {
	now := time.Now()
	calls := func(n int) claude.Recounted {
		var actions []claude.Action
		for i := range n {
			actions = append(actions, claude.Action{At: now, Tool: "Bash", Input: fmt.Sprintf("call %d", i)})
		}

		return claude.Recounted{Written: now, Actions: actions}
	}

	r := &Run{Role: "build", Session: "s", Why: "first run", Start: now, To: -1}
	src := source{specs: []Spec{{Name: "x", State: Running, Builds: []*Build{{Spec: "x", Steps: []Step{r}}}}},
		calls: map[string]claude.Recounted{"s": calls(20)}}
	m := drive(t, src, "x", 80, 12, press(tea.KeyEnter), press(tea.KeyEnter))
	tops := func(want string) {
		t.Helper()
		if top := strings.Split(screenOf(m), "\n")[3]; !strings.Contains(top, want) {
			t.Errorf("the view's top row is %q, want %q in it", top, want)
		}
	}

	tops("call 19 ")
	src.calls["s"] = calls(21)
	m = send(m, func() tea.Msg { return tick{} })
	tops("call 20 ")
	m = sendAll(m, press('j'), press('j'), press('j'))
	tops("call 17 ")
	src.calls["s"] = calls(23)
	m = send(m, func() tea.Msg { return tick{} })
	tops("call 17 ")
	late := calls(21)
	m = send(m, func() tea.Msg {
		return loaded{at: now, specs: src.specs, opened: runRef{"x", 0, 0}, open: &late}
	})

	tops("call 17 ")
	answered := calls(23)
	answered.Answer = "Built x."
	src.calls["s"] = answered
	m = send(m, func() tea.Msg { return tick{} })
	tops("call 17 ")
	m = sendAll(m, press('g'))
	tops("Built x.")
}

// TestNewBuild holds that a build queued while the developer is on its spec's screen leaves the selection where it
// was. The new build is listed first.
func TestNewBuild(t *testing.T) {
	now := time.Now()
	build := func(at time.Duration) *Build {
		return &Build{Spec: "x", Started: now.Add(at), Steps: []Step{&Run{Role: "build", Session: "s", Start: now}}}
	}

	src := source{specs: []Spec{{Name: "x", State: UAT, Builds: []*Build{build(-2 * time.Hour), build(-time.Hour)}}}}
	m := drive(t, src, "x", 100, 20, press('j'))
	src.specs[0].Builds = append(src.specs[0].Builds, build(0))
	m = send(m, func() tea.Msg { return tick{} })
	if !regexp.MustCompile(`(?m)^#.*\n3 .*\n2 .*\n1 `).MatchString(screenOf(m)) {
		t.Errorf("the builds are not listed newest first:\n%s", screenOf(m))
	}

	if m = sendAll(m, press(tea.KeyEnter)); !strings.Contains(screenOf(m), "x · build 1 · ") {
		t.Errorf("enter opened another build than build 1:\n%s", screenOf(m))
	}
}

// TestKeyList holds that while the key list shows, only a key that closes it or quits does anything. The wheel does
// nothing. A click closes it, and opens nothing. On home, while the filter takes text, ? is text.
func TestKeyList(t *testing.T) {
	now := time.Now()
	b := &Build{Spec: "x", Started: now, Steps: []Step{&Run{Role: "build", Session: "s", Why: "first run", Start: now,
		End: now}}}
	src := source{specs: []Spec{{Name: "x", State: UAT, Builds: []*Build{b}}}}
	m := drive(t, src, "", 100, 20, press('?'), press('j'), press(tea.KeyTab),
		tea.MouseWheelMsg{X: 50, Y: 10, Button: tea.MouseWheelDown})
	if !strings.Contains(screenOf(m), "? or esc closes this") || m.focus != specsPane || m.sel[specsPane] != 0 {
		t.Errorf("a key reached the screen under the key list, or closed it:\n%s", screenOf(m))
	}

	x, y := locate(t, m, "◆ x")
	if m = sendAll(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}); m.keysShown || len(m.stack) != 1 {
		t.Errorf("a click left the key list open, or opened a screen:\n%s", screenOf(m))
	}

	if m = sendAll(m, press('/'), tea.KeyPressMsg{Code: '?', Text: "?"}); m.keysShown || m.filter != "?" {
		t.Errorf("? while the filter takes text is not text: the filter is %q", m.filter)
	}
}

// sendAll sends m each message in turn. It carries out each command the model answers.
func sendAll(m model, msgs ...tea.Msg) model {
	for _, msg := range msgs {
		m = send(m, func() tea.Msg { return msg })
	}

	return m
}

// TestNarrow holds that in a narrow or short terminal, each footer still names ? and esc, and the key list fits.
func TestNarrow(t *testing.T) {
	now := time.Now()
	b := &Build{Spec: "x", Started: now, Steps: []Step{&Run{Role: "build", Session: "s", Why: "first run", Start: now,
		End: now}}}
	src := source{specs: []Spec{{Name: "x", State: UAT, Builds: []*Build{b}}}}
	for _, keys := range [][]tea.Msg{nil, {press(tea.KeyEnter)}, {press(tea.KeyEnter), press(tea.KeyEnter)}} {
		rows := strings.Split(screenOf(drive(t, src, "x", 80, 24, keys...)), "\n")
		if foot := rows[len(rows)-1]; !strings.Contains(foot, "? keys") || !strings.Contains(foot, "esc back") ||
			strings.Contains(foot, "…") {
			t.Errorf("the footer is %q", foot)
		}
	}

	m := drive(t, src, "x", 80, 12, press(tea.KeyEnter), press(tea.KeyEnter), press('?'))
	if rows := strings.Split(screenOf(m), "\n"); len(rows) != 12 || !strings.Contains(screenOf(m), "? or esc closes") ||
		!strings.Contains(screenOf(m), "q ctrl+c") {
		t.Errorf("the key list does not fit 12 rows:\n%s", screenOf(m))
	}
}
