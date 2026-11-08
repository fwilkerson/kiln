// Command bubbletea draws the ledger's builds with Bubble Tea v2 and Lip Gloss v2.
//
//	┌ builds ─────────────────────────┐
//	│ # started    spec      outcome  │  up, down, k, j, the wheel: move the selection
//	│ 8 Sat Nov 07 build-stats …      │  a click: select a row or a bar; on the selected row, open it
//	└─────────────────────────────────┘  enter: open, esc: back, q: quit
//	┌ cost per build ─┐┌ selected ──────┐
//	│ █ █ █           ││ build-stats    │
//	│ 1 2 3           ││ …              │
//	└─────────────────┘└────────────────┘
//
// Each table row and each bar is a Lip Gloss layer with an ID. A click asks the layers which one it hit. The cells
// drawn and the cells clicked then come from one layout.
package main

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"kilntui/ledger"
)

func main() {
	if _, err := tea.NewProgram(model{builds: ledger.Builds(), sel: len(ledger.Builds()) - 1}).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// model is the whole screen's state. Bubble Tea passes it by value, so Update answers the changed copy.
type model struct {
	builds []ledger.Build
	sel    int
	open   bool
	w, h   int
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			m.move(-1)
		case "down", "j":
			m.move(1)
		case "enter":
			m.open = true
		case "esc":
			m.open = false
		}
	case tea.MouseWheelMsg:
		if msg.Button == tea.MouseWheelUp {
			m.move(-1)
		} else if msg.Button == tea.MouseWheelDown {
			m.move(1)
		}
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft && !m.open {
			m.click(m.layout().Hit(msg.X, msg.Y).ID())
		}
	}

	return m, nil
}

func (m *model) move(by int) { m.sel = max(0, min(len(m.builds)-1, m.sel+by)) }

// click selects the row or bar whose layer ID is id, and opens a row that was already selected.
func (m *model) click(id string) {
	kind, n, _ := strings.Cut(id, ":")
	i, err := strconv.Atoi(n)
	if err != nil {
		return
	}

	m.open = kind == "row" && i == m.sel
	m.sel = i
}

func (m model) View() tea.View {
	v := tea.NewView(m.detail())
	if !m.open {
		v.SetContent(m.layout().Render())
	}

	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion

	return v
}

var (
	panel    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("8"))
	title    = lipgloss.NewStyle().Bold(true)
	faint    = lipgloss.NewStyle().Faint(true)
	reversed = lipgloss.NewStyle().Reverse(true)
	bar      = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	picked   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
)

// columns are the table's header and each column's width.
var columns = []struct {
	name  string
	width int
}{{"#", 2}, {"started", 10}, {"spec", 14}, {"outcome", 22}, {"size", 4}, {"time", 5}, {"cost", 7}, {"failed", 6}}

// layout places the three panels in a terminal of m.w by m.h cells. Each row and each bar is a layer.
func (m model) layout() *lipgloss.Compositor {
	tableH := len(m.builds) + 4
	lowH := max(m.h-tableH, 6)
	chartW := max(m.w*3/5, 2*len(m.builds)+4)

	table := lipgloss.NewLayer(box(m.w, tableH, "builds", faint.Render(row(header()))))
	for i, b := range m.builds {
		// Lip Gloss wraps text wider than Width, and a wrapped row would cover the next.
		text := lipgloss.NewStyle().Width(m.w - 2).Render(ansi.Truncate(row(cells(i, b)), m.w-2, ""))
		if i == m.sel {
			text = reversed.Render(text)
		}

		table.AddLayers(lipgloss.NewLayer(text).ID(fmt.Sprintf("row:%d", i)).X(1).Y(3 + i).Z(1))
	}

	chart := lipgloss.NewLayer(box(chartW, lowH, "cost per build", "")).Y(tableH)
	most := slices.MaxFunc(m.builds, func(a, b ledger.Build) int { return cmp.Compare(a.Cost, b.Cost) }).Cost
	barsH := lowH - 4
	axis := make([]string, len(m.builds))
	for i, b := range m.builds {
		height := max(1, int(b.Cost/most*float64(barsH)))
		// A reversed block would draw as a blank, so the selected bar takes a color instead.
		style := bar
		if i == m.sel {
			style = picked
		}

		column := strings.Repeat("█\n", height-1) + "█"
		chart.AddLayers(lipgloss.NewLayer(style.Render(column)).ID(fmt.Sprintf("bar:%d", i)).
			X(2 + 2*i).Y(2 + barsH - height).Z(1))
		axis[i] = strconv.Itoa(i + 1)
	}

	chart.AddLayers(lipgloss.NewLayer(strings.Join(axis, " ")).X(2).Y(lowH - 2).Z(1))

	b := m.builds[m.sel]
	summary := fmt.Sprintf("%s\n%s\ntime %s · waiting %s · gates %s\n$%.2f · %d turns · %d failed",
		b.Spec, b.Outcome, b.Time, b.Waiting, b.Gates, b.Cost, b.Turns, b.Failed())
	side := lipgloss.NewLayer(box(m.w-chartW, lowH, "selected", summary)).X(chartW).Y(tableH)

	return lipgloss.NewCompositor(table, chart, side)
}

// box draws a bordered panel of w by h cells, with a bold heading over body. It cuts each line of body that would wrap.
func box(w, h int, heading, body string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, w-2, "…")
	}

	return panel.Width(w).Height(h).MaxHeight(h).Render(title.Render(heading) + "\n" + strings.Join(lines, "\n"))
}

func header() []string {
	names := make([]string, len(columns))
	for i, c := range columns {
		names[i] = c.name
	}

	return names
}

func cells(i int, b ledger.Build) []string {
	return []string{strconv.Itoa(i + 1), b.Started.Format("Mon Jan 02"), b.Spec, b.Outcome, strconv.Itoa(b.Size),
		fmt.Sprintf("%.0fm", b.Time.Minutes()), fmt.Sprintf("$%.2f", b.Cost), strconv.Itoa(b.Failed())}
}

// row pads each cell to its column's width.
func row(cells []string) string {
	var s strings.Builder
	for i, c := range cells {
		fmt.Fprintf(&s, "%-*s ", columns[i].width, c)
	}

	return s.String()
}

// detail draws the selected build by role and by cause.
func (m model) detail() string {
	b := m.builds[m.sel]
	var s strings.Builder
	fmt.Fprintf(&s, "%s, %s   size %d · %s · $%.2f · %d turns   esc: back\n\n", b.Spec, b.Started.Format("Mon Jan 02"),
		b.Size, b.Time, b.Cost, b.Turns)
	s.WriteString(title.Render("by role") + "\n")
	for _, r := range b.Roles {
		fmt.Fprintf(&s, "  %-6s %d run · %3d turns · $%5.2f · %s · context %dk → %dk → %dk\n", r.Name, r.Runs, r.Turns,
			r.Cost, r.Time, r.Context[0]/1000, r.Context[1]/1000, r.Context[2]/1000)
	}

	s.WriteString("\n" + title.Render("by cause") + "\n")
	if len(b.Causes) == 0 {
		s.WriteString("  no failed calls\n")
	}

	for _, c := range b.Causes {
		fmt.Fprintf(&s, "  %3d  %s\n       %s\n", c.Count, c.Text, faint.Render(c.Input))
	}

	return s.String()
}
