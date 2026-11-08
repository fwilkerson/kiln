// Package termtest runs a program in a pseudo-terminal and reads its screen as a terminal would draw it.
//
//	term := termtest.Start(t, 100, 30, "./kiln-tui")
//	term.WaitFor("build-stats")
//	term.Press(vt.KeyDown)
//	x, y := term.Find("amendments")
//	term.Click(x, y)
//	term.WaitFor("by cause")
//
// The program's output feeds a vt emulator from charmbracelet/x/vt. The emulator encodes each key and mouse event in
// the modes the program asked for, such as SGR mouse reports. It also answers the program's queries about the terminal.
package termtest

import (
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// wait is how long WaitFor polls before the test fails.
const wait = 5 * time.Second

// Term is one program running in a pseudo-terminal.
type Term struct {
	t   testing.TB
	emu *vt.SafeEmulator
	tty *os.File

	// exited closes once the program has exited.
	exited chan struct{}
}

// Start runs name with args in a terminal of w columns and h rows. It stops the program when the test ends.
func Start(t testing.TB, w, h int, name string, args ...string) *Term {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(w), Rows: uint16(h)})
	if err != nil {
		t.Fatal(err)
	}

	term := &Term{t: t, emu: vt.NewSafeEmulator(w, h), tty: tty, exited: make(chan struct{})}
	// Each copy ends with an error once the program exits, and no test needs that error.
	go func() { _, _ = io.Copy(term.emu, tty) }()
	go func() { _, _ = io.Copy(tty, term.emu) }()
	go func() {
		_ = cmd.Wait()
		close(term.exited)
	}()

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-term.exited
		_ = tty.Close()
	})

	return term
}

// Press sends each key, as the terminal would encode it in the program's current modes.
func (term *Term) Press(codes ...rune) {
	for _, code := range codes {
		term.emu.SendKey(vt.KeyPressEvent{Code: code})
	}
}

// Type sends text as typed keys.
func (term *Term) Type(text string) { term.emu.SendText(text) }

// Click presses and releases the left button on the cell at column x, row y, counted from 0.
func (term *Term) Click(x, y int) {
	term.emu.SendMouse(vt.MouseClick{X: x, Y: y, Button: vt.MouseLeft})
	term.emu.SendMouse(vt.MouseRelease{X: x, Y: y, Button: vt.MouseLeft})
}

// Wheel turns the wheel once over the cell at column x, row y, down when down is true.
func (term *Term) Wheel(x, y int, down bool) {
	button := vt.MouseWheelUp
	if down {
		button = vt.MouseWheelDown
	}

	term.emu.SendMouse(vt.MouseWheel{X: x, Y: y, Button: button})
}

// Resize changes the terminal's size, and tells the program as a real terminal does.
func (term *Term) Resize(w, h int) {
	term.emu.Resize(w, h)
	if err := pty.Setsize(term.tty, &pty.Winsize{Cols: uint16(w), Rows: uint16(h)}); err != nil {
		term.t.Fatal(err)
	}
}

// WaitExit waits for the program to exit, or fails the test.
func (term *Term) WaitExit() {
	term.t.Helper()
	select {
	case <-term.exited:
	case <-time.After(wait):
		term.t.Fatalf("still running after %s:\n%s", wait, term.Screen())
	}
}

// AltScreen reports whether the program has switched to the terminal's alternate screen, and not back.
func (term *Term) AltScreen() bool { return term.emu.IsAltScreen() }

// Screen answers the text on screen, one line per row, with trailing spaces dropped.
func (term *Term) Screen() string {
	rows := make([]string, term.emu.Height())
	for y := range rows {
		var line strings.Builder
		for x := 0; x < term.emu.Width(); x++ {
			if c := term.emu.CellAt(x, y); c != nil && c.Width > 0 {
				line.WriteString(c.Content)
			} else if c == nil || c.Content == "" {
				line.WriteByte(' ')
			}
		}

		rows[y] = strings.TrimRight(line.String(), " ")
	}

	return strings.Join(rows, "\n")
}

// Styled answers the screen with its colors and attributes, as ANSI escapes a terminal would draw.
func (term *Term) Styled() string { return term.emu.Render() }

// Inverse reports whether the cell at column x, row y is drawn in reverse video now. Use it only on a settled screen.
func (term *Term) Inverse(x, y int) bool {
	c := term.emu.CellAt(x, y)
	return c != nil && c.Style.Attrs&uv.AttrReverse != 0
}

// WaitReversed polls until text's first cell is drawn in reverse video, as a selected row often is. It fails the test
// with the screen it last saw.
func (term *Term) WaitReversed(text string) {
	term.t.Helper()
	deadline := time.Now().Add(wait)
	for {
		if x, y, ok := term.find(text); ok && term.Inverse(x, y) {
			return
		}

		if time.Now().After(deadline) {
			term.t.Fatalf("%q not drawn reversed after %s:\n%s", text, wait, term.Screen())
		}

		time.Sleep(20 * time.Millisecond)
	}
}

// Find answers the column and row of text's first cell, or fails the test when the screen doesn't hold it.
func (term *Term) Find(text string) (x, y int) {
	term.t.Helper()
	x, y, ok := term.find(text)
	if !ok {
		term.t.Fatalf("screen lacks %q:\n%s", text, term.Screen())
	}

	return x, y
}

func (term *Term) find(text string) (x, y int, ok bool) {
	for y, line := range strings.Split(term.Screen(), "\n") {
		if i := strings.Index(line, text); i >= 0 {
			return len([]rune(line[:i])), y, true
		}
	}

	return 0, 0, false
}

// WaitMatch polls the screen until pattern matches it, or fails the test with the screen it last saw.
func (term *Term) WaitMatch(pattern *regexp.Regexp) {
	term.t.Helper()
	deadline := time.Now().Add(wait)
	for !pattern.MatchString(term.Screen()) {
		if time.Now().After(deadline) {
			term.t.Fatalf("screen doesn't match %s after %s:\n%s", pattern, wait, term.Screen())
		}

		time.Sleep(20 * time.Millisecond)
	}
}

// WaitFor polls the screen until it holds each of texts, or fails the test with the screen it last saw.
func (term *Term) WaitFor(texts ...string) {
	term.t.Helper()
	deadline := time.Now().Add(wait)
	for {
		screen := term.Screen()
		if holdsAll(screen, texts) {
			return
		}

		if time.Now().After(deadline) {
			term.t.Fatalf("screen lacks %q after %s:\n%s", texts, wait, screen)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

func holdsAll(screen string, texts []string) bool {
	for _, text := range texts {
		if !strings.Contains(screen, text) {
			return false
		}
	}

	return true
}
