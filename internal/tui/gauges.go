package tui

import (
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// landingRead is a reading, and the spec whose landing it read. A reading of the whole repository has no spec.
type landingRead struct {
	Reading
	spec string
}

// gaugeReads are the readings of the gauges, each gauge's in the order kiln wrote them.
type gaugeReads map[string][]landingRead

// add adds r, of spec's landing, or of the whole repository where spec is "".
func (g gaugeReads) add(r Reading, spec string) {
	g[r.Gauge] = append(g[r.Gauge], landingRead{r, spec})
}

// readings are the readings by gauge: the specs' of their landings, and the monitors' of the whole repository. Each
// gauge's are in the order kiln wrote them. names lists the gauges, sorted.
func readings(specs []Spec) (names []string, landings, repository gaugeReads) {
	landings, repository = gaugeReads{}, gaugeReads{}
	for _, s := range specs {
		for _, b := range s.Builds {
			for _, r := range b.Readings {
				landings.add(r, b.Spec)
			}
		}

		if s.Monitor != nil {
			for _, r := range s.Monitor.Readings {
				repository.add(r, "")
			}
		}
	}

	for _, g := range []gaugeReads{landings, repository} {
		for name, rs := range g {
			slices.SortStableFunc(rs, func(a, b landingRead) int { return a.At.Compare(b.At) })
			names = append(names, name)
		}
	}

	slices.Sort(names)
	return slices.Compact(names), landings, repository
}

// gaugeRows draws, in w cells, a row for each gauge's readings of landings. Below it is a row of its readings of the
// whole repository, where a monitor has read it. It answers none while no build or monitor has a reading.
func gaugeRows(specs []Spec, w int) []row {
	names, landings, repository := readings(specs)
	var rows []row
	for _, name := range names {
		if rs := landings[name]; len(rs) > 0 {
			rows = append(rows, gaugeRow(name, rs, w))
		}

		if rs := repository[name]; len(rs) > 0 {
			rows = append(rows, gaugeRow(name+", repository", rs, w))
		}
	}

	return rows
}

// gaugeRow draws the row title, its newest line, then the readings rs. A row too narrow for them all keeps the newest
// that fit, and always the newest.
func gaugeRow(title string, rs []landingRead, w int) row {
	line := rs[len(rs)-1].Line
	head := bold.Render(title) + " " + dim.Render(line)
	cells := make([]string, len(rs))
	for j, r := range rs {
		cells[j] = r.shown(line)
	}

	return row{text: head + "  " + newestFitting(cells, w-ansi.StringWidth(head)-2)}
}

// shown is the reading beside its spec, where it has one. One that crossed its line is red beside ✗. One read against
// a line other than line shows its own.
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

	if r.spec == "" {
		return value
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
