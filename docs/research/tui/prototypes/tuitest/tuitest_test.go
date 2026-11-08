// Package tuitest_test runs e2e's scenarios through Microsoft's tui-test on its Ghostty backend. TestSameScreen also
// compares Ghostty's screen with the one termtest's x/vt emulator draws, for the same inputs.
package tuitest_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tt "github.com/microsoft/tui-test/bindings/go"

	"kilntui/termtest"
)

var programs = []string{"bubbletea", "tview", "raw"}

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "kilntui")
	if err != nil {
		panic(err)
	}

	bin = dir
	for _, p := range programs {
		if out, err := exec.Command("go", "build", "-o", filepath.Join(dir, p), "../"+p).CombinedOutput(); err != nil {
			panic(p + ": " + string(out))
		}
	}

	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// start runs program p in a Ghostty terminal of w by h cells, and waits for its first screen.
func start(t *testing.T, p string, w, h uint16) *tt.Client {
	t.Helper()
	c, err := tt.Ephemeral(p, tt.ClientOptions{Backend: tt.Ghostty})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { c.CloseQuiet() })
	_, err = c.Run(filepath.Join(bin, p), nil, tt.SpawnOptions{Cols: &w, Rows: &h, WaitReady: tt.Ptr(false)})
	check(t, err)
	shows(t, c, "build-stats")

	return c
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// shows waits until the screen holds text.
func shows(t *testing.T, c *tt.Client, s string) {
	t.Helper()
	check(t, c.GetByText(s, tt.TextSelectorOptions{}).Expect(tt.LocatorExpectOptions{}))
}

// selected waits until text is drawn in reverse video.
func selected(t *testing.T, c *tt.Client, s string) {
	t.Helper()
	inverse := c.GetByStyle(tt.TextStyle{Inverse: tt.Ptr(true)}, tt.StyleSelectorOptions{})
	check(t, inverse.GetByText(s, tt.TextSelectorOptions{}).Expect(tt.LocatorExpectOptions{}))
}

func each(t *testing.T, scenario func(t *testing.T, c *tt.Client)) {
	for _, p := range programs {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			scenario(t, start(t, p, 100, 30))
		})
	}
}

func TestNewestStartsSelected(t *testing.T) {
	each(t, func(t *testing.T, c *tt.Client) {
		selected(t, c, "build-stats")
		state, err := c.State()
		check(t, err)
		if !state.Modes["alternate_screen"] {
			t.Error("the program draws outside the alternate screen")
		}
	})
}

func TestKeyboardOpensABuild(t *testing.T) {
	each(t, func(t *testing.T, c *tt.Client) {
		check(t, c.Type("kkkkk"))
		selected(t, c, "stopped")
		check(t, c.Press("Enter"))
		shows(t, c, "refused: A variable in this command")
		check(t, c.Press("Escape"))
		shows(t, c, "cost per build")
	})
}

func TestClickSelectsThenOpens(t *testing.T) {
	each(t, func(t *testing.T, c *tt.Client) {
		row := c.GetByText("gate-flakes", tt.TextSelectorOptions{}).First()
		check(t, row.Click(tt.LocatorClickOptions{}))
		selected(t, c, "gate-flakes")
		check(t, row.Click(tt.LocatorClickOptions{}))
		shows(t, c, "no failed calls")
	})
}

func TestWheelMovesSelection(t *testing.T) {
	each(t, func(t *testing.T, c *tt.Client) {
		at, err := c.GetByText("build-stats", tt.TextSelectorOptions{}).First().Location()
		check(t, err)
		check(t, c.Mouse.Move(uint16(at.Start.Column), uint16(at.Start.Row)))
		check(t, c.Mouse.Scroll(tt.Up, 1))
		selected(t, c, "notifications")
	})
}

func TestClickABar(t *testing.T) {
	each(t, func(t *testing.T, c *tt.Client) {
		axis := c.GetByText(`1\s+2\s+3\s+4\s+5\s+6\s+7\s+8`, tt.TextSelectorOptions{Regex: true})
		three, err := axis.GetByText("3", tt.TextSelectorOptions{}).Location()
		check(t, err)
		x, y := uint16(three.Start.Column), uint16(three.Start.Row-1)
		check(t, c.Mouse.Click(tt.MouseClickOptions{X: &x, Y: &y}))
		selected(t, c, "stopped")
	})
}

