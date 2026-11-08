// Package e2e_test runs the same scenarios against each prototype's binary, through a pseudo-terminal. It is the
// contract each prototype meets. An acceptance test written this way need not know which library drew the screen.
//
// The screen each prototype draws:
//
//	builds            a table, one row per build, oldest first, with a # column; the newest row starts selected
//	cost per build    a bar chart, one bar per build, numbered 1 to 8 on the line beneath the bars
//	selected          the selected build's spec, outcome, time, cost and failed calls
//
// Keys: up and down, or k and j, move the selection. Enter opens the selected build, showing "by role" and
// "by cause", or "no failed calls". Esc goes back. q quits.
// Mouse: a click on a row or a bar selects it, and a click on the selected row opens it. The wheel moves the
// selection.
package e2e_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"kilntui/termtest"
)

// programs names each prototype's directory.
var programs = []string{"bubbletea", "tview", "raw"}

var bin string

// failed holds each prototype's build output when it fails to build, so one broken prototype fails only its own runs.
var failed = map[string]string{}

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "kilntui")
	if err != nil {
		panic(err)
	}

	bin = dir
	for _, p := range programs {
		if _, err := os.Stat(filepath.Join("..", p)); err != nil {
			continue
		}

		out, err := exec.Command("go", "build", "-o", filepath.Join(dir, p), "../"+p).CombinedOutput()
		if err != nil {
			failed[p] = string(out)
		}
	}

	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// each runs scenario once for each prototype that built.
func each(t *testing.T, scenario func(t *testing.T, term *termtest.Term)) {
	for _, p := range programs {
		t.Run(p, func(t *testing.T) {
			path := filepath.Join(bin, p)
			if out, ok := failed[p]; ok {
				t.Fatalf("build failed:\n%s", out)
			}

			if _, err := os.Stat(path); err != nil {
				t.Skip("no such prototype")
			}

			t.Parallel()
			term := termtest.Start(t, 100, 30, path)
			term.WaitFor("builds", "cost per build", "status-line", "build-stats")
			scenario(t, term)
		})
	}
}

func TestNewestStartsSelected(t *testing.T) {
	each(t, func(t *testing.T, term *termtest.Term) {
		term.WaitReversed("build-stats")
		if !term.AltScreen() {
			t.Error("the program draws outside the alternate screen")
		}
	})
}

func TestKeyboardOpensABuild(t *testing.T) {
	each(t, func(t *testing.T, term *termtest.Term) {
		term.Type("kkkkk")
		term.WaitReversed("stopped")
		term.Type("\r")
		term.WaitFor("by role", "by cause", "refused: A variable in this command")
		term.Type("\x1b")
		term.WaitFor("cost per build")
	})
}

func TestClickSelectsThenOpens(t *testing.T) {
	each(t, func(t *testing.T, term *termtest.Term) {
		x, y := term.Find("gate-flakes")
		term.Click(x, y)
		term.WaitReversed("gate-flakes")
		term.Click(x, y)
		term.WaitFor("by role", "no failed calls")
	})
}

func TestWheelMovesSelection(t *testing.T) {
	each(t, func(t *testing.T, term *termtest.Term) {
		x, y := term.Find("build-stats")
		term.Wheel(x, y, false)
		term.WaitReversed("notifications")
	})
}

// axis matches the line beneath the chart's bars.
var axis = regexp.MustCompile(`1\s+2\s+3\s+4\s+5\s+6\s+7\s+8`)

func TestClickABar(t *testing.T) {
	each(t, func(t *testing.T, term *termtest.Term) {
		lines := strings.Split(term.Screen(), "\n")
		for y, line := range lines {
			loc := axis.FindStringIndex(line)
			if loc == nil {
				continue
			}

			x := len([]rune(line[:loc[0]])) + strings.Index(line[loc[0]:loc[1]], "3")
			term.Click(x, y-1)
			term.WaitReversed("stopped")

			return
		}

		t.Fatalf("no axis numbered 1 to 8:\n%s", term.Screen())
	})
}

// fits matches a screen whose first line closes a border at the last of w columns. After a resize, the text a test
// waits for is often there before the program redraws. So a test waits for this instead.
func fits(w int) *regexp.Regexp { return regexp.MustCompile(fmt.Sprintf(`\A.{%d}[┐╮╗]\n`, w-1)) }

func TestResize(t *testing.T) {
	each(t, func(t *testing.T, term *termtest.Term) {
		term.Resize(70, 20)
		term.WaitMatch(fits(70))
		term.WaitFor("builds", "cost per build")
		term.WaitReversed("build-stats")
	})
}

func TestQuitRestoresTheTerminal(t *testing.T) {
	each(t, func(t *testing.T, term *termtest.Term) {
		term.Type("q")
		term.WaitExit()
		if term.AltScreen() {
			t.Error("the program left the terminal on the alternate screen")
		}
	})
}

// TestScreens saves each view at a wide and a narrow size, as text, as ANSI and as PNG. No assertion catches a panel
// that overflows its border, so an agent reads these files. To keep them, run:
//
//	go test ./e2e -run TestScreens -artifacts -outputdir DIR
func TestScreens(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {60, 16}} {
		each(t, func(t *testing.T, term *termtest.Term) {
			term.Resize(size[0], size[1])
			term.WaitMatch(fits(size[0]))
			save(t, term, "trend")
			term.Type("kkkkk\r")
			term.WaitFor("by cause")
			save(t, term, "detail")
		})
	}
}

func save(t *testing.T, term *termtest.Term, view string) {
	f, err := os.Create(filepath.Join(t.ArtifactDir(), view+".png"))
	if err != nil {
		t.Fatal(err)
	}

	defer f.Close()
	if err := term.PNG(f); err != nil {
		t.Fatal(err)
	}

	for name, text := range map[string]string{view + ".txt": term.Screen(), view + ".ansi": term.Styled()} {
		if err := os.WriteFile(filepath.Join(t.ArtifactDir(), name), []byte(text+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
