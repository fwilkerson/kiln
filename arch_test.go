package kiln_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestArchitecture keeps kiln's layers apart. It checks these rules:
//   - Nothing imports cmd or uat.
//   - A package imports from outside the standard library only the modules that outside lists for it.
func TestArchitecture(t *testing.T) {
	format := `{{.ImportPath}} {{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}}`
	cmd := exec.Command("go", "list", "-f", format, "./...")
	cmd.Stderr = t.Output()
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}

	for line := range strings.Lines(string(out)) {
		pkg, imports, _ := strings.Cut(strings.TrimSpace(line), " ")
		for imp := range strings.FieldsSeq(imports) {
			if why := violation(pkg, imp); why != "" {
				t.Errorf("%s imports %s: %s", pkg, imp, why)
			}
		}
	}
}

func violation(pkg, imp string) string {
	impTop := locate(imp)
	switch {
	case foreign(imp) && !allowed(pkg, imp):
		return "list its module for this package in outside"
	case impTop == "cmd" || impTop == "uat":
		return "nothing imports " + impTop
	}

	return ""
}

// outside lists the modules from outside the standard library that each package may import. A key is a package path
// within kiln.
var outside = map[string][]string{
	// The root holds only tests, which take the checks of a whole repository from repotest.
	"github.com/fwilkerson/kiln": {"github.com/fwilkerson/practices/repotest"},
	// repotest's Facts holds a package doc's numbers to its constants, so only a test imports it.
	"internal/proc":   {"golang.org/x/sys", "github.com/fwilkerson/practices/repotest"},
	"internal/claude": {"golang.org/x/sys", "github.com/fwilkerson/practices/repotest"},
	// claim holds the checks tests share, so only a test imports it.
	"internal/kiln": {"github.com/fwilkerson/practices/claim", "github.com/fwilkerson/practices/repotest"},
	"internal/scenariotest": {"github.com/rogpeppe/go-internal", "github.com/charmbracelet/x/ansi",
		"github.com/charmbracelet/x/vt"},
	"internal/tui": {"charm.land/bubbletea/v2", "charm.land/bubbles/v2", "charm.land/lipgloss/v2",
		"github.com/alecthomas/chroma/v2", "github.com/charmbracelet/x/ansi", "github.com/charmbracelet/x/term",
		"github.com/charmbracelet/x/vt", "github.com/fwilkerson/practices/repotest"},
	"internal/termtest": {"github.com/creack/pty", "github.com/charmbracelet/x/vt",
		"github.com/charmbracelet/ultraviolet", "golang.org/x/image"},
}

// foreign reports whether imp is from outside kiln and the standard library, whose first path element has no dot.
func foreign(imp string) bool {
	return locate(imp) == "" && strings.Contains(strings.Split(imp, "/")[0], ".")
}

// allowed reports whether pkg may import imp, a package from outside the standard library.
func allowed(pkg, imp string) bool {
	within := strings.TrimPrefix(pkg, "github.com/fwilkerson/kiln/")
	return slices.ContainsFunc(outside[within], func(mod string) bool {
		return imp == mod || strings.HasPrefix(imp, mod+"/")
	})
}

// locate returns the top directory of a package within kiln, or "" for a package outside it.
func locate(path string) string {
	rest, ok := strings.CutPrefix(path, "github.com/fwilkerson/kiln/")
	if !ok {
		return ""
	}

	top, _, _ := strings.Cut(rest, "/")
	return top
}

// TestOneShell holds that internal/kiln runs a command from kiln.json with sh -c only in runShell, in monitor.go.
// runShell ends a command whose child holds its output, so no other caller can wait on that child forever.
func TestOneShell(t *testing.T) {
	files, err := filepath.Glob("internal/kiln/*.go")
	if err != nil {
		t.Fatal(err)
	}

	for _, file := range files {
		code, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		if file == "internal/kiln/monitor.go" || strings.HasSuffix(file, "_test.go") {
			continue
		}

		if shells := strings.Count(string(code), `"/bin/sh", "-c",`); shells > 0 {
			t.Errorf("%s runs sh -c %d times: run the command with runShell", file, shells)
		}
	}
}

// TestHomeForGit holds that each package whose tests run git starts them with hometest.Main, or scenariotest.Main,
// which drops the same variables. A test's git would otherwise read the developer's config. It would also obey a
// GIT_DIR that git rebase --exec or a hook passed on, and write to that repository.
func TestHomeForGit(t *testing.T) {
	cmd := exec.Command("go", "list", "-f", `{{.Dir}} {{join .TestGoFiles " "}} {{join .XTestGoFiles " "}}`, "./...")
	cmd.Stderr = t.Output()
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}

	top, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	for line := range strings.Lines(string(out)) {
		dir, files, _ := strings.Cut(strings.TrimSpace(line), " ")
		var code strings.Builder
		for file := range strings.FieldsSeq(files) {
			raw, err := os.ReadFile(filepath.Join(dir, file))
			if err != nil {
				t.Fatal(err)
			}

			code.Write(raw)
		}

		text := code.String()
		started := strings.Contains(text, "hometest.Main(m)") || strings.Contains(text, "scenariotest.Main(m)")
		if runsGit.MatchString(text) && !started {
			rel, _ := filepath.Rel(top, dir)
			t.Errorf("%s: its tests run git; start them with func TestMain(m *testing.M) { hometest.Main(m) }", rel)
		}
	}
}

// runsGit matches a test's call of git: a command it starts, or a line of shell that begins with git.
var runsGit = regexp.MustCompile(`exec\.Command(Context)?\((ctx, |t\.Context\(\), )?"git"|["\x60;&|(]\s*git [a-z-]+`)
