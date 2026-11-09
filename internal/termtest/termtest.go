// Package termtest runs a program in a pseudo-terminal, and reads its screen as a terminal would draw it.
//
//	term, err := termtest.Start(exec.Command("kiln", "tui"), 100, 30)
//	defer term.Close()
//	err = term.Wait(func() error { _, _, err := term.Find("build-stats"); return err })
//	x, y, err := term.Find("build-stats")
//	term.Click(x, y)
//	term.Press(vt.KeyEnter)
//
// The program's output feeds an emulator from charmbracelet/x/vt. The emulator encodes each key and mouse event in
// the modes the program asked for, such as SGR mouse reports. It also answers the program's queries about the terminal.
//
// A check reads the screen as it is, and the program draws in its own time. So after an input, wait for a fact the
// input causes, never for one already on screen. Wait polls for 10 seconds.
//
// A sandboxed process cannot open a pseudo-terminal, so Start fails inside a Claude Code session's sandbox.
package termtest

import (
	"errors"
	"fmt"
	"image/color"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// Waited is how long Wait polls before it gives up.
const Waited = 10 * time.Second

// Term is one program running in a pseudo-terminal.
type Term struct {
	emu *vt.SafeEmulator
	tty *os.File
	cmd *exec.Cmd
	// exited closes once the program has exited.
	exited chan struct{}
}

// Start runs cmd in a terminal of w columns and h rows. The terminal claims 256 colors and true color, whatever
// cmd's environment says, and drops NO_COLOR.
func Start(cmd *exec.Cmd, w, h int) (*Term, error) {
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}

	for _, name := range []string{"TERM", "COLORTERM", "NO_COLOR"} {
		env = drop(env, name)
	}

	cmd.Env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(w), Rows: uint16(h)})
	if err != nil {
		return nil, err
	}

	term := &Term{emu: vt.NewSafeEmulator(w, h), tty: tty, cmd: cmd, exited: make(chan struct{})}
	// Each copy ends with an error once the program exits, and no caller needs that error.
	go func() { _, _ = io.Copy(term.emu, tty) }()
	go func() { _, _ = io.Copy(tty, term.emu) }()
	go func() {
		_ = cmd.Wait()
		close(term.exited)
	}()

	return term, nil
}

func drop(env []string, name string) []string {
	kept := env[:0:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, name+"=") {
			kept = append(kept, kv)
		}
	}

	return kept
}

// Close stops the program, if it still runs, and closes its terminal.
func (term *Term) Close() error {
	_ = term.cmd.Process.Kill()
	<-term.exited
	return term.tty.Close()
}

// Press sends each key, as the terminal would encode it in the program's current modes. A key is a character or one
// of vt's keys, such as vt.KeyEnter. It pauses after each, as a person would. A program reads an Esc and the byte that
// follows it at once as one key, Alt and that byte.
func (term *Term) Press(codes ...rune) {
	for _, code := range codes {
		term.emu.SendKey(vt.KeyPressEvent{Code: code})
		time.Sleep(keyPause)
	}
}

// keyPause is the pause after each key Press sends.
const keyPause = 100 * time.Millisecond

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

// Size answers the terminal's columns and rows.
func (term *Term) Size() (w, h int) { return term.emu.Width(), term.emu.Height() }

// Exited reports whether the program has exited.
func (term *Term) Exited() bool {
	select {
	case <-term.exited:
		return true
	default:
		return false
	}
}

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

// Find answers the column and row of text's first cell. It fails unless the screen holds text exactly once. A check
// then never reads the wrong one of two.
func (term *Term) Find(text string) (x, y int, err error) {
	found := 0
	for row, line := range strings.Split(term.Screen(), "\n") {
		if i := strings.Index(line, text); i >= 0 && found == 0 {
			x, y = len([]rune(line[:i])), row
		}

		found += strings.Count(line, text)
	}

	if found != 1 {
		return 0, 0, fmt.Errorf("the screen holds %q %d times, not once", text, found)
	}

	return x, y, nil
}

// Foreground answers the color the cell at column x, row y is drawn in, or nil for the terminal's own.
func (term *Term) Foreground(x, y int) color.Color {
	if c := term.emu.CellAt(x, y); c != nil {
		return c.Style.Fg
	}

	return nil
}

// Background answers the color behind the cell at column x, row y, or nil for the terminal's own.
func (term *Term) Background(x, y int) color.Color {
	if c := term.emu.CellAt(x, y); c != nil {
		return c.Style.Bg
	}

	return nil
}

// Same reports whether two colors draw the same.
func Same(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == b
	}

	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}

// Wait polls holds until it answers nil, or until Waited has passed. Then it answers holds' last error, with the
// screen it last read.
func (term *Term) Wait(holds func() error) error {
	deadline := time.Now().Add(Waited)
	for {
		err := holds()
		switch {
		case err == nil:
			return nil
		case time.Now().After(deadline):
			return fmt.Errorf("%w, after %s:\n%s", err, Waited, term.Screen())
		case term.Exited():
			return errors.Join(err, fmt.Errorf("the program exited:\n%s", term.Screen()))
		}

		time.Sleep(20 * time.Millisecond)
	}
}
