package tui

import (
	"cmp"
	"context"
	"fmt"
	"image/color"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"

	"github.com/fwilkerson/kiln/internal/claude"
)

// TestTrend holds the home screen's trend: the last 10 builds that ended, beside the 10 before them. Each number is
// the median of the builds that know it, above a bar for each build. A number worse than before is red, and a better
// one green.
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
			}, Took: new(time.Minute), Cost: cost, Failed: new(0), Rounds: new(0)})
		}

		return builds
	}

	running := &Build{Spec: "x", Started: now, Size: 2, Steps: []Step{&Run{Role: "build", Start: now, To: -1}}}
	for _, tc := range []struct {
		name   string
		builds []*Build
		// w is the terminal's width, or 140.
		w int
		// rows is a pattern the trend's rows for cost match. median is a text that starts with its median, or "".
		rows, median string
		color        color.Color
	}{
		{"the window holds the last 10 that ended, beside the 10 before them",
			ended(9, 9, 9, 9, 9, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1), 0,
			`last 10 builds .* the 10 before .*\n.*││cost .*\n.*││\$1\.00 ▼ was \$3\.00 .*\n.*\n.*││█ █ █ █ █ █ █ █ █ █ .*\n` +
				`.*││newest \$1\.00 .*\n.*││range \$1\.00–\$1\.00 `, "▼ was $3.00", dark.passed.GetForeground()},
		{"a build that has not ended is outside it", append(ended(1, 2, 4), running), 0,
			`last 3 builds .*\n.*││cost .*\n.*││\$2\.00 .*\n.*││ {8}███ .*\n.*││▄▄▄ ███ ███ .*\n.*││newest \$4\.00 .*\n` +
				`.*││range \$1\.00–\$4\.00 .*\n.*│╰`, "", nil},
		{"a build whose number is unknown is left out of its median, and draws a dot", ended(1, -1, 3), 0,
			`││cost 2/3 known .*\n.*││\$2\.00 .*\n.*\n.*││▅▅▅ ··· ███ `, "2/3 known", dark.dim.GetForeground()},
		{"a worse number is red", ended(1, 1, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2), 0,
			`││\$2\.00 ▲ was \$1\.00 `, "▲ was $1.00", dark.failed.GetForeground()},
		{"a trend too narrow for its tiles is a table with no bars", ended(1, 1), 74,
			`│cost +\$1\.00 +│\n.*│minutes `, "", nil},
		{"an 80-column terminal shows both medians",
			ended(3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1), 80,
			`│cost +\$1\.00 ▼ was \$3\.00 *│`, "▼ was $3.00", dark.passed.GetForeground()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := source{specs: []Spec{{Name: "x", State: UAT, Builds: tc.builds}}}
			m := drive(t, src, "", sized(cmp.Or(tc.w, 140), 30))
			if !regexp.MustCompile(tc.rows).MatchString(screenOf(m)) {
				t.Errorf("no rows match\n%s\non the screen\n%s", tc.rows, screenOf(m))
			}

			if tc.median == "" {
				return
			}

			if got := colorOf(t, m, tc.median); !sameColor(got, tc.color) {
				t.Errorf("the median is drawn in %v, want %v", got, tc.color)
			}
		})
	}
}