func TestResize(t *testing.T) {
	each(t, func(t *testing.T, c *tt.Client) {
		check(t, c.Resize(70, 20))
		check(t, c.GetByText(`^.{69}[┐╮╗]`, tt.TextSelectorOptions{Regex: true}).Expect(tt.LocatorExpectOptions{}))
		selected(t, c, "build-stats")
	})
}

func TestQuitRestoresTheTerminal(t *testing.T) {
	each(t, func(t *testing.T, c *tt.Client) {
		check(t, c.Type("q"))
		check(t, c.WaitExit(tt.WaitOptions{}))
		state, err := c.State()
		check(t, err)
		if state.Modes["alternate_screen"] {
			t.Error("the program left the terminal on the alternate screen")
		}
	})
}

// fits matches a first line that closes a border at the last of w columns.
func fits(w int) *regexp.Regexp { return regexp.MustCompile(fmt.Sprintf(`\A.{%d}[┐╮╗]\n`, w-1)) }

// TestSameScreen gives Ghostty and x/vt the same inputs, and reports each row where their text differs.
func TestSameScreen(t *testing.T) {
	for _, p := range programs {
		for _, size := range [][2]uint16{{100, 30}, {60, 16}} {
			t.Run(fmt.Sprintf("%s/%dx%d", p, size[0], size[1]), func(t *testing.T) {
				t.Parallel()
				c := start(t, p, 100, 30)
				vt := termtest.Start(t, 100, 30, filepath.Join(bin, p))
				vt.WaitFor("build-stats")
				check(t, c.Resize(size[0], size[1]))
				vt.Resize(int(size[0]), int(size[1]))
				for _, view := range []string{"trend", "detail"} {
					if view == "detail" {
						check(t, c.Type("kkkkk\r"))
						vt.Type("kkkkk\r")
						vt.WaitFor("by cause")
						shows(t, c, "by cause")
					}

					vt.WaitMatch(regexp.MustCompile(`.`))
					if view == "trend" {
						vt.WaitMatch(fits(int(size[0])))
					}

					check(t, c.WaitIdle(tt.WaitOptions{}))
					time.Sleep(200 * time.Millisecond)
					ghostty, err := c.Screenshot("", tt.ScreenshotOptions{})
					check(t, err)
					diff(t, view, ghostty, vt.Screen())
					inverse(t, view, c, vt, size[0], size[1])
					t.Logf("%s: %d rows, row 0 %q", view, len(lines(ghostty)), lines(ghostty)[0])
					png := filepath.Join(t.ArtifactDir(), view+".png")
					_, err = c.Screenshot(png, tt.ScreenshotOptions{})
					check(t, err)
				}
			})
		}
	}
}

func diff(t *testing.T, view, ghostty, vt string) {
	t.Helper()
	g, v := lines(ghostty), lines(vt)
	for i := range max(len(g), len(v)) {
		a, b := at(g, i), at(v, i)
		if a != b {
			t.Errorf("%s row %d:\n  ghostty %q\n  x/vt    %q", view, i, a, b)
		}
	}
}

// inverse reports each cell that one terminal draws in reverse video and the other does not.
func inverse(t *testing.T, view string, c *tt.Client, vt *termtest.Term, w, h uint16) {
	t.Helper()
	cells, err := c.Cells(0, 0, w, h)
	check(t, err)
	n := 0
	for _, cell := range cells {
		if cell.Inverse != vt.Inverse(int(cell.X), int(cell.Y)) {
			t.Errorf("%s: cell %d,%d %q: ghostty inverse %v", view, cell.X, cell.Y, cell.Char, cell.Inverse)
		}

		if cell.Inverse {
			n++
		}
	}

	t.Logf("%s: %d cells compared, %d inverse", view, len(cells), n)
}

func lines(s string) []string {
	out := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range out {
		out[i] = strings.TrimRight(l, " ")
	}

	return out
}

func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}

	return ""
}
