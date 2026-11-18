package scenariotest

import (
	"errors"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/charmbracelet/x/vt"
	"github.com/rogpeppe/go-internal/testscript"

	"github.com/fwilkerson/kiln/internal/proc"
	"github.com/fwilkerson/kiln/internal/termtest"
)

// A script drives a full-screen program with these commands. Each that waits gives up after [termtest.Waited].
//
//	term <cols> <rows> <program> [args...]  start the program in a terminal of that size, one per script
//	keys <key or text>...                   press each named key, or type each other word as text
//	click <text>                            wait until text is on screen once, then click its first cell
//	wheel up|down <turns>                   turn the wheel over the middle of the screen
//	screen <regexp>                         wait until the screen matches; with !, until it does not
//	color [background] <color> <text>       wait until text is on screen once, its first cell in that color
//
// A named key is one of keyNames. A pattern matches the screen's text, line by line, with (?m) set. A screen check
// with ! passes at once when its text is not yet drawn, so wait for what the screen does hold first.
//
// A color is a hue, red, yellow or green, which any shade of it matches. Or it is none, for the terminal's own. color
// reads the cell's foreground, or with background its background. With !, it waits until the cell is drawn in any
// other color.
var terminalCommands = map[string]func(*testscript.TestScript, bool, []string){
	"term":   startTerm,
	"keys":   pressKeys,
	"click":  clickText,
	"wheel":  turnWheel,
	"screen": waitScreen,
	"color":  waitColor,
}

var keyNames = map[string]rune{
	"enter": vt.KeyEnter, "esc": vt.KeyEscape, "tab": vt.KeyTab, "up": vt.KeyUp, "down": vt.KeyDown,
	"home": vt.KeyHome, "end": vt.KeyEnd, "pgup": vt.KeyPgUp, "pgdown": vt.KeyPgDown, "backspace": vt.KeyBackspace,
	"space": vt.KeySpace,
}

// colors match the colors a script names. A hue matches each of its shades, so a check holds on any theme, in 256
// colors too.
var colors = map[string]func(color.Color) bool{
	"red":    hued(-25, 12),
	"yellow": hued(35, 70),
	"green":  hued(80, 160),
	"none":   func(c color.Color) bool { return c == nil },
}

// hued matches a color whose hue lies from lo to hi degrees, red at 0, from -180 to 180. A gray has no hue, and
// matches no span.
func hued(lo, hi float64) func(color.Color) bool {
	return func(c color.Color) bool {
		if c == nil {
			return false
		}

		r, g, b, _ := c.RGBA()
		top, bottom := max(r, g, b), min(r, g, b)
		if top == bottom {
			return false
		}

		span := float64(top - bottom)
		var h float64
		switch top {
		case r:
			h = (float64(g) - float64(b)) / span
		case g:
			h = 2 + (float64(b)-float64(r))/span
		default:
			h = 4 + (float64(r)-float64(g))/span
		}

		if h *= 60; h > 180 {
			h -= 360
		}

		return h >= lo && h <= hi
	}
}

// terminalKey holds a script's terminal among testscript's values.
type terminalKey struct{}

// terminal is the program a script started with term, and the names of the variables it starts with.
type terminal struct {
	names []string
	term  *termtest.Term
	// skip ends the script as skipped, for the reason given.
	skip func(reason ...any)
}

// sandboxEnv is the variable Claude Code's sandbox sets for each command it runs.
const sandboxEnv = "SANDBOX_RUNTIME"

func startTerm(ts *testscript.TestScript, neg bool, args []string) {
	t := ts.Value(terminalKey{}).(*terminal)
	switch {
	case neg, len(args) < 3, t.term != nil:
		ts.Fatalf("usage: term <cols> <rows> <program> [args...], once per script")
	}

	w, errW := strconv.Atoi(args[0])
	h, errH := strconv.Atoi(args[1])
	if errors.Join(errW, errH) != nil {
		ts.Fatalf("term: %q by %q is not a size", args[0], args[1])
	}

	cmd := exec.Command(lookPath(ts, args[2]), args[3:]...)
	cmd.Dir = ts.MkAbs(".")
	for _, name := range t.names {
		cmd.Env = append(cmd.Env, name+"="+ts.Getenv(name))
	}

	term, err := termtest.Start(cmd, termtest.Size{W: w, H: h})
	// A failure here would fail the package in every session, and so hide what else fails there. Outside a sandbox
	// the same error is a fault, so kiln's gate never skips.
	if errors.Is(err, syscall.EPERM) && os.Getenv(sandboxEnv) != "" {
		t.skip("term: a sandbox opens no pseudo-terminal, so this script runs only outside one, as in kiln's gate")
	}

	ts.Check(err)
	t.term = term
	ts.Defer(func() { _ = term.Close() })
}

