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
//   - Only internal/proc imports os/exec, but for tests, a package whose name ends in test, and tools/. So each
//     program kiln runs obeys proc's one policy.
func TestArchitecture(t *testing.T) {
	format := `{{.ImportPath}} {{join .Imports " "}}|{{join .TestImports " "}} {{join .XTestImports " "}}`
	cmd := exec.Command("go", "list", "-f", format, "./...")
	cmd.Stderr = t.Output()
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}

	for line := range strings.Lines(string(out)) {
		code, tests, _ := strings.Cut(strings.TrimSpace(line), "|")
		pkg, imports, _ := strings.Cut(code, " ")
		for imp := range strings.FieldsSeq(imports) {
			if why := violation(pkg, imp, false); why != "" {
				t.Errorf("%s imports %s: %s", pkg, imp, why)
			}
		}

		for imp := range strings.FieldsSeq(tests) {
			if why := violation(pkg, imp, true); why != "" {
				t.Errorf("%s's tests import %s: %s", pkg, imp, why)
			}
		}
	}
}

// violation says why pkg may not import imp, or "" when it may. test is true of an import by pkg's tests alone.
func violation(pkg, imp string, test bool) string {
	impTop := locate(imp)
	switch {
	case foreign(imp) && !allowed(pkg, imp):
		return "list its module for this package in outside"
	case impTop == "cmd" || impTop == "uat":
		return "nothing imports " + impTop
	case imp == "os/exec" && !mayExec(pkg, test):
		return "run the program with proc.Run, proc.RunSession or proc.RunAttached, which hold kiln's policy for a " +
			"child process"
	}

	return ""
}

// mayExec reports whether pkg, or its tests when test is true, may import os/exec. internal/proc runs each program for
// the rest of kiln. A package whose name ends in test serves tests alone, and tools/ builds or installs kiln.
func mayExec(pkg string, test bool) bool {
	within := strings.TrimPrefix(pkg, "github.com/fwilkerson/kiln/")
	switch {
	case test, within == "internal/proc", strings.HasSuffix(within, "test"), locate(pkg) == "tools":
		return true
	}

	return false
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
	// A test build's snapshot clones a directory, as only golang.org/x/sys exports.
	"internal/crash": {"golang.org/x/sys"},
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

// TestOneShell holds that internal/kiln runs a command from kiln.json with /bin/sh only in projectShell.run, in
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

		if shells := len(shell.FindAll(code, -1)); shells > 0 {
			t.Errorf("%s runs sh %d times: run the command with projectShell.run", file, shells)
		}
	}
}

// shell matches a Go string that names sh, as a program kiln would run.
var shell = regexp.MustCompile(`"(/bin/)?sh"`)

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

// TestExhaustive holds that each switch on an enum of enums names every value of it. So does each map or array keyed
// by one. The compiler takes one that misses a value, and that value then falls through unseen.
func TestExhaustive(t *testing.T) {
	for dir, typ := range enums {
		fset, files := parseDir(t, dir)
		values := enumValues(files, typ)
		for _, f := range files {
			ast.Inspect(f, func(n ast.Node) bool {
				if missing := unlisted(n, values); len(missing) > 0 {
					t.Errorf("%s: names no %s: name each %s", fset.Position(n.Pos()), strings.Join(missing, ", "), typ)
				}

				return true
			})
		}
	}
}

// enums are the enums whose switches and tables TestExhaustive holds, by the directory of the package that declares
// each. kiln tui draws a State left out as no other.
var enums = map[string]string{"internal/tui": "State"}

// parseDir parses the Go files in dir, but its tests.
func parseDir(t *testing.T, dir string) (*token.FileSet, []*ast.File) {
	names, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	var files []*ast.File
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}

		files = append(files, f)
	}

	return fset, files
}

// enumValues names the constants of the type typ that files declare.
func enumValues(files []*ast.File, typ string) []string {
	var names []string
	for _, f := range files {
		for _, decl := range f.Decls {
			if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.CONST {
				names = append(names, typedConsts(gen, typ)...)
			}
		}
	}

	return names
}

// typedConsts names the constants of the type typ in the block gen. A constant with no type and no value takes the
// type of the one above it, as with iota.
func typedConsts(gen *ast.GenDecl, typ string) []string {
	var names []string
	of := ""
	for _, spec := range gen.Specs {
		v := spec.(*ast.ValueSpec)
		if v.Type != nil || len(v.Values) > 0 {
			of = ""
			if id, ok := v.Type.(*ast.Ident); ok {
				of = id.Name
			}
		}

		if of == typ {
			for _, name := range v.Names {
				names = append(names, name.Name)
			}
		}
	}

	return names
}

// unlisted names each of values that n leaves out, where n lists at least one. n lists a value as a switch's case, or
// as a key of a map or an array literal.
func unlisted(n ast.Node, values []string) []string {
	named := listedNames(n)
	if !slices.ContainsFunc(values, func(v string) bool { return slices.Contains(named, v) }) {
		return nil
	}

	return slices.DeleteFunc(slices.Clone(values), func(v string) bool { return slices.Contains(named, v) })
}

// listedNames names each bare name that n lists. A switch lists its cases, and a map or an array literal its keys.
func listedNames(n ast.Node) []string {
	var keys []ast.Expr
	switch n := n.(type) {
	case *ast.SwitchStmt:
		for _, c := range n.Body.List {
			keys = append(keys, c.(*ast.CaseClause).List...)
		}
	case *ast.CompositeLit:
		switch n.Type.(type) {
		case *ast.MapType, *ast.ArrayType:
			for _, e := range n.Elts {
				if kv, ok := e.(*ast.KeyValueExpr); ok {
					keys = append(keys, kv.Key)
				}
			}
		}
	}

	var names []string
	for _, k := range keys {
		if id, ok := k.(*ast.Ident); ok {
			names = append(names, id.Name)
		}
	}

	return names
}
