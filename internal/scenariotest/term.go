package scenariotest

import (
	"errors"
	"fmt"
	"image/color"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
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
// A color is red, yellow, green, or none for the terminal's own. color reads the cell's foreground, or with
// background its background. With !, it waits until the cell is drawn in any other color.
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

var colors = map[string]color.Color{"red": ansi.Red, "yellow": ansi.Yellow, "green": ansi.Green, "none": nil}

// terminalKey holds a script's terminal among testscript's values.
type terminalKey struct{}

// terminal is the program a script started with term, and the names of the variables it starts with.
type terminal struct {
	names []string
	term  *termtest.Term
}

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

var wheelButtons = map[string]vt.MouseButton{"up": vt.MouseWheelUp, "down": vt.MouseWheelDown}

func turnWheel(ts *testscript.TestScript, neg bool, args []string) {
	button, known := wheelButtons[strings.Join(args[:min(1, len(args))], "")]
	turns, err := strconv.Atoi(strings.Join(args[1:], ""))
	switch {
	case neg, len(args) != 2, !known, err != nil:
		ts.Fatalf("usage: wheel up|down <turns>")
	}

	term := termOf(ts)
	size := term.Size()
	for range turns {
		term.Wheel(termtest.Pos{X: size.W / 2, Y: size.H / 2}, button)
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

// colorCheck is what a color command waits for: text's first cell drawn in want. With neg, it waits for another color.
type colorCheck struct {
	background, neg bool
	name            string
	want            color.Color
	text            string
}

func parseColorCheck(neg bool, args []string) (colorCheck, bool) {
	check := colorCheck{neg: neg, background: len(args) == 3 && args[0] == "background"}
	if check.background {
		args = args[1:]
	}

	want, ok := colors[strings.Join(args[:min(1, len(args))], "")]
	if len(args) != 2 || !ok {
		return check, false
	}

	check.name, check.want, check.text = args[0], want, args[1]
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

	switch same := termtest.Same(got, c.want); {
	case same && c.neg:
		return fmt.Errorf("%q is drawn in %s", c.text, c.name)
	case !same && !c.neg:
		return fmt.Errorf("%q is drawn in %v, not %s", c.text, got, c.name)
	}

	return nil
}
