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
// gauge's are in the order kiln wrote them. names lists the gauges, sorted. Of a monitor's, it keeps the newest keep,
// and always the newest.
func readings(specs []Spec, keep int) (names []string, landings, repository gaugeReads) {
	landings, repository = gaugeReads{}, gaugeReads{}
	for _, s := range specs {
		for _, b := range s.Builds {
			for _, r := range b.Readings {
				landings.add(r, b.Spec)
			}
		}

		if s.Monitor != nil {
			rs := s.Monitor.Readings
			for _, r := range rs[max(0, len(rs)-max(keep, 1)):] {
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
func (m model) gaugeRows(w int) []row {
	// Each reading takes at least one cell, so w+1 readings still leave an older one for the … to mark.
	names, landings, repository := readings(m.specs, w+1)
	t := m.theme
	var rows []row
	for _, name := range names {
		if rs := landings[name]; len(rs) > 0 {
			rows = append(rows, t.gaugeRow(name, rs, w))
		}

		if rs := repository[name]; len(rs) > 0 {
			rows = append(rows, t.gaugeRow(name+", repository", rs, w))
		}
	}

	return rows
}

// gaugeRow draws the row title, its newest line, then the readings rs. A row too narrow for them all keeps the newest
// that fit, and always the newest.
func (t theme) gaugeRow(title string, rs []landingRead, w int) row {
	line := rs[len(rs)-1].Line
	head := t.bold.Render(title) + " " + t.dim.Render(line)
	return row{text: head + "  " + t.newestFitting(rs, line, w-ansi.StringWidth(head)-2)}
}

// shown is the reading beside its spec, where it has one. One that crossed its line is red beside ✗. One read against
// a line other than line shows its own.
func (r landingRead) shown(t theme, line string) string {
	value := "?"
	if r.Value != nil {
		value = strconv.FormatFloat(*r.Value, 'f', -1, 64)
	}

	if r.Crossed {
		value = t.failed.Render("✗ " + value)
	}

	if r.Line != line {
		value += t.dim.Render(" (" + r.Line + ")")
	}

	if r.spec == "" {
		return value
	}

	return r.spec + " " + value
}

// newestFitting joins the newest of rs, as shown against line, that fit in w cells, oldest first. A … marks that
// older ones are left out. Where not even the newest fits, it is the newest alone, which the panel then cuts.
func (t theme) newestFitting(rs []landingRead, line string, w int) string {
	sep, more := t.dim.Render(" · "), t.dim.Render("… ")
	newest := rs[len(rs)-1].shown(t, line)
	cells, width := []string{newest}, ansi.StringWidth(newest)
	// From the newest backward, the walk stops at the width, so a reading too old to fit costs nothing.
	for i := len(rs) - 2; i >= 0; i-- {
		cell := rs[i].shown(t, line)
		next, room := width+ansi.StringWidth(sep)+ansi.StringWidth(cell), w
		if i > 0 {
			room -= ansi.StringWidth(more)
		}

		if next > room {
			break
		}

		cells, width = append(cells, cell), next
	}

	slices.Reverse(cells)
	joined := strings.Join(cells, sep)
	switch {
	case len(cells) == len(rs):
		return joined
	case ansi.StringWidth(more)+width <= w:
		return more + joined
	default:
		return newest
	}
}
