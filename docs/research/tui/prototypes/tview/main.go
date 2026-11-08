// Command tview draws the fake ledger with rivo/tview. It shows a table of builds, a bar chart of their cost, and the
// selected build's summary. Enter, or a click on the selected row, opens the build by role and by cause.
//
//	┌builds──────────────────────────────────────┐
//	│#  started  spec         outcome  size  ... │
//	│8  Nov 07   build-stats  waiting  6     ... │  ← selected, in reverse video
//	└────────────────────────────────────────────┘
//	┌cost per build────────────┐┌selected────────┐
//	│        █                 ││build-stats     │
//	│  █  █  █     █     █  █  ││...             │
//	│1  2  3  4  5  6  7  8    ││                │
//	└──────────────────────────┘└────────────────┘
package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"kilntui/ledger"
)

func main() {
	if err := newApp(ledger.Builds()).Run(); err != nil {
		panic(err)
	}
}

// newApp answers the application, ready to run on a terminal or on a screen a test sets.
func newApp(builds []ledger.Build) *tview.Application {
	app := tview.NewApplication().EnableMouse(true)
	pages := tview.NewPages()
	table := buildTable(builds)
	bars := newChart(builds)
	summary := tview.NewTextView().SetDynamicColors(false)
	summary.SetBorder(true).SetTitle("selected")
	detail := tview.NewTextView()
	detail.SetBorder(true)

	// The table's row 0 is its header, so build i sits on row i+1.
	table.SetSelectionChangedFunc(func(row, _ int) {
		bars.selected = row - 1
		summary.SetText(summarize(builds[row-1]))
	})

	open := func(row int) {
		b := builds[row-1]
		detail.SetText(describe(b)).SetTitle(b.Spec + ", " + b.Outcome)
		pages.SwitchToPage("detail")
	}

	table.SetSelectedFunc(func(row, _ int) { open(row) })
	bars.onClick = func(i int) { table.Select(i+1, 0) }
	detail.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			pages.SwitchToPage("trend")
			app.SetFocus(table)
		}
	})

	// tview's table selects on a click and scrolls on the wheel. Here a click on the selected row opens it instead.
	// The wheel moves the selection, so a mouse alone can drill down.
	table.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction,
		*tcell.EventMouse) {
		action = singleClick(action)
		selected, _ := table.GetSelection()
		switch row, _ := table.CellAt(event.Position()); {
		case action == tview.MouseLeftClick && row == selected:
			open(row)
		case action == tview.MouseScrollUp:
			table.Select(max(selected-1, 1), 0)
		case action == tview.MouseScrollDown:
			table.Select(min(selected+1, len(builds)), 0)
		default:
			return action, event
		}

		return tview.MouseConsumed, nil
	})

	bottom := tview.NewFlex().AddItem(bars, 0, 3, false).AddItem(summary, 0, 2, false)
	trend := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(table, len(builds)+3, 0, true).
		AddItem(bottom, 0, 1, false)
	pages.AddPage("trend", trend, true, true).AddPage("detail", detail, true, false)
	table.Select(len(builds), 0)

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune && event.Rune() == 'q' {
			app.Stop()
			return nil
		}

		return event
	})

	return app.SetRoot(pages, true)
}

// buildTable answers the builds as a table with a fixed header row, one row per build.
func buildTable(builds []ledger.Build) *tview.Table {
	table := tview.NewTable().SetSelectable(true, false).SetFixed(1, 0).
		SetSelectedStyle(tcell.StyleDefault.Reverse(true))
	table.SetBorder(true).SetTitle("builds")
	for col, head := range []string{"#", "started", "spec", "outcome", "size", "time", "cost", "failed"} {
		table.SetCell(0, col, tview.NewTableCell(head).SetSelectable(false).SetAttributes(tcell.AttrBold))
	}

	for i, b := range builds {
		row := []string{fmt.Sprint(i + 1), b.Started.Format("Jan 02"), b.Spec, b.Outcome, fmt.Sprint(b.Size),
			minutes(b.Time), fmt.Sprintf("$%.2f", b.Cost), fmt.Sprint(b.Failed())}
		for col, text := range row {
			// Expansion 1 on each cell spreads the selected row's reverse video across the table's width.
			table.SetCell(i+1, col, tview.NewTableCell(text).SetExpansion(1))
		}
	}

	return table
}

// summarize answers the lines of the selected panel.
func summarize(b ledger.Build) string {
	return fmt.Sprintf("%s, %s\nstarted  %s\ntime     %s · %s waiting · %s in gates\ncost     $%.2f · "+
		"$%.2f per test\nturns    %d\nfailed   %d calls",
		b.Spec, b.Outcome, b.Started.Format("Mon Jan 02 15:04"), minutes(b.Time), b.Waiting, b.Gates, b.Cost,
		b.Cost/float64(b.Size), b.Turns, b.Failed())
}

// describe answers the detail view: each role, then each cause of a failed call.
func describe(b ledger.Build) string {
	var s strings.Builder
	fmt.Fprintf(&s, "%s\n\nby role\n", summarize(b))
	for _, r := range b.Roles {
		fmt.Fprintf(&s, "  %-6s %d run · %3d turns · $%5.2f · %s · context %dk → %dk → %dk\n", r.Name, r.Runs,
			r.Turns, r.Cost, minutes(r.Time), r.Context[0]/1000, r.Context[1]/1000, r.Context[2]/1000)
	}

	if len(b.Causes) == 0 {
		s.WriteString("\nno failed calls\n")
		return s.String()
	}

	s.WriteString("\nby cause\n")
	for _, c := range b.Causes {
		fmt.Fprintf(&s, "  %3d  %s\n       %s\n", c.Count, c.Text, c.Input)
	}

	return s.String()
}

// singleClick answers a double click as a click. tview reports a quick second click only as a double click, which
// would otherwise be lost.
func singleClick(action tview.MouseAction) tview.MouseAction {
	if action == tview.MouseLeftDoubleClick {
		return tview.MouseLeftClick
	}

	return action
}

func minutes(d time.Duration) string { return fmt.Sprintf("%d min", int(d.Minutes())) }
