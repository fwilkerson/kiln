package tui

import (
	"fmt"
	"slices"
	"strings"
)

// The trend's tiles are tileGap cells apart. A panel too narrow to give each tile tileMin cells draws a table.
const (
	tileGap = 2
	tileMin = 12
)

// trendTitle names the builds the trend holds, and the builds before them when there are some.
func trendTitle(recent, previous []*Build) string {
	switch {
	case len(recent) == 0:
		return "trend"
	case len(previous) == 0:
		return fmt.Sprintf("last %s · median per build", plural(len(recent), "build"))
	}

	return fmt.Sprintf("last %s · median per build, beside the %d before", plural(len(recent), "build"), len(previous))
}

// trendRows draws the trend in w cells: a tile for each measure, side by side. Too narrow for tiles, it is a table
// with no bars.
func (m model) trendRows(w int) []row {
	recent, previous := trend(m.specs)
	tileW := (w - tileGap*(len(measures)-1)) / len(measures)
	switch {
	case len(recent) == 0:
		return []row{{text: m.theme.dim.Render("no build has ended yet")}}
	case tileW < tileMin:
		return trendTable(m.theme, recent, previous)
	}

	tiles := make([][]string, len(measures))
	for i, ms := range measures {
		tiles[i] = ms.tile(m.theme, recent, previous, tileW)
	}

	rows := make([]row, len(tiles[0]))
	for y := range rows {
		cells := make([]string, len(tiles))
		for i, t := range tiles {
			cells[i] = fit(t[y], tileW)
		}

		rows[y].text = strings.Join(cells, strings.Repeat(" ", tileGap))
	}

	return rows
}

// trendTable is the trend as a row for each measure: its name, its median, and the median before.
func trendTable(t theme, recent, previous []*Build) []row {
	rows := make([]row, len(measures))
	for i, ms := range measures {
		rows[i].text = cols([]int{12, 18}, ms.short, ms.headline(t, recent, previous),
			t.dim.Render(ms.before(previous)))
	}

	return rows
}

// tile is m's rows of the trend, each at most w cells. They are its name, its median, and its bars in two rows. Below
// them are the newest build's number, in the color of its bar, then the lowest and the highest. With builds before
// the recent ones, their median is its last row.
func (m measure) tile(t theme, recent, previous []*Build, w int) []string {
	top, bottom := m.bars(t, recent, w)
	newest := "?"
	if v, ok := m.of(recent[len(recent)-1]); ok {
		newest = m.shown(v)
	}

	lines := []string{t.bold.Render(m.short), m.headline(t, recent, previous), top, bottom,
		t.kiln.Render("newest " + newest), t.dim.Render(m.spread(recent))}
	if len(previous) > 0 {
		lines = append(lines, t.dim.Render(m.before(previous)))
	}

	return lines
}

// headline is m's median over the recent builds. One worse than before is red beside ✗, and a better one green
// beside ✓. Where some builds do not know m, it says how many do.
func (m measure) headline(t theme, recent, previous []*Build) string {
	shown, known := m.shownMedian(recent)
	switch m.change(recent, previous) {
	case 1:
		shown = t.failed.Render("✗ " + shown)
	case -1:
		shown = t.passed.Render("✓ " + shown)
	default:
		shown = t.bold.Render(shown)
	}

	if known < len(recent) {
		shown += t.dim.Render(fmt.Sprintf("  %d of %d", known, len(recent)))
	}

	return shown
}

// before is m's median over the builds before the recent ones, or nothing when there are none.
func (m measure) before(previous []*Build) string {
	if len(previous) == 0 {
		return ""
	}

	was, _ := m.shownMedian(previous)
	return "before " + was
}

// shownMedian is the median of m over the builds that know it, as shown, and how many do. It is — when none does.
func (m measure) shownMedian(builds []*Build) (string, int) {
	median, known := m.median(builds)
	if known == 0 {
		return "—", 0
	}

	return m.shown(median), known
}

// spread is the lowest and the highest m among the builds that know it, or nothing when none does.
func (m measure) spread(builds []*Build) string {
	values := m.values(builds)
	if len(values) == 0 {
		return ""
	}

	return m.shown(slices.Min(values)) + " – " + m.shown(slices.Max(values))
}

// bars draws m as a bar for each build, oldest first, in at most w cells. Each bar is two rows high: bars answers
// the top row, then the bottom. A bar is as high as its build's share of the largest. The newest is in kiln's color,
// and the rest are dim. A build that does not know m draws a dot. Where they fit, the bars widen and stand a cell
// apart.
func (m measure) bars(t theme, builds []*Build, w int) (top, bottom string) {
	barW, gap := 1, ""
	if n := len(builds); n > 0 && w >= 2*n-1 {
		barW, gap = min(3, (w-n+1)/n), " "
	}

	most := slices.Max(append(m.values(builds), 0))
	var tops, bottoms []string
	for i, b := range builds {
		style := t.dim
		if i == len(builds)-1 {
			style = t.kiln
		}

		hi, lo := " ", "·"
		if v, ok := m.of(b); ok {
			hi, lo = bar(v, most)
		}

		tops = append(tops, style.Render(strings.Repeat(hi, barW)))
		bottoms = append(bottoms, style.Render(strings.Repeat(lo, barW)))
	}

	return strings.Join(tops, gap), strings.Join(bottoms, gap)
}

// bar is a bar's two cells, the top then the bottom, for v's share of most, in 16 steps. A zero draws the lowest
// step, so its build still shows.
func bar(v, most float64) (top, bottom string) {
	steps := []rune(" ▁▂▃▄▅▆▇█")
	level := 1
	if most > 0 {
		level = max(1, int(v/most*16+0.5))
	}

	return string(steps[max(0, level-8)]), string(steps[min(8, level)])
}