// lookPath finds program on the script's PATH.
func lookPath(ts *testscript.TestScript, program string) string {
	for _, dir := range filepath.SplitList(ts.Getenv("PATH")) {
		if p := filepath.Join(dir, program); proc.Executable(p) {
			return p
		}
	}

	ts.Fatalf("term: no %s on the script's PATH", program)
	return ""
}

func termOf(ts *testscript.TestScript) *termtest.Term {
	t := ts.Value(terminalKey{}).(*terminal)
	if t.term == nil {
		ts.Fatalf("no terminal: start one with term first")
	}

	return t.term
}

func pressKeys(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) == 0 {
		ts.Fatalf("usage: keys <key or text>...")
	}

	term := termOf(ts)
	for _, arg := range args {
		if code, ok := keyNames[arg]; ok {
			term.Press(code)
		} else {
			term.Type(arg)
		}
	}
}

func clickText(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 1 {
		ts.Fatalf("usage: click <text>")
	}

	term := termOf(ts)
	var at termtest.Pos
	ts.Check(term.Wait(func() (err error) {
		at, err = term.Find(args[0])
		return err
	}))

	term.Click(at)
}

var wheelTurns = map[string]func(*termtest.Term, termtest.Pos){
	"up":   (*termtest.Term).WheelUp,
	"down": (*termtest.Term).WheelDown,
}

func turnWheel(ts *testscript.TestScript, neg bool, args []string) {
	turn, known := wheelTurns[strings.Join(args[:min(1, len(args))], "")]
	turns, err := strconv.Atoi(strings.Join(args[1:], ""))
	switch {
	case neg, len(args) != 2, !known, err != nil:
		ts.Fatalf("usage: wheel up|down <turns>")
	}

	term := termOf(ts)
	size := term.Size()
	for range turns {
		turn(term, termtest.Pos{X: size.W / 2, Y: size.H / 2})
	}
}

func waitScreen(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) != 1 {
		ts.Fatalf("usage: screen <regexp>")
	}

	pattern, err := regexp.Compile("(?m)" + args[0])
	ts.Check(err)
	term := termOf(ts)
	ts.Check(term.Wait(func() error {
		switch matched := pattern.MatchString(term.Screen()); {
		case matched && neg:
			return fmt.Errorf("the screen matches %s", args[0])
		case !matched && !neg:
			return fmt.Errorf("the screen does not match %s", args[0])
		}

		return nil
	}))
}

func waitColor(ts *testscript.TestScript, neg bool, args []string) {
	check, ok := parseColorCheck(neg, args)
	if !ok {
		ts.Fatalf("usage: color [background] red|yellow|green|none <text>")
	}

	term := termOf(ts)
	ts.Check(term.Wait(func() error { return check.holds(term) }))
}

// colorCheck is what a color command waits for: text's first cell drawn in a color that drawn matches. With neg, it
// waits for another color.
type colorCheck struct {
	background, neg bool
	name            string
	drawn           func(color.Color) bool
	text            string
}

func parseColorCheck(neg bool, args []string) (colorCheck, bool) {
	check := colorCheck{neg: neg, background: len(args) == 3 && args[0] == "background"}
	if check.background {
		args = args[1:]
	}

	drawn, ok := colors[strings.Join(args[:min(1, len(args))], "")]
	if len(args) != 2 || !ok {
		return check, false
	}

	check.name, check.drawn, check.text = args[0], drawn, args[1]
	return check, true
}

func (c colorCheck) holds(term *termtest.Term) error {
	at, err := term.Find(c.text)
	if err != nil {
		return err
	}

	got := term.Foreground(at)
	if c.background {
		got = term.Background(at)
	}

	switch same := c.drawn(got); {
	case same && c.neg:
		return fmt.Errorf("%q is drawn in %s", c.text, c.name)
	case !same && !c.neg:
		return fmt.Errorf("%q is drawn in %v, not %s", c.text, got, c.name)
	}

	return nil
}
