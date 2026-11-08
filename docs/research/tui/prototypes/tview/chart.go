package main

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"kilntui/ledger"
)

// chart draws one bar per build's cost, numbered beneath, with the selected build's bar in another color. Draw and
// the mouse handler share one geometry, in column, so a click lands on the bar drawn under it.
type chart struct {
	*tview.Box
	builds   []ledger.Build
	selected int
	onClick  func(i int)
}

func newChart(builds []ledger.Build) *chart {
	c := &chart{Box: tview.NewBox(), builds: builds}
	c.SetBorder(true).SetTitle("cost per build")

	return c
}

// column answers the first column of build i's bar, and the bar's width, inside the chart's inner rectangle.
func (c *chart) column(i int) (x, width int) {
	left, _, w, _ := c.GetInnerRect()
	step := max(w/len(c.builds), 2)

	return left + 1 + i*step, min(step-1, 3)
}

// Draw draws the bars above a line of their numbers.
func (c *chart) Draw(screen tcell.Screen) {
	c.DrawForSubclass(screen, c)
	_, top, _, h := c.GetInnerRect()
	peak := 0.0
	for _, b := range c.builds {
		peak = max(peak, b.Cost)
	}

	base := top + h - 2
	for i, b := range c.builds {
		x, width := c.column(i)
		style := tcell.StyleDefault.Foreground(tcell.ColorSteelBlue)
		if i == c.selected {
			style = tcell.StyleDefault.Foreground(tcell.ColorYellow)
		}

		// Every bar is at least one row, so even the cheapest build has a cell to click.
		rows := max(int(b.Cost/peak*float64(h-1)), 1)
		for y := base; y > base-rows; y-- {
			for dx := range width {
				screen.SetContent(x+dx, y, '█', nil, style)
			}
		}

		tview.Print(screen, fmt.Sprint(i+1), x, base+1, width, tview.AlignLeft, tcell.ColorWhite)
	}
}

// MouseHandler selects the build whose bar, or number, the click landed on.
func (c *chart) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool,
	tview.Primitive) {
	return c.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse,
		_ func(tview.Primitive)) (bool, tview.Primitive) {
		x, y := event.Position()
		if singleClick(action) != tview.MouseLeftClick || !c.InRect(x, y) {
			return false, nil
		}

		for i := range c.builds {
			if left, width := c.column(i); x >= left && x < left+width {
				c.onClick(i)
				return true, nil
			}
		}

		return true, nil
	})
}
