package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"kilntui/ledger"
)

// clickOn clicks the first cell of the first place the screen shows its text.
type clickOn string

// clickBar clicks the bottom cell of the bar numbered n on the chart's axis.
type clickBar string

func TestModel(t *testing.T) {
	up, enter := tea.KeyPressMsg{Code: tea.KeyUp}, tea.KeyPressMsg{Code: tea.KeyEnter}
	esc := tea.KeyPressMsg{Code: tea.KeyEscape}
	tests := []struct {
		name  string
		steps []any
		sel   int
		shows []string
	}{
		{"keys move and open", []any{up, up, up, up, up, enter}, 2, []string{"by cause", "refused: A variable"}},
		{"esc goes back", []any{enter, esc}, 7, []string{"cost per build"}},
		{"a click selects, a second opens", []any{clickOn("gate-flakes"), clickOn("gate-flakes")}, 5,
			[]string{"by role", "no failed calls"}},
		{"a click on a bar selects its build", []any{clickBar("3")}, 2, []string{"cost per build"}},
		{"the wheel moves the selection", []any{tea.MouseWheelMsg{Button: tea.MouseWheelUp}}, 6, []string{"builds"}},
		{"a small terminal keeps every panel", []any{tea.WindowSizeMsg{Width: 70, Height: 20}}, 7,
			[]string{"builds", "cost per build", "selected"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m tea.Model = model{builds: ledger.Builds(), sel: 7}
			m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			for _, step := range tt.steps {
				m, _ = m.Update(resolve(t, m, step))
			}

			screen := ansi.Strip(m.View().Content)
			if got := m.(model).sel; got != tt.sel {
				t.Errorf("selected %d, want %d", got, tt.sel)
			}

			for _, s := range tt.shows {
				if !strings.Contains(screen, s) {
					t.Errorf("screen lacks %q:\n%s", s, screen)
				}
			}
		})
	}
}

// resolve turns a click on text into a mouse message at the cell the current view draws it in.
func resolve(t *testing.T, m tea.Model, step any) tea.Msg {
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	for y, line := range lines {
		switch s := step.(type) {
		case clickOn:
			if i := strings.Index(line, string(s)); i >= 0 {
				return tea.MouseClickMsg{X: ansi.StringWidth(line[:i]), Y: y, Button: tea.MouseLeft}
			}
		case clickBar:
			if i := strings.Index(line, "1 2 3 4 5 6 7 8"); i >= 0 {
				x := ansi.StringWidth(line[:i]) + strings.Index("1 2 3 4 5 6 7 8", string(s))
				return tea.MouseClickMsg{X: x, Y: y - 1, Button: tea.MouseLeft}
			}
		default:
			return step
		}
	}

	t.Fatalf("screen lacks %v:\n%s", step, strings.Join(lines, "\n"))

	return nil
}

// TestProgram runs the model in a real Bubble Tea program, with teatest standing in for the terminal.
func TestProgram(t *testing.T) {
	tm := teatest.NewTestModel(t, model{builds: ledger.Builds(), sel: 7}, teatest.WithInitialTermSize(100, 30))
	tm.Type("kkkkk")
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool { return bytes.Contains(b, []byte("by cause")) },
		teatest.WithDuration(3*time.Second))
	tm.Type("q")
	if got := tm.FinalModel(t, teatest.WithFinalTimeout(3*time.Second)).(model).sel; got != 2 {
		t.Errorf("selected %d, want 2", got)
	}
}
