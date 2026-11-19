package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The trend's tiles are tileGap cells apart. A panel too narrow to give each tile tileMin cells draws a table.
const (
	tileGap = 2
	tileMin = 12
)

// trendTitle names the builds the trend holds, and the builds before them when there are some.
func (m model) trendTitle(recent, previous []*Build) string {
	builds := m.side.noun(len(recent))
	if s, ok := m.picked(); ok {
		builds += " of " + s.Name
	}

	switch {
	case len(recent) == 0:
		return "trend"
	case len(previous) == 0:
		return fmt.Sprintf("last %d %s · median of each", len(recent), builds)
	}

	return fmt.Sprintf("last %d %s · median of each, and how it moved from the %d before", len(recent), builds,
		len(previous))
}

// measures are the trend's measures of what the side panel lists. A fix has no tests, and no rounds.
func (m model) measures() []measure {
	if m.side != monitorsPane {
		return measures
	}

	return slices.DeleteFunc(slices.Clone(measures), func(ms measure) bool { return ms.specOnly })
}

// trendRows draws the trend in w cells: a tile for each measure, side by side. Too narrow for tiles, it is a table
// with no bars.
func (m model) trendRows(w int) []row {
	recent, previous := trend(m.shown())
	ms := m.measures()
	tileW := (w - tileGap*(len(ms)-1)) / len(ms)
	switch {
	case len(recent) == 0:
		return []row{{text: m.theme.dim.Render("no " + m.side.noun(1) + " has ended yet")}}
	case tileW < tileMin:
		return trendTable(m.theme, ms, recent, previous)
	}

	tiles := make([][]string, len(ms))
	for i, ms := range ms {
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

// trendTable is the trend as a row for each measure: its name, then its median and how it moved.
func trendTable(t theme, ms []measure, recent, previous []*Build) []row {
	rows := make([]row, len(ms))
	for i, m := range ms {
		median, was := m.headline(t, recent, previous)
		rows[i].text = cols([]int{12}, m.short, strings.TrimSpace(median+" "+was))
	}

	return rows
}

// tile is m's rows of the trend, each at most w cells. They are its name, its headline, and its bars in two rows.
// Below them are the newest build's number, in the color of its bar, then the lowest and the highest. A tile too
// narrow for its headline on one row gives the median before the last row instead.
func (m measure) tile(t theme, recent, previous []*Build, w int) []string {
	top, bottom := m.bars(t, recent, w)
	newest := "?"
	if v, ok := m.of(recent[len(recent)-1]); ok {
		newest = m.shown(v)
	}

	name := t.bold.Render(m.short)
	if known := len(m.values(recent)); known < len(recent) {
		name += t.dim.Render(fmt.Sprintf(" %d/%d known", known, len(recent)))
	}

	median, was := m.headline(t, recent, previous)
	last := t.dim.Render(m.spread(recent))
	switch {
	case was == "":
	case ansi.StringWidth(median+" "+was) <= w:
		median += " " + was
	default:
		last = was
	}

	return []string{name, median, top, bottom, t.kiln.Render("newest " + newest), last}
}

// headline is m's median over the recent builds, then the median of the builds before. A rise puts ▲ beside the
// median, and a fall ▼. Each is red for the worse and green for the better. The median before is "same" where both
// show alike, and "" where either is unknown.
func (m measure) headline(t theme, recent, previous []*Build) (median, was string) {
	shown, _ := m.shownMedian(recent)
	median = t.bold.Render(shown)
	now, known := m.median(recent)
	before, knownBefore := m.median(previous)
	switch {
	case known == 0, knownBefore == 0:
		return median, ""
	case m.shown(now) == m.shown(before):
		return median, t.dim.Render("same")
	}

	arrow, style := "▲", t.failed
	if now < before {
		arrow, style = "▼", t.passed
	}

	if !m.judged {
		style = t.dim
	}

	return median + " " + style.Render(arrow), style.Render("was " + m.shown(before))
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

	return "range " + m.shown(slices.Min(values)) + "–" + m.shown(slices.Max(values))
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
