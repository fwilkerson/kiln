package main

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"kilntui/ledger"
)

// app is the screen's state. Its draw and handle are pure, so a test drives them without a terminal.
type app struct {
	builds []ledger.Build
	sel    int
	open   bool
	w, h   int
}

func newApp(builds []ledger.Build) *app { return &app{builds: builds, sel: len(builds) - 1} }

type rect struct{ x, y, w, h int }

func (r rect) holds(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

// layout splits the screen: the table on top, the chart and the selected build beneath. Both draw and click use it,
// so a click lands where the frame drew.
func (a *app) layout() (table, chart, panel rect) {
	th := max(min(len(a.builds)+3, a.h-8), 4)
	half := a.w / 2

	return rect{0, 0, a.w, th}, rect{0, th, half, a.h - th}, rect{half, th, a.w - half, a.h - th}
}

// handle applies one event, and reports whether the app should quit.
func (a *app) handle(e event) (quit bool) {
	switch {
	case e.key == "q" || e.key == "ctrl+c":
		return true
	case a.open:
		a.open = e.key != "esc"
	case e.key == "up" || e.key == "k" || e.mouse == wheelUp:
		a.sel = max(a.sel-1, 0)
	case e.key == "down" || e.key == "j" || e.mouse == wheelDown:
		a.sel = min(a.sel+1, len(a.builds)-1)
	case e.key == "enter":
		a.open = true
	case e.mouse == click:
		a.click(e.x, e.y)
	}

	return false
}

// click selects the row or bar under column x, row y. A click on the row already selected opens its build.
func (a *app) click(x, y int) {
	table, chart, _ := a.layout()
	if i := a.offset(table) + y - table.y - 2; table.holds(x, y) && y >= table.y+2 && y < table.y+table.h-1 &&
		i < len(a.builds) {
		a.open = i == a.sel
		a.sel = i
	}

	slot := a.slot(chart)
	if i := (x - chart.x - 1) / slot; chart.holds(x, y) && x > chart.x && i < len(a.builds) {
		a.sel = i
	}
}

// offset answers the first build the table shows, so the selected row stays in view on a short screen.
func (a *app) offset(table rect) int { return max(0, a.sel-(table.h-3)+1) }

// slot answers the columns each bar takes, its gap included.
func (a *app) slot(chart rect) int { return min(max((chart.w-2)/len(a.builds), 2), 6) }

func (a *app) draw() *canvas {
	c := newCanvas(a.w, a.h)
	if a.open {
		a.drawBuild(c, rect{0, 0, a.w, a.h})
		return c
	}

	table, chart, panel := a.layout()
	a.drawTable(c, table)
	a.drawChart(c, chart)
	a.drawSelected(c, panel)

	return c
}

const row = "%-2v  %-10v  %-14v  %-22v  %4v  %6v  %8v  %6v"

func (a *app) drawTable(c *canvas, r rect) {
	c.box(r, "builds")
	c.text(r.x+2, r.y+1, fmt.Sprintf(row, "#", "started", "spec", "outcome", "size", "time", "cost", "failed"), plain)
	for i := a.offset(r); i < len(a.builds) && r.y+2+i-a.offset(r) < r.y+r.h-1; i++ {
		b, st := a.builds[i], plain
		if i == a.sel {
			st = reverse
		}

		line := fmt.Sprintf(row, i+1, b.Started.Format("Mon Jan 2"), b.Spec, b.Outcome, b.Size, minutes(b.Time),
			fmt.Sprintf("$%.2f", b.Cost), b.Failed())
		c.text(r.x+1, r.y+2+i-a.offset(r), fmt.Sprintf(" %-*s", r.w-3, line), st)
	}
}

// drawChart draws one bar per build, scaled to the costliest, with each build's number beneath its bar.
func (a *app) drawChart(c *canvas, r rect) {
	c.box(r, "cost per build")
	top := slices.MaxFunc(a.builds, func(x, y ledger.Build) int { return cmp.Compare(x.Cost, y.Cost) }).Cost
	slot, axis := a.slot(r), r.y+r.h-2
	for i, b := range a.builds {
		x, bar, label := r.x+1+i*slot, accent, plain
		if i == a.sel {
			bar, label = plain, reverse
		}

		height := max(int(math.Round(b.Cost/top*float64(r.h-4))), 1)
		for k := range height {
			c.text(x, axis-1-k, strings.Repeat("█", slot-1), bar)
		}

		c.text(x, axis, fmt.Sprint(i+1), label)
	}
}

func (a *app) drawSelected(c *canvas, r rect) {
	b := a.builds[a.sel]
	c.box(r, "selected")
	lines := []string{
		b.Spec + " · " + b.Outcome,
		"started  " + b.Started.Format("Mon Jan 2 15:04"),
		fmt.Sprintf("size     %d acceptance tests", b.Size),
		fmt.Sprintf("time     %s · %s waiting · %s in gates", minutes(b.Time), minutes(b.Waiting), minutes(b.Gates)),
		fmt.Sprintf("cost     $%.2f · %d turns", b.Cost, b.Turns),
		fmt.Sprintf("failed   %d calls", b.Failed()),
	}
	c.lines(r, lines)
}

// drawBuild draws the open build: each role, then its failed calls by cause.
func (a *app) drawBuild(c *canvas, r rect) {
	b := a.builds[a.sel]
	c.box(r, b.Spec+" · "+b.Outcome)
	lines := []string{"by role", fmt.Sprintf("  %-6s  %4s  %5s  %7s  %6s  %-20s  %s", "role", "runs", "turns",
		"cost", "time", "context", "tools")}
	for _, role := range b.Roles {
		lines = append(lines, fmt.Sprintf("  %-6s  %4d  %5d  %7s  %6s  %-20s  %s", role.Name, role.Runs, role.Turns,
			fmt.Sprintf("$%.2f", role.Cost), minutes(role.Time), context(role.Context), tools(role.ToolUses)))
	}

	lines = append(lines, "", "by cause")
	if len(b.Causes) == 0 {
		lines = append(lines, "  no failed calls")
	}

	for _, cause := range b.Causes {
		lines = append(lines, fmt.Sprintf("  %3d  %s", cause.Count, cause.Text), "       "+cause.Input)
	}

	c.lines(r, lines)

	c.text(r.x+2, r.y+r.h-2, "esc back · q quit", accent)
}

func minutes(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}

	return fmt.Sprintf("%.1fm", d.Minutes())
}

// context shows a session's context at its start, peak and end, in thousands of tokens.
func context(tokens []int) string {
	parts := make([]string, len(tokens))
	for i, t := range tokens {
		parts[i] = fmt.Sprintf("%dk", t/1000)
	}

	return strings.Join(parts, " → ")
}

// tools lists tool calls by count, most first, so the costliest tool reads first.
func tools(uses map[string]int) string {
	names := slices.SortedFunc(maps.Keys(uses), func(x, y string) int {
		return cmp.Or(cmp.Compare(uses[y], uses[x]), cmp.Compare(x, y))
	})

	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = fmt.Sprintf("%d %s", uses[name], name)
	}

	return strings.Join(parts, " · ")
}
