package kiln_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestStyle fails while the practices module's style command finds a problem. Run go tool style . to fix blank lines
// in place. It only reports wide lines and long comment sentences, so fix those by hand.
func TestStyle(t *testing.T) {
	out, err := exec.Command("go", "tool", "style", "-l", ".").CombinedOutput()
	if err != nil {
		t.Errorf("%s%v", out, err)
	}
}

// TestGuide fails while docs/guide.md differs from what go tool guide prints. CLAUDE.md loads that file, so a session
// reads the guide of the practices module that go.mod names. Run go tool guide > docs/guide.md to bring it up to date.
func TestGuide(t *testing.T) {
	want, err := exec.Command("go", "tool", "guide").Output()
	if err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile("docs/guide.md")
	if err != nil || !bytes.Equal(got, want) {
		t.Errorf("docs/guide.md differs from the guide, %v: run go tool guide > docs/guide.md", err)
	}
}

// TestModern fails while go fix would change the code, such as to use min or range over an int. Run go fix ./... to
// apply its changes.
func TestModern(t *testing.T) {
	out, err := exec.Command("go", "fix", "-diff", "./...").CombinedOutput()
	if err != nil || len(out) > 0 {
		t.Errorf("%s%v", out, err)
	}
}

// TestNames fails on a code span in a doc that names nothing declared in kiln's Go code. It skips lower-case words
// and names in capitals. It also skips Markdown file names and names from packages outside kiln.
func TestNames(t *testing.T) {
	k, err := kilnNames()
	if err != nil {
		t.Fatal(err)
	}

	var docs []string
	for _, pattern := range []string{".kiln/agents/*.md", ".kiln/skills/*/*.md"} {
		found, _ := filepath.Glob(pattern)
		docs = append(docs, found...)
	}

	for _, doc := range append(docs, "README.md") {
		b, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}

		for _, s := range k.unknown(string(b)) {
			t.Errorf("%s:%d: `%s` names nothing in kiln. Fix it, or write it in words.", doc, s.line, s.name)
		}
	}
}

// names is what kiln's Go code declares, and the packages it declares them in.
type names struct {
	declared, packages map[string]bool
}

// kilnNames reads the names kiln's Go files declare. It skips testdata and directories whose names start with a dot.
func kilnNames() (names, error) {
	k := names{declared: map[string]bool{}, packages: map[string]bool{}}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case skipped(path, d):
			return filepath.SkipDir
		case !strings.HasSuffix(path, ".go"):
			return nil
		}

		return k.read(path)
	})

	return k, err
}

// skipped reports whether d, at path, is a directory below the root that kilnNames leaves out.
func skipped(path string, d fs.DirEntry) bool {
	return d.IsDir() && path != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata")
}

// read adds the names the Go file at path declares, alone and qualified by its package.
func (k names) read(path string) error {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		return err
	}

	pkg := strings.TrimSuffix(f.Name.Name, "_test")
	k.packages[pkg] = true
	for _, name := range declarations(f) {
		k.declared[name] = true
		k.declared[pkg+"."+name] = true
	}

	return nil
}

// codeSpan is a code span that holds a Go name, qualified or not.
var codeSpan = regexp.MustCompile("`([A-Za-z]\\w*(?:\\.[A-Za-z]\\w*)*)`")

// span is a code span's name, on a line of a doc.
type span struct {
	line int
	name string
}

// unknown lists each code span of doc that names nothing k holds.
func (k names) unknown(doc string) []span {
	var unknown []span
	for i, line := range strings.Split(doc, "\n") {
		for _, m := range codeSpan.FindAllStringSubmatch(line, -1) {
			if !k.allows(m[1]) {
				unknown = append(unknown, span{i + 1, m[1]})
			}
		}
	}

	return unknown
}

// allows reports whether a doc may name name: kiln declares it, or TestNames skips it.
func (k names) allows(name string) bool {
	qualifier, _, qualified := strings.Cut(name, ".")
	switch {
	case name == strings.ToLower(name), name == strings.ToUpper(name), strings.HasSuffix(name, ".md"):
		return true
	case qualified && qualifier == strings.ToLower(qualifier) && !k.packages[qualifier]:
		return true
	}

	return k.declared[name]
}

// declarations lists the names a file declares at its top level, with each method and field also as Type.Name.
func declarations(f *ast.File) []string {
	var names []string
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			names = append(names, funcNames(d)...)
		case *ast.GenDecl:
			names = append(names, genNames(d)...)
		}
	}

	return names
}

// funcNames lists a func's name, and a method's also as Type.Name.
func funcNames(d *ast.FuncDecl) []string {
	if d.Recv == nil {
		return []string{d.Name.Name}
	}

	recv, _, _ := strings.Cut(strings.TrimPrefix(types.ExprString(d.Recv.List[0].Type), "*"), "[")
	return []string{d.Name.Name, recv + "." + d.Name.Name}
}

// genNames lists the types, constants and variables d declares, with each type's fields and methods.
func genNames(d *ast.GenDecl) []string {
	var names []string
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			names = append(names, typeNames(s)...)
		case *ast.ValueSpec:
			for _, n := range s.Names {
				names = append(names, n.Name)
			}
		}
	}

	return names
}

// typeNames lists a type's name, and each of its fields or methods as Type.Name.
func typeNames(s *ast.TypeSpec) []string {
	names := []string{s.Name.Name}
	switch typ := s.Type.(type) {
	case *ast.StructType:
		return append(names, members(s.Name.Name, typ.Fields)...)
	case *ast.InterfaceType:
		return append(names, members(s.Name.Name, typ.Methods)...)
	}

	return names
}

// members lists each name of fields, alone and as typ.Name.
func members(typ string, fields *ast.FieldList) []string {
	var names []string
	for _, field := range fields.List {
		for _, n := range field.Names {
			names = append(names, n.Name, typ+"."+n.Name)
		}
	}

	return names
}
