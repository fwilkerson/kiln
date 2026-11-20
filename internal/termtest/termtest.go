// Package termtest runs a program in a pseudo-terminal, and reads its screen as a terminal would draw it.
//
//	term, err := termtest.Start(exec.Command("kiln", "tui"), termtest.Size{W: 100, H: 30})
//	defer term.Close()
//	err = term.Wait(ctx, func() error { _, err := term.Find("build-stats"); return err })
//	at, err := term.Find("build-stats")
//	term.Click(at)
//	term.Press(vt.KeyEnter)
//
// The program's output feeds an emulator from charmbracelet/x/vt. The emulator encodes each key and mouse event in
// the modes the program asked for, such as SGR mouse reports. It also answers the program's queries about the terminal.
//
// A check reads the screen as it is, and the program draws in its own time. So after an input, wait for a fact the
// input causes, never for one already on screen. Wait checks again each time the program draws. Wait stops when its
// context ends, or when the program has exited and the emulator has drawn its last output. So a loaded machine draws
// late, never wrong.
//
// A sandboxed process cannot open a pseudo-terminal, so Start fails inside a Claude Code session's sandbox. Its
// error is then syscall.EPERM.
package termtest

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// Term is one program running in a pseudo-terminal.
type Term struct {
	emu *vt.SafeEmulator
	tty *os.File
	cmd *exec.Cmd
	// waited closes once the program has exited.
	waited chan struct{}
	// exited closes once the program has exited, and the emulator has drawn all that reached the terminal.
	exited chan struct{}
	// drawn holds a token once the program has drawn since Wait last looked.
	drawn chan struct{}
}

// Size is a terminal's size: W columns and H rows.
type Size struct{ W, H int }

// Pos is a cell's place on screen: column X and row Y, each counted from 0.
type Pos struct{ X, Y int }

// Start runs cmd in a terminal of the given size. The terminal claims 256 colors and true color, whatever cmd's
// environment says: it sets TERM=xterm-256color and COLORTERM=truecolor, and drops NO_COLOR.
func Start(cmd *exec.Cmd, size Size) (*Term, error) {
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}

	for _, name := range []string{"TERM", "COLORTERM", "NO_COLOR"} {
		env = drop(env, name)
	}

	cmd.Env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(size.W), Rows: uint16(size.H)})
	if err != nil {
		return nil, err
	}

	term := &Term{emu: vt.NewSafeEmulator(size.W, size.H), tty: tty, cmd: cmd, waited: make(chan struct{}),
		exited: make(chan struct{}), drawn: make(chan struct{}, 1)}
	// The copy from the terminal ends with an error once no process holds the terminal. No caller needs that error.
	drained := make(chan struct{})
	go func() {
		_, _ = io.Copy(drawing{term}, tty)
		close(drained)
	}()

	go func() { _, _ = io.Copy(tty, term.emu) }()
	go func() {
		_ = cmd.Wait()
		close(term.waited)
		<-drained
		close(term.exited)
	}()

	return term, nil
}

// drawing feeds what the program prints to the emulator, and tells a Wait that the screen changed.
type drawing struct{ term *Term }

func (d drawing) Write(p []byte) (int, error) {
	n, err := d.term.emu.Write(p)
	select {
	case d.term.drawn <- struct{}{}:
	default:
	}

	return n, err
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
	<-term.waited
	return term.tty.Close()
}

// Press sends each key, as the terminal would encode it in the program's current modes. A key is a character or one
// of vt's keys, such as vt.KeyEnter. Esc goes as CSI 27 u. Ghostty sends it so once a program asks to tell keys
// apart, as Bubble Tea does. A program reads a bare Esc as a key only once no byte follows it within a timeout. On
// a loaded machine, no pause would keep the next key out of that timeout.
func (term *Term) Press(codes ...rune) {
	for _, code := range codes {
		if code == vt.KeyEscape {
			term.emu.SendText(escapeKey)
			continue
		}

		term.emu.SendKey(vt.KeyPressEvent{Code: code})
	}
}

// escapeKey is Esc in the kitty keyboard protocol, which Bubble Tea reads whatever modes it asked for.
const escapeKey = "\x1b[27u"

// Type sends text as typed keys.
func (term *Term) Type(text string) { term.emu.SendText(text) }

// Click presses and releases the left button on the cell at.
func (term *Term) Click(at Pos) {
	term.emu.SendMouse(vt.MouseClick{X: at.X, Y: at.Y, Button: vt.MouseLeft})
	term.emu.SendMouse(vt.MouseRelease{X: at.X, Y: at.Y, Button: vt.MouseLeft})
}

// WheelUp turns the wheel up once over the cell at.
func (term *Term) WheelUp(at Pos) {
	term.emu.SendMouse(vt.MouseWheel{X: at.X, Y: at.Y, Button: vt.MouseWheelUp})
}

// WheelDown turns the wheel down once over the cell at.
func (term *Term) WheelDown(at Pos) {
	term.emu.SendMouse(vt.MouseWheel{X: at.X, Y: at.Y, Button: vt.MouseWheelDown})
}

// Size answers the terminal's size.
func (term *Term) Size() Size { return Size{W: term.emu.Width(), H: term.emu.Height()} }

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

// Find answers where text's first cell is. It fails unless the screen holds text exactly once. A check then never
// reads the wrong one of two.
func (term *Term) Find(text string) (Pos, error) {
	var at Pos
	found := 0
	for row, line := range strings.Split(term.Screen(), "\n") {
		if i := strings.Index(line, text); i >= 0 && found == 0 {
			at = Pos{X: len([]rune(line[:i])), Y: row}
		}

		found += strings.Count(line, text)
	}

	if found != 1 {
		return Pos{}, fmt.Errorf("the screen holds %q %d times, not once", text, found)
	}

	return at, nil
}

// Foreground answers the color the cell at is drawn in, or nil for the terminal's own.
func (term *Term) Foreground(at Pos) color.Color {
	if c := term.emu.CellAt(at.X, at.Y); c != nil {
		return c.Style.Fg
	}

	return nil
}

// Background answers the color behind the cell at, or nil for the terminal's own.
func (term *Term) Background(at Pos) color.Color {
	if c := term.emu.CellAt(at.X, at.Y); c != nil {
		return c.Style.Bg
	}

	return nil
}

// Wait checks holds now and after each time the program draws, until holds answers nil. Wait gives up when ctx is
// done, or the program has exited and the emulator has drawn its last output. Then Wait answers holds' last
// error, with the screen it last read.
func (term *Term) Wait(ctx context.Context, holds func() error) error {
	for {
		err := holds()
		if err == nil {
			return nil
		}

		select {
		case <-term.drawn:
		case <-term.exited:
			if err := holds(); err != nil {
				return errors.Join(err, fmt.Errorf("the program exited:\n%s", term.Screen()))
			}

			return nil
		case <-ctx.Done():
			return fmt.Errorf("%w, when %w:\n%s", err, context.Cause(ctx), term.Screen())
		}
	}
}
