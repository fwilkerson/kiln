package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"kilntui/ledger"
)

// sim runs the app on tcell's simulation screen, which takes injected events and keeps each cell it was drawn.
type sim struct {
	t      *testing.T
	app    *tview.Application
	screen tcell.SimulationScreen
}

func start(t *testing.T) sim {
	screen := tcell.NewSimulationScreen("UTF-8")
	// SetScreen calls the screen's Init, which sets its size to 80 by 25, so the size comes after.
	app := newApp(ledger.Builds()).SetScreen(screen)
	screen.SetSize(100, 30)
	done := make(chan struct{})
	go func() {
		_ = app.Run()
		close(done)
	}()

	t.Cleanup(func() {
		app.Stop()
		<-done
	})

	return sim{t, app, screen}
}

// row answers the text of row y, and whether its first cell is drawn in reverse video.
func (s sim) row(y int) (string, bool) {
	// The app draws on its own goroutine, so the cells are copied there, between draws.
	var cells []tcell.SimCell
	var w int
	s.app.QueueUpdate(func() {
		got, width, _ := s.screen.GetContents()
		cells, w = slices.Clone(got), width
	})

	var line strings.Builder
	for _, c := range cells[y*w : (y+1)*w] {
		line.WriteString(string(c.Runes))
	}

	_, _, attrs := cells[y*w+1].Style.Decompose()

	return line.String(), attrs&tcell.AttrReverse != 0
}

// waitSelected polls until the selected row holds text. The app draws on its own goroutine, after the event.
func (s sim) waitSelected(text string) {
	s.t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		for y := range 30 {
			if line, reversed := s.row(y); reversed && strings.Contains(line, text) {
				return
			}
		}
	}

	s.t.Fatalf("no selected row holds %q", text)
}

func (s sim) click(x, y int) {
	s.screen.InjectMouse(x, y, tcell.Button1, tcell.ModNone)
	s.screen.InjectMouse(x, y, tcell.ButtonNone, tcell.ModNone)
}

func TestKeysAndMouseMoveTheSelection(t *testing.T) {
	s := start(t)
	s.waitSelected("build-stats")
	s.screen.InjectKey(tcell.KeyRune, 'k', tcell.ModNone)
	s.waitSelected("notifications")

	// Row 2 is build 1, since row 0 is the table's border and row 1 its header.
	s.click(10, 2+5)
	s.waitSelected("gate-flakes")

	// The chart numbers each bar on the line beneath it.
	for y := range 30 {
		if line, _ := s.row(y); strings.Contains(line, "│ 1 ") {
			s.click(len([]rune(line[:strings.Index(line, "3")])), y-1)
		}
	}

	s.waitSelected("stopped")
}
