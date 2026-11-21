package kiln_test

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
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
	"internal/git":    {"github.com/fwilkerson/practices/repotest"},
	// claim holds the checks tests share, so only a test imports it.
	"internal/kiln": {"github.com/fwilkerson/practices/claim", "github.com/fwilkerson/practices/repotest"},
	"internal/scenariotest": {"github.com/rogpeppe/go-internal", "github.com/charmbracelet/x/ansi",
		"github.com/charmbracelet/x/vt", "github.com/fwilkerson/practices/repotest"},
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

// TestOneShell holds that internal/kiln runs a command from kiln.json with sh -c only in projectShell.run, in
// shell.go. run ends a command that runs past kiln.json's timeout, or whose child holds its output. No caller waits
// forever.
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

		if file == "internal/kiln/shell.go" || strings.HasSuffix(file, "_test.go") {
			continue
		}

		if shells := strings.Count(string(code), `"/bin/sh", "-c",`); shells > 0 {
			t.Errorf("%s runs sh -c %d times: run the command with projectShell.run", file, shells)
		}
	}
}

// TestHomeForGit holds that a package whose tests run git starts them in hometest.Main or scenariotest.Main. A test's
// git would otherwise read the developer's config. That git would also obey a GIT_DIR from git rebase --exec or a
// hook, writing to another repository.
func TestHomeForGit(t *testing.T) {
	for _, p := range testPackages(t) {
		fset := token.NewFileSet()
		var runs, started bool
		for _, name := range slices.Concat(p.TestGoFiles, p.XTestGoFiles) {
			f, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}

			runs = runs || runsGit(f)
			started = started || startsHome(f)
		}

		if runs && !started {
			t.Errorf("%s: its tests run git; start them with func TestMain(m *testing.M) { hometest.Main(m) }",
				p.ImportPath)
		}
	}
}

// testPackage is what go list says of a package's tests.
type testPackage struct {
	ImportPath, Dir           string
	TestGoFiles, XTestGoFiles []string
}

// testPackages lists each package of the module, with its test files.
func testPackages(t *testing.T) []testPackage {
	cmd := exec.Command("go", "list", "-json=ImportPath,Dir,TestGoFiles,XTestGoFiles", "./...")
	cmd.Stderr = t.Output()
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}

	var pkgs []testPackage
	for dec := json.NewDecoder(bytes.NewReader(out)); dec.More(); {
		var p testPackage
		if err := dec.Decode(&p); err != nil {
			t.Fatal(err)
		}

		pkgs = append(pkgs, p)
	}

	return pkgs
}

// gitImports are the import paths of kiln's packages that run git.
var gitImports = []string{`"github.com/fwilkerson/kiln/internal/git"`, `"github.com/fwilkerson/kiln/internal/kiln"`}

// runsGit reports whether the test file f runs git. f imports a package of kiln's that runs git, or a string in f
// starts a command with git.
func runsGit(f *ast.File) bool {
	imports := func(s *ast.ImportSpec) bool { return slices.Contains(gitImports, s.Path.Value) }
	if slices.ContainsFunc(f.Imports, imports) {
		return true
	}

	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && gitCommand.MatchString(lit.Value) {
			found = true
		}

		return !found
	})

	return found
}

// gitCommand matches a string literal, quotes included, whose text or a shell command in it starts with git.
var gitCommand = regexp.MustCompile("^[\"`]git[\"`\\s]|[;&|(]\\s*git\\s")

// startsHome reports whether the test file f's TestMain calls hometest.Main or scenariotest.Main.
func startsHome(f *ast.File) bool {
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "TestMain" {
			continue
		}

		found := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && homeMain(call.Fun) {
				found = true
			}

			return !found
		})

		return found
	}

	return false
}

// homeMain reports whether fun names hometest.Main or scenariotest.Main.
func homeMain(fun ast.Expr) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Main" {
		return false
	}

	pkg, ok := sel.X.(*ast.Ident)
	return ok && slices.Contains([]string{"hometest", "scenariotest"}, pkg.Name)
}