// TestMonitors holds that home lists the monitors beside the specs, each with its last run's result. Selected, a
// monitor shows when it last ran and runs next, and its summary. Its fixes open as a spec's builds do.
func TestMonitors(t *testing.T) {
	now := time.Now().Truncate(time.Minute)
	fix := &Build{Spec: "deps", Started: now.Add(-time.Hour), Outcome: "stopped", Fix: true, Steps: []Step{
		&Gate{Start: now.Add(-time.Hour), End: now.Add(-time.Hour)},
	}}
	src := source{specs: []Spec{{Name: "x", State: UAT}}, monitors: []Spec{
		{Name: "deps", State: Stopped, Builds: []*Build{fix}, Monitor: &Monitor{Result: "stopped",
			Summary: "3 modules to update", Last: now.Add(-time.Hour), Next: now.Add(22 * time.Hour)}},
		{Name: "docs", State: Waits, Monitor: &Monitor{Result: "held: waiting on the developer"}},
		{Name: "vulns", State: "never run", Monitor: &Monitor{Result: "never run", Due: true}},
	}}
	// At 80 columns, the specs panel is a third of the width, and each monitor's name still shows.
	m := drive(t, src, "", sized(80, 30))
	shows(t, m, `│◆ x +UAT *│`, `│✗ deps +stopped *│`, `│◆ docs +waits *│`, `│◌ vulns +never run *│`)

	// A monitor that waits on the developer is marked as a spec that waits is. A stopped fix is marked as a stopped
	// spec is.
	marks := map[string]color.Color{"◆ docs": dark.you.GetForeground(), "✗ deps": dark.failed.GetForeground()}
	for text, want := range marks {
		if got := colorOf(t, m, text); !sameColor(got, want) {
			t.Errorf("%q is drawn in %v, want %v", text, got, want)
		}
	}

	m = drive(t, src, "deps", sized(140, 30))
	last := now.Add(-time.Hour).Local().Format("Jan 02 15:04")
	shows(t, m, `fixes of deps · stopped · ran `+last+` · next `, `│3 modules to update *│`, `#1 +✗ stopped`)

	m = sendAll(m, tea.KeyPressMsg{Code: tea.KeyTab}, tea.KeyPressMsg{Code: tea.KeyTab},
		tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.top().kind != buildScreen || m.top().spec != "deps" {
		t.Errorf("enter on a fix opens screen %v of %q, want its build:\n%s", m.top().kind, m.top().spec, screenOf(m))
	}
}

// TestMonitorsPanel holds that the monitors have a panel of their own, below the specs. The specs' side lists and
// trends spec builds alone. The monitors' side lists and trends fixes alone, with no tests or rounds.
func TestMonitorsPanel(t *testing.T) {
	now := time.Now()
	ended := func(spec string, fix bool, size int, cost float64) *Build {
		return &Build{Spec: spec, Started: now.Add(-time.Hour), Outcome: "landed", Fix: fix, Size: size,
			Steps: []Step{&Run{Role: "build", Why: "first run", Start: now.Add(-time.Hour), End: now,
				Totals: claude.Totals{CostUSD: &cost}}}}
	}

	src := source{specs: []Spec{{Name: "x", State: Accepted, Builds: []*Build{ended("x", false, 3, 2)}}},
		monitors: []Spec{{Name: "deps", State: Healthy, Builds: []*Build{ended("deps", true, 0, 0.5)},
			Monitor: &Monitor{Result: "healthy"}}}}
	m := drive(t, src, "", sized(140, 30))
	shows(t, m, `╭ monitors ─+╮`, `│≡ all monitors *│`, `╭ recent builds `, `last 1 build · `, `size, tests`,
		` x +✓ landed +3 tests `)
	if regexp.MustCompile(` deps +✓ landed`).MatchString(screenOf(m)) {
		t.Errorf("the specs' side lists a fix:\n%s", screenOf(m))
	}

	m = sendAll(m, tea.KeyPressMsg{Code: tea.KeyTab})
	shows(t, m, `╭ recent fixes `, `last 1 fix · `, ` deps +✓ landed `)
	for _, gone := range []string{` x +✓ landed`, `size, tests`, `rounds`, `0 tests`} {
		if regexp.MustCompile(gone).MatchString(screenOf(m)) {
			t.Errorf("the monitors' side shows %s:\n%s", gone, screenOf(m))
		}
	}
}

// TestMonitorSummaryLines holds that each line of a monitor's summary has a row, up to three, above its builds. A
// cut summary ends in "…".
func TestMonitorSummaryLines(t *testing.T) {
	now := time.Now()
	fix := &Build{Spec: "deps", Started: now.Add(-time.Hour), Outcome: "stopped", Fix: true}
	src := source{monitors: []Spec{{Name: "deps", State: Stopped, Builds: []*Build{fix}, Monitor: &Monitor{
		Result: "stopped", Summary: "the session failed:\nAPI Error: 529 overloaded\nthird\nfourth"}}}}
	m := drive(t, src, "deps", sized(140, 30))
	shows(t, m, `│the session failed: *│\n.*│API Error: 529 overloaded *│\n.*│third … *│\n.*│ *started `,
		`#1 +✗ stopped`)
	if strings.Contains(screenOf(m), "fourth") {
		t.Errorf("the screen shows a summary's fourth line:\n%s", screenOf(m))
	}

	// A panel of three rows keeps one for a build, and marks the summary's cut.
	src.monitors[0].Builds = []*Build{fix, fix, fix, fix}
	m = drive(t, src, "deps", sized(100, 14))
	shows(t, m, `│the session failed: *│\n.*│API Error: 529 overloaded … *│\n.*│.*#\d +✗ stopped`)
}

// shows checks that m's screen matches each pattern.
func shows(t *testing.T, m model, patterns ...string) {
	t.Helper()
	for _, want := range patterns {
		if !regexp.MustCompile(want).MatchString(screenOf(m)) {
			t.Errorf("the screen shows no %s:\n%s", want, screenOf(m))
		}
	}
}

// TestMonitorNamedAsSpec holds that the model tells a monitor from a spec of the same name. A refresh keeps the
// monitor selected. Its running fix shows the monitor's state, and Enter, Enter opens the fix's run.
func TestMonitorNamedAsSpec(t *testing.T) {
	now := time.Now()
	build := &Build{Spec: "deps", Started: now.Add(-2 * time.Hour), Outcome: "accepted", Steps: []Step{
		&Run{Role: "build", Session: "build", Start: now.Add(-2 * time.Hour), End: now.Add(-time.Hour)},
	}}
	fix := &Build{Spec: "deps", Started: now.Add(-time.Minute), Fix: true, Steps: []Step{
		&Run{Role: "fix", Session: "fix", Start: now.Add(-time.Minute), To: -1},
	}}
	src := source{specs: []Spec{{Name: "deps", State: Accepted, Builds: []*Build{build}}}, monitors: []Spec{
		{Name: "deps", State: Running, Builds: []*Build{fix}, Monitor: &Monitor{Result: "running"}},
	}, calls: map[string]claude.Recounted{"build": {Answer: "the build's answer"}, "fix": {Answer: "the fix's answer"}}}
	m := drive(t, src, "", sized(140, 30), tea.KeyPressMsg{Code: tea.KeyTab}, press('j'), tick{})
	shows(t, m, `╭ fixes of deps · `, `#1 +▶ running `)

	m = sendAll(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if b := m.build(m.top().buildRef); m.top().kind != buildScreen || b != fix {
		t.Errorf("enter on the monitor opens screen %v of build %v, want its fix:\n%s", m.top().kind, b, screenOf(m))
	}

	m = sendAll(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	shows(t, m, `the fix's answer`)
}

// TestRoundsUnknown holds that a build whose rounds kiln could not add up is left out of the trend's median. The
// builds panel shows its rounds as unknown, never as 0.
func TestRoundsUnknown(t *testing.T) {
	now := time.Now()
	b := &Build{Spec: "x", Started: now, Outcome: "landed", Steps: []Step{&Run{Role: "build", Start: now, End: now}},
		Took: new(time.Duration(0)), Failed: new(0)}
	src := source{specs: []Spec{{Name: "x", State: UAT, Builds: []*Build{b}}}}
	tiles := `││cost 0/1 known +minutes +failed calls +rounds 0/1 known +size, tests *│\n.*││— +0 +0 +— +0 *│`
	if m := drive(t, src, "", sized(140, 30)); !regexp.MustCompile(tiles).MatchString(screenOf(m)) {
		t.Errorf("the trend counts the rounds:\n%s", screenOf(m))
	}

	if m := drive(t, src, "x", sized(140, 30)); !regexp.MustCompile(`#1 +✓ landed .* 0 +\? +│`).MatchString(
		screenOf(m)) {
		t.Errorf("the builds panel shows the rounds:\n%s", screenOf(m))
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
	m := drive(t, src, "", sized(120, 24))
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
			"▶ " + call.At.Local().Format("15:04:05") + " Bash", dark.agent.GetForeground()},
		{"an open call in an ended run was cut off", Run{End: now}, time.Second, []claude.Action{call}, "cut off",
			dark.failed.GetForeground()},
		{"a run whose kiln fire is gone shows it", Run{Lost: true}, time.Second, []claude.Action{call},
			"! kiln stopped mid-run", dark.kiln.GetForeground()},
		{"idle past a minute with no call open is marked", Run{To: -1}, 2 * time.Minute, nil, "idle 2:00",
			dark.you.GetForeground()},
		{"idle in a call is not", Run{To: -1}, 2 * time.Minute, []claude.Action{call}, "idle 2:00", nil},
		{"a refusal is red", Run{End: now}, time.Second, []claude.Action{{At: now, Tool: "Write", Input: "specs/x.md",
			Failed: true, Refused: true, Cause: "no"}}, "refused: no", dark.failed.GetForeground()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.run
			r.Role, r.Session, r.Why, r.Start = "build", "s", "first run", now.Add(-3*time.Minute)
			b := &Build{Spec: "x", Started: r.Start, Steps: []Step{&r}}
			src := source{specs: []Spec{{Name: "x", State: Running, Builds: []*Build{b}}},
				calls: map[string]claude.Recounted{"s": {Written: now.Add(-tc.written), Actions: tc.calls}}}
			m := drive(t, src, "x", sized(100, 20), press(tea.KeyEnter), press(tea.KeyEnter))
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
		"from a build": {sized(100, 20), press(tea.KeyEnter), press('s')},
		"from a run":   {sized(100, 20), press(tea.KeyEnter), press(tea.KeyEnter), press('s')},
	} {
		t.Run(name, func(t *testing.T) {
			if m := drive(t, src, "x", keys...); !strings.Contains(screenOf(m), "The approved text.") {
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

// dark is the theme of a terminal that has not said its background is light.
var dark = newTheme(true)

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
	specs, monitors []Spec
	// calls are each session's stretch of its stream.
	calls map[string]claude.Recounted
	text  string
}

func (s source) Specs(context.Context) ([]Spec, error)        { return slices.Clone(s.specs), nil }
func (s source) Recount(r *Run) (claude.Recounted, error)     { return s.calls[r.Session], nil }
func (s source) Text(context.Context, *Build) (string, error) { return s.text, nil }
func (s source) Monitors(context.Context) ([]Spec, error)     { return slices.Clone(s.monitors), nil }

// drive starts a model on src, opened on the spec name, and sends it each message in turn. It carries out each
// command the model answers. Its first message is the terminal's size, from sized.
func drive(t *testing.T, src Source, name string, msgs ...tea.Msg) model {
	t.Helper()
	m := newModel(src, name, 0)
	return sendAll(send(m, m.Init()), msgs...)
}

// sized is the size of a terminal of w by h cells.
func sized(w, h int) tea.Msg { return tea.WindowSizeMsg{Width: w, Height: h} }

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
			m := drive(t, src, "x", append([]tea.Msg{sized(35, 20), press('s')}, tc.keys...)...)
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
	m := drive(t, src, "x", sized(80, 12), press(tea.KeyEnter), press(tea.KeyEnter))
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
		return loaded{at: now, specs: src.specs, opened: runRef{buildRef{specRef{spec: "x"}, 0}, 0}, open: &late}
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

// TestNewBuild holds that a new build leaves both of home's selections where they were. So does a spec that moves
// in the list. The new build is listed first.
func TestNewBuild(t *testing.T) {
	now := time.Now()
	build := func(at time.Duration) *Build {
		return &Build{Spec: "x", Started: now.Add(at), Steps: []Step{&Run{Role: "build", Session: "s", Start: now,
			End: now}}}
	}

	x := Spec{Name: "x", State: UAT, Builds: []*Build{build(-2 * time.Hour), build(-time.Hour)}}
	src := source{specs: []Spec{x, {Name: "w", State: Queued}}}
	m := drive(t, src, "x", sized(100, 20), press(tea.KeyTab), press('j'))
	x.Builds = append(x.Builds, build(0))
	src.specs[0], src.specs[1] = src.specs[1], x
	m = send(m, func() tea.Msg { return tick{} })
	listed := regexp.MustCompile(`builds of x · approved 3 times .*\n.*\n.* #3 .*\n.* #2 .*\n.* #1 `)
	if !listed.MatchString(screenOf(m)) {
		t.Errorf("x's builds are not listed newest first:\n%s", screenOf(m))
	}

	if m = sendAll(m, press(tea.KeyEnter)); !strings.Contains(screenOf(m), "x · build 1 · ") {
		t.Errorf("enter opened another build than build 1:\n%s", screenOf(m))
	}
}

// TestSpecsPanel holds how home's specs panel narrows its builds. A spec selected lists only its own, and Enter on
// it opens its newest. The row for all specs lists every build. A click on a spec selects it, and opens nothing.
func TestSpecsPanel(t *testing.T) {
	now := time.Now()
	build := func(spec string, at time.Duration) *Build {
		return &Build{Spec: spec, Started: now.Add(at), Steps: []Step{&Run{Role: "build", Session: "s", Start: now}}}
	}

	src := source{specs: []Spec{{Name: "new", State: Queued},
		{Name: "x", State: UAT, Builds: []*Build{build("x", -3*time.Hour), build("x", -time.Hour)}},
		{Name: "y", State: UAT, Builds: []*Build{build("y", -2*time.Hour)}}}}
	m := drive(t, src, "", sized(100, 20))
	if !regexp.MustCompile(`recent builds .*\n.*\n.* x .*\n.* y .*\n.* x `).MatchString(screenOf(m)) {
		t.Errorf("home does not list every build, newest first:\n%s", screenOf(m))
	}

	x, y := locate(t, m, "◆ y")
	m = sendAll(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	switch shown := screenOf(m); {
	case len(m.stack) != 1, !strings.Contains(shown, "builds of y · approved once"), strings.Contains(shown, "#2"):
		t.Errorf("a click on y did not list y's one build on home:\n%s", shown)
	}

	if m = sendAll(m, press('k'), press(tea.KeyEnter)); !strings.Contains(screenOf(m), "x · build 2 · ") {
		t.Errorf("enter on x did not open its newest build:\n%s", screenOf(m))
	}

	m = sendAll(m, press(tea.KeyEscape), press('k'), press(tea.KeyEnter))
	switch {
	case len(m.stack) != 1, !strings.Contains(screenOf(m), "no build yet"):
		t.Errorf("enter on a spec with no build opened a screen:\n%s", screenOf(m))
	}
}

// TestKeyList holds that while the key list shows, only a key that closes it or quits does anything. The wheel does
// nothing. A click closes it, and opens nothing. On home, while the filter takes text, ? is text.
func TestKeyList(t *testing.T) {
	now := time.Now()
	b := &Build{Spec: "x", Started: now, Steps: []Step{&Run{Role: "build", Session: "s", Why: "first run", Start: now,
		End: now}}}
	src := source{specs: []Spec{{Name: "x", State: UAT, Builds: []*Build{b}}}}
	m := drive(t, src, "", sized(100, 20), press('?'), press('j'), press(tea.KeyTab),
		tea.MouseWheelMsg{X: 50, Y: 10, Button: tea.MouseWheelDown})
	switch {
	case !strings.Contains(screenOf(m), "? or esc closes this"), m.focus != specsPane, m.sel[specsPane] != 0:
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

// TestNarrow holds that in a narrow or short terminal, each footer still names ?, and esc where it goes back. The
// key list fits.
func TestNarrow(t *testing.T) {
	now := time.Now()
	b := &Build{Spec: "x", Started: now, Steps: []Step{&Run{Role: "build", Session: "s", Why: "first run", Start: now,
		End: now}}}
	src := source{specs: []Spec{{Name: "x", State: UAT, Builds: []*Build{b}}}}
	for _, keys := range [][]tea.Msg{nil, {press(tea.KeyEnter)}, {press(tea.KeyEnter), press(tea.KeyEnter)}} {
		rows := strings.Split(screenOf(drive(t, src, "x", append([]tea.Msg{sized(80, 24)}, keys...)...)), "\n")
		switch foot := rows[len(rows)-1]; {
		case !strings.Contains(foot, "? keys"), strings.Contains(foot, "esc back") != (len(keys) > 0),
			strings.Contains(foot, "…"):
			t.Errorf("the footer is %q", foot)
		}
	}

	m := drive(t, src, "x", sized(80, 12), press(tea.KeyEnter), press(tea.KeyEnter), press('?'))
	switch shown := screenOf(m); {
	case len(strings.Split(shown, "\n")) != 12, !strings.Contains(shown, "? or esc closes"),
		!strings.Contains(shown, "q ctrl+c"):
		t.Errorf("the key list does not fit 12 rows:\n%s", shown)
	}
}

// TestKeyListScrolls holds that a key list too wide for the terminal scrolls. Each j shows one key more, and k one
// key back. Its last line says more follow, until the last key shows. A list that fits starts at its first key.
func TestKeyListScrolls(t *testing.T) {
	now := time.Now()
	b := &Build{Spec: "x", Started: now, Steps: []Step{&Run{Role: "build", Session: "s", Why: "first run", Start: now,
		End: now}}}
	src := source{specs: []Spec{{Name: "x", State: UAT, Builds: []*Build{b}}}}
	keys := keyLines(runScreen)
	// shows finds a key's row in the box. It cuts each row at 100 columns, and reads runs of spaces as one.
	shows := func(m model, l keyLine) bool {
		for row := range strings.SplitSeq(screenOf(m), "\n") {
			cut := strings.Join(strings.Fields(ansi.Truncate(row, 100, "")), " ")
			if strings.Contains(cut, "│ "+l.keys+" "+l.desc+" │") {
				return true
			}
		}

		return false
	}

	for _, h := range []int{5, 7} {
		rows := h - 4
		m := drive(t, src, "x", sized(100, h), press(tea.KeyEnter), press(tea.KeyEnter), press('?'))
		for top := range len(keys) - rows + 1 {
			switch more := strings.Contains(screenOf(m), "↓ more"); {
			case len(strings.Split(screenOf(m), "\n")) != h, !shows(m, keys[top]), !shows(m, keys[top+rows-1]),
				top > 0 && shows(m, keys[top-1]), more != (top+rows < len(keys)):
				t.Fatalf("at %d rows, after %d j, the list does not start at %q:\n%s", h, top, keys[top].desc,
					screenOf(m))
			}

			m = sendAll(m, press('j'))
		}

		if m = sendAll(m, press('k')); !shows(m, keys[len(keys)-rows-1]) {
			t.Errorf("at %d rows, k does not show %q again:\n%s", h, keys[len(keys)-rows-1].desc, screenOf(m))
		}
	}

	m := drive(t, src, "x", sized(100, 30), press(tea.KeyEnter), press(tea.KeyEnter), press('?'), press('j'),
		press('j'), sized(100, 7))
	if !shows(m, keys[0]) {
		t.Errorf("j moved a list that fits, so it opens part way down after a resize:\n%s", screenOf(m))
	}
}

// TestGauges holds home's gauges panel: a row per gauge, with its newest line. Its readings follow, as kiln wrote
// them, each beside its spec. A crossed reading is red beside ✗. One read against another line shows it, and an
// unknown one shows as ?.
func TestGauges(t *testing.T) {
	now := time.Now()
	value := func(v float64) *float64 { return &v }
	landed := func(spec string, at time.Time, readings ...Reading) *Build {
		return &Build{Spec: spec, Started: at, Outcome: "landed", Readings: readings, Steps: []Step{
			&Run{Role: "build", Why: "first run", Start: at, End: at.Add(time.Minute)},
		}}
	}

	// billing's build started first, but checkout-flow's landing was read first, as an amended spec accepted late
	// lands.
	a := landed("checkout-flow", now.Add(-time.Hour),
		Reading{Gauge: "files changed", At: now.Add(-3 * time.Minute), Value: value(2), Line: "≤ 9"},
		Reading{Gauge: "mutation score", At: now.Add(-3 * time.Minute), Value: value(70.5), Line: "≥ 80",
			Crossed: true})
	b := landed("billing", now.Add(-2*time.Hour),
		Reading{Gauge: "files changed", At: now.Add(-2 * time.Minute), Value: value(4), Line: "≤ 3", Crossed: true},
		Reading{Gauge: "mutation score", At: now.Add(-2 * time.Minute), Line: "≥ 80"})
	src := source{specs: []Spec{{Name: "checkout-flow", State: UAT, Builds: []*Build{a}}, {Name: "billing",
		State: UAT, Builds: []*Build{b}}}}
	m := drive(t, src, "", sized(120, 30))
	// The trend above it keeps its tiles.
	for _, want := range []string{`╭ gauges ─+╮\n`,
		`│✗ files changed ≤ 3 · 1 of 2 above  checkout-flow 2 \(≤ 9\) · billing 4 +│\n`,
		`│\? mutation score ≥ 80 · 1 of 2 below  checkout-flow 70\.5 · billing \? +│\n`, `││newest `} {
		if !regexp.MustCompile(want).MatchString(screenOf(m)) {
			t.Errorf("no row matches %s on the screen\n%s", want, screenOf(m))
		}
	}

	// In a narrow pane, the panels stay apart, and a row too narrow for every reading keeps the newest.
	narrow := drive(t, src, "", sized(48, 20))
	for _, want := range []string{`│◆ checkout-fl… UAT││cost `, `│\? mutation score ≥ 80  bi…│\n`} {
		if !regexp.MustCompile(want).MatchString(screenOf(narrow)) {
			t.Errorf("no row matches %s on the screen\n%s", want, screenOf(narrow))
		}
	}

	if got := colorOf(t, m, "70.5"); !sameColor(got, dark.failed.GetForeground()) {
		t.Errorf("the crossed reading is drawn in %v, want red", got)
	}
}

// TestRepositoryGauge holds the gauges panel's row of a gauge that monitors name, below its row of landings. It holds
// their readings of the whole repository, oldest first. Two monitors share it. A monitor's readings alone show the
// panel, and a panel too short for both rows marks the cut.
func TestRepositoryGauge(t *testing.T) {
	now := time.Now()
	value := func(v float64) *float64 { return &v }
	read := func(ago time.Duration, v *float64, crossed bool) Reading {
		return Reading{Gauge: "health", At: now.Add(-ago), Value: v, Line: "≥ 10", Crossed: crossed}
	}

	monitors := []Spec{
		{Name: "code-health", State: Healthy, Monitor: &Monitor{Result: "healthy",
			Readings: []Reading{read(3*time.Hour, value(8), true), read(time.Hour, value(9), true)}}},
		{Name: "lint", State: Healthy, Monitor: &Monitor{Result: "couldn't run",
			Readings: []Reading{read(2*time.Hour, nil, false)}}},
	}

	alone := drive(t, source{monitors: monitors}, "", sized(120, 30))
	want := `│✗ health, repository ≥ 10 · 2 of 3 below  8 · \? · 9 +│\n`
	if !regexp.MustCompile(want).MatchString(screenOf(alone)) {
		t.Errorf("no row matches %s on the screen\n%s", want, screenOf(alone))
	}

	landed := &Build{Spec: "languages", Started: now, Outcome: "landed",
		Readings: []Reading{read(30*time.Minute, value(9), true)}}
	src := source{specs: []Spec{{Name: "languages", State: UAT, Builds: []*Build{landed}}}, monitors: monitors}
	both := drive(t, src, "", sized(120, 30))
	want = `│✗ health ≥ 10 · 1 of 1 below  languages 9 +│\n[│ ]+` +
		`│✗ health, repository ≥ 10 · 2 of 3 below  8 · \? · 9 +│\n`
	if !regexp.MustCompile(want).MatchString(screenOf(both)) {
		t.Errorf("no rows match %s on the screen\n%s", want, screenOf(both))
	}

	// A panel too short for both rows says it cut one.
	short := drive(t, src, "", sized(120, 20))
	if want := `╭ gauges \+1 ─+╮\n`; !regexp.MustCompile(want).MatchString(screenOf(short)) {
		t.Errorf("no row matches %s on the screen\n%s", want, screenOf(short))
	}
}

// TestFixOutcomes holds that a failed fix is red beside ✗. A fix that met a usage limit is in a session's color,
// beside ▶. The specs panel marks its monitor the same.
func TestFixOutcomes(t *testing.T) {
	for outcome, ended := range map[string]State{
		"session failed": Failed, "couldn't run": CouldNotRun, "usage limit": Limited,
	} {
		want, glyph := dark.failed.GetForeground(), "✗"
		if ended == Limited {
			want, glyph = dark.agent.GetForeground(), "▶"
		}

		fix := &Build{Spec: "deps", Started: time.Now().Add(-time.Hour), Outcome: outcome, Fix: true, Ended: ended}
		src := source{specs: []Spec{{Name: "x", State: UAT}}, monitors: []Spec{{Name: "deps", State: ended,
			Builds: []*Build{fix}, Monitor: &Monitor{Result: outcome}}}}
		// The selected row is reversed, so the specs panel's color is read with x selected.
		specs, builds := drive(t, src, "", sized(140, 30)), drive(t, src, "deps", sized(140, 30))
		shows(t, specs, `│`+glyph+` deps +`+string(ended)+` *│`)
		shows(t, builds, `#1 +`+glyph+` `+outcome)
		for text, m := range map[string]model{glyph + " deps": specs, glyph + " " + outcome: builds} {
			if got := colorOf(t, m, text); !sameColor(got, want) {
				t.Errorf("%q is drawn in %v, want %v", text, got, want)
			}
		}
	}
}

// TestGaugeRowCost holds that readings too old to fit add no allocations to a gauge's row, and never show. 40 times
// the readings allocate under twice the bytes, and show the same newest ones. A count of bytes, unlike a time, ignores
// the load.
func TestGaugeRowCost(t *testing.T) {
	start := time.Now()
	draw := func(n int) (string, uint64) {
		rs := make([]Reading, n)
		for i := range rs {
			v := float64(i % 10)
			rs[i] = Reading{Gauge: "health", At: start.Add(time.Duration(i) * time.Hour), Value: &v, Line: "≥ 10"}
		}

		specs := []Spec{{Name: "code-health", Monitor: &Monitor{Readings: rs}}}
		cost := uint64(1<<64 - 1)
		var text string
		for range 3 {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			text = model{specs: specs, theme: dark}.gaugeRows(150)[0].text
			runtime.ReadMemStats(&after)
			cost = min(cost, after.TotalAlloc-before.TotalAlloc)
		}

		return text, cost
	}

	few, fewCost := draw(500)
	many, manyCost := draw(20_000)
	if many != few {
		t.Errorf("20,000 readings show\n%s\nwant the newest, as 500 show\n%s", many, few)
	}

	if manyCost >= 2*fewCost {
		t.Errorf("20,000 readings allocated %d bytes, 500 allocated %d: want under twice as many", manyCost, fewCost)
	}
}

// TestNarrowGauge holds that a terminal narrower than the specs panel still draws a monitor's newest reading.
func TestNarrowGauge(t *testing.T) {
	v := 9.0
	rs := make([]Reading, 5)
	for i := range rs {
		rs[i] = Reading{Gauge: "health", At: time.Now().Add(time.Duration(i) * time.Hour), Value: &v, Line: "≥ 10"}
	}

	src := source{specs: []Spec{{Name: "x", State: UAT}},
		monitors: []Spec{{Name: "code-health", Monitor: &Monitor{Readings: rs}}}}
	for _, w := range []int{20, 21} {
		if m := drive(t, src, "", sized(w, 30)); !strings.Contains(screenOf(m), "health") {
			t.Errorf("at %d columns, no gauge row is on the screen\n%s", w, screenOf(m))
		}
	}
}

// TestNowLanes holds that the now pane shows the run at work in each lane, the spec's above the fix's. A fix's
// session that sits idle shows there too, marked as idle. Enter opens the run selected there.
func TestNowLanes(t *testing.T) {
	now := time.Now()
	build := &Run{Role: "build", Session: "b", Why: "first run", Start: now, To: -1}
	fix := &Run{Role: "deps", Session: "f", Why: "first run", Start: now.Add(-10 * time.Minute), To: -1}
	src := source{
		specs: []Spec{{Name: "x", State: Running, Builds: []*Build{{Spec: "x", Steps: []Step{build}}}}},
		monitors: []Spec{{Name: "deps", State: Running, Monitor: &Monitor{Result: "fix building"},
			Builds: []*Build{{Spec: "deps", Fix: true, Steps: []Step{fix}}}}},
		calls: map[string]claude.Recounted{"b": {Written: now}, "f": {Written: now.Add(-5 * time.Minute)}},
	}

	m := drive(t, src, "", sized(140, 30))
	_, specAt := locate(t, m, "▶ x · build role")
	_, fixAt := locate(t, m, "▶ deps · deps role")
	_, idleAt := locate(t, m, "▶ idle 5:00, in no call")
	if specAt >= fixAt || idleAt != fixAt+1 {
		t.Errorf("the spec's run is on row %d, the fix's on %d, and its idle line on %d; want the spec's above, "+
			"and the fix's idle line below its own:\n%s", specAt, fixAt, idleAt, screenOf(m))
	}

	m = sendAll(m, press(tea.KeyTab), press(tea.KeyTab), press('j'), press(tea.KeyEnter))
	if s := m.top(); s.kind != runScreen || s.spec != "deps" {
		t.Errorf("enter on the fix's run opened screen %v of %q, want the fix's run:\n%s", s.kind, s.spec,
			screenOf(m))
	}
}
