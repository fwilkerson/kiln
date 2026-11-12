package tui

import (
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// landingRead is a reading, and the spec whose landing it read.
type landingRead struct {
	Reading
	spec string
}

// readings are the specs' readings by gauge, each gauge's in the order kiln wrote them. names lists the gauges, sorted.
func readings(specs []Spec) (names []string, byGauge map[string][]landingRead) {
	byGauge = map[string][]landingRead{}
	for _, s := range specs {
		for _, b := range s.Builds {
			for _, r := range b.Readings {
				byGauge[r.Gauge] = append(byGauge[r.Gauge], landingRead{r, b.Spec})
			}
		}
	}

	for name, rs := range byGauge {
		slices.SortStableFunc(rs, func(a, b landingRead) int { return a.At.Compare(b.At) })
		names = append(names, name)
	}

	slices.Sort(names)
	return names, byGauge
}

// gaugeRows draws a row for each gauge in w cells: its name, its newest line, then its readings. A row too narrow for
// them all keeps the newest that fit, and always the newest. It answers none while no build has a reading.
func gaugeRows(specs []Spec, w int) []row {
	names, byGauge := readings(specs)
	rows := make([]row, len(names))
	for i, name := range names {
		rs := byGauge[name]
		line := rs[len(rs)-1].Line
		head := bold.Render(name) + " " + dim.Render(line)
		cells := make([]string, len(rs))
		for j, r := range rs {
			cells[j] = r.shown(line)
		}

		rows[i].text = head + "  " + newestFitting(cells, w-ansi.StringWidth(head)-2)
	}

	return rows
}

// shown is the reading beside its spec. One that crossed its line is red beside ✗. One read against a line other
// than line shows its own.
func (r landingRead) shown(line string) string {
	value := "?"
	if r.Value != nil {
		value = strconv.FormatFloat(*r.Value, 'f', -1, 64)
	}

	if r.Crossed {
		value = red.Render("✗ " + value)
	}

	if r.Line != line {
		value += dim.Render(" (" + r.Line + ")")
	}

	return r.spec + " " + value
}

// newestFitting joins the last of cells that fit in w cells, oldest first. A … marks that older ones are left out.
// Where not even the newest fits, it is the newest alone, which the panel then cuts.
func newestFitting(cells []string, w int) string {
	sep := dim.Render(" · ")
	for from := range cells {
		joined := strings.Join(cells[from:], sep)
		if from > 0 {
			joined = dim.Render("… ") + joined
		}

		if ansi.StringWidth(joined) <= w {
			return joined
		}
	}

	return cells[len(cells)-1]
}
