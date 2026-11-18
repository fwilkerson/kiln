package tui

import (
	"cmp"
	"fmt"
	"image/color"
	"math"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/vt"

	"github.com/fwilkerson/kiln/internal/claude"
)

// TestContrast holds that every screen reads on the terminal's background, dark or light, once the terminal says
// which. A glyph reaches 4.5 to 1 against its background, and a border or a bar 3 to 1.
func TestContrast(t *testing.T) {
	src := scene(time.Now())
	screens := map[string][]tea.Msg{
		"home":     nil,
		"build":    {press(tea.KeyEnter)},
		"run":      {press(tea.KeyEnter), press(tea.KeyDown), press(tea.KeyEnter)},
		"spec":     {press('s')},
		"key list": {press('?')},
	}
	for _, bg := range []string{"#000000", "#110b09", "#282c34", "#eeeeee", "#ffffff"} {
		for name, keys := range screens {
			t.Run(bg+" "+name, func(t *testing.T) {
				msgs := append([]tea.Msg{sized(140, 40), tea.BackgroundColorMsg{Color: lipgloss.Color(bg)}}, keys...)
				for _, low := range lowContrast(drive(t, src, "x", msgs...), lipgloss.Color(bg)) {
					t.Error(low)
				}
			})
		}
	}
}

// lowContrast lists each glyph on m's screen whose color falls short against its background, once per color pair.
func lowContrast(m model, terminal color.Color) []string {
	emu := vt.NewEmulator(m.w, m.h)
	_, _ = emu.WriteString(strings.ReplaceAll(m.View().Content, "\n", "\r\n"))
	seen := map[string]bool{}
	var low []string
	for y := range m.h {
		for x := range m.w {
			c := emu.CellAt(x, y)
			if c == nil || c.Style.Fg == nil {
				continue
			}

			got, need := legibility(c.Content, c.Style.Fg, cmp.Or(c.Style.Bg, terminal))
			key := fmt.Sprint(c.Style.Fg, c.Style.Bg, need)
			if got < need && !seen[key] {
				seen[key] = true
				low = append(low, fmt.Sprintf("%q at %d,%d reaches %.2f to 1, want %.1f", c.Content, x, y, got, need))
			}
		}
	}

	return low
}

// legibility is the contrast of glyph, drawn in fg on bg, and the least it needs. A blank needs none. A border or a
// bar needs 3 to 1, and any other glyph 4.5.
func legibility(glyph string, fg, bg color.Color) (got, need float64) {
	got = contrast(fg, bg)
	switch r := []rune(strings.TrimSpace(glyph)); {
	case len(r) == 0:
		return got, 0
	case r[0] >= '─' && r[0] <= '▟':
		return got, 3
	}

	return got, 4.5
}

// contrast is the WCAG 2 contrast ratio of two colors, from 1 to 21.
func contrast(a, b color.Color) float64 {
	la, lb := luminance(a), luminance(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	linear := func(v uint32) float64 {
		s := float64(v) / 0xffff
		if s <= 0.04045 {
			return s / 12.92
		}

		return math.Pow((s+0.055)/1.055, 2.4)
	}

	return 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b)
}

// scene is a source whose screens draw every style: each state, a monitor, gauges, and Markdown with code.
func scene(now time.Time) source {
	cost := func(usd float64) claude.Totals {
		turns := 12
		return claude.Totals{CostUSD: &usd, Turns: &turns}
	}

	at := func(ago time.Duration) time.Time { return now.Add(-ago) }
	var ended []*Build
	for i := range 12 {
		start := at(time.Duration(40-i) * time.Hour)
		ended = append(ended, &Build{Spec: "y", Started: start, Size: 3, Outcome: "landed", Steps: []Step{
			&Run{Role: "build", Why: "first run", Start: start, End: start.Add(time.Duration(9+i%4) * time.Minute),
				Totals: cost(1 + float64(i%5)/2)},
		}})
	}

	worst, best := 61.0, 84.0
	failing := cost(2.5)
	failing.Failed = []claude.Failure{{Tool: "Bash", Input: "go test", Cause: "FAIL x"}}
	landed := &Build{Spec: "x", Approved: "3f9c2ab81d0e", Started: at(3 * time.Hour), Size: 4, Outcome: "landed",
		Readings: []Reading{{Gauge: "mutation score", At: at(2 * time.Hour), Value: &worst, Line: "≥ 80", Crossed: true}},
		Steps: []Step{
			&Run{Role: "build", Session: "a", Why: "first run", Start: at(3 * time.Hour), End: at(170 * time.Minute),
				Totals: failing},
			&Gate{Start: at(170 * time.Minute), End: at(168 * time.Minute)},
			&Run{Role: "audit", Session: "b", Why: "round 1 of 3", Start: at(168 * time.Minute),
				End: at(160 * time.Minute), Changed: []string{"x.go"}, Totals: cost(0.8)},
			&Gate{Start: at(160 * time.Minute), End: at(158 * time.Minute), Passed: true, Landing: true},
		}}
	running := &Build{Spec: "x", Approved: "9e1d47c0aa12", Started: at(5 * time.Minute), Size: 4, Steps: []Step{
		&Run{Role: "build", Session: "c", Why: "first run", Start: at(5 * time.Minute), To: -1},
		&Run{Role: "verify", Session: "d", Why: "first run", Start: at(5 * time.Minute), End: at(time.Minute),
			Lost: true},
	}}
	return source{
		specs: []Spec{
			{Name: "x", State: Running, Builds: []*Build{landed, running}},
			{Name: "y", State: Accepted, Builds: ended},
			{Name: "z", State: UAT, Builds: []*Build{{Spec: "z", Started: at(time.Hour), Outcome: "landed",
				Readings: []Reading{{Gauge: "mutation score", At: at(time.Hour), Value: &best, Line: "≥ 80"}},
				Steps:    []Step{&Run{Role: "build", Start: at(time.Hour), End: at(50 * time.Minute), Totals: cost(1.2)}}}}},
			{Name: "paused", State: Paused}, {Name: "halted", State: Halted}, {Name: "stopped", State: Stopped},
			{Name: "rejected", State: Rejected}, {Name: "queued", State: Queued},
		},
		monitors: []Spec{{Name: "docs", State: NeedsYou, Monitor: &Monitor{Result: "held: waiting on the developer"}}},
		calls: map[string]claude.Recounted{"c": {Written: at(2 * time.Minute), Answer: "## Built x\n\nIt answers `42`.",
			Actions: []claude.Action{
				{At: at(4 * time.Minute), Input: "Reading the spec."},
				{At: at(3 * time.Minute), Tool: "Write", Input: "specs/x.md", Failed: true, Refused: true, Cause: "no"},
				{At: at(3 * time.Minute), Tool: "Bash", Input: "go vet", Failed: true, Took: time.Second, Cause: "x"},
				{At: at(2 * time.Minute), Tool: "Read", Input: "x.go", Took: time.Second},
			}}},
		text: "# x\n\n### 1. X answers\n\n- **Given**: nothing.\n- **Then**: it answers `42`, as *the spec* says." +
			"\n\n```go\n// X answers.\nfunc X() int { return 42 + len(\"x\") }\n```\n",
	}
}
