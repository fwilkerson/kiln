package kiln_test

import (
	"cmp"
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

// TestStyle fails while tools/style finds a problem. Running the tool fixes blank lines and the layout in place. It
// only reports wide lines and long comment sentences, so fix those by hand.
func TestStyle(t *testing.T) {
	out, err := exec.Command("go", "run", "./tools/style", "-l", ".").CombinedOutput()
	if err != nil {
		t.Errorf("%s%v", out, err)
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
	declared, packages := map[string]bool{}, map[string]bool{}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() && path != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata") {
			return cmp.Or(err, filepath.SkipDir)
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}

		pkg := strings.TrimSuffix(f.Name.Name, "_test")
		packages[pkg] = true
		for _, name := range declarations(f) {
			declared[name] = true
			declared[pkg+"."+name] = true
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	docs, _ := filepath.Glob("guidance/*/*.md")
	skills, _ := filepath.Glob("guidance/skills/*/*.md")
	docs = append(append(docs, skills...), "README.md")
	span := regexp.MustCompile("`([A-Za-z]\\w*(?:\\.[A-Za-z]\\w*)*)`")
	for _, doc := range docs {
		b, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}

		for i, line := range strings.Split(string(b), "\n") {
			for _, m := range span.FindAllStringSubmatch(line, -1) {
				name := m[1]
				qualifier, _, qualified := strings.Cut(name, ".")
				switch {
				case name == strings.ToLower(name), name == strings.ToUpper(name), strings.HasSuffix(name, ".md"):
				case qualified && qualifier == strings.ToLower(qualifier) && !packages[qualifier]:
				case !declared[name]:
					t.Errorf("%s:%d: `%s` names nothing in kiln. Fix it, or write it in words.", doc, i+1, name)
				}
			}
		}
	}
}

// declarations lists the names a file declares at its top level, with each method and field also as Type.Name.
func declarations(f *ast.File) []string {
	var names []string
	members := func(typ string, fields *ast.FieldList) {
		for _, field := range fields.List {
			for _, n := range field.Names {
				names = append(names, n.Name, typ+"."+n.Name)
			}
		}
	}

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			names = append(names, d.Name.Name)
			if d.Recv != nil {
				recv, _, _ := strings.Cut(strings.TrimPrefix(types.ExprString(d.Recv.List[0].Type), "*"), "[")
				names = append(names, recv+"."+d.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					names = append(names, s.Name.Name)
					switch typ := s.Type.(type) {
					case *ast.StructType:
						members(s.Name.Name, typ.Fields)
					case *ast.InterfaceType:
						members(s.Name.Name, typ.Methods)
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						names = append(names, n.Name)
					}
				}
			}
		}
	}

	return names
}
