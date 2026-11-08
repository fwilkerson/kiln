// Command style keeps the rules gofmt leaves out, so they hold without anyone judging them:
//
//   - A file is formatted as gofmt formats it.
//   - An if that checks what the statement above it assigned sits directly under it.
//   - Otherwise a blank line follows a block's closing brace.
//   - A line is at most 120 columns wide, a tab counting as 4. A row of a table may be wider.
//   - A sentence in a comment has at most 20 words. A list item, an indented line or an example's output may be
//     longer.
//
// It fixes formatting and blank lines in place, and reports wide lines and long sentences. With -l it
// changes nothing, lists what is wrong, and exits 1 if anything is:
//
//	go run ./tools/style .
//	go run ./tools/style -l .
//
// Where to break a long run of statements into steps is still the writer's call.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	maxWidth = 120
	maxWords = 20
)

type problem struct {
	line  int
	msg   string
	fixed bool
}

func main() {
	list := flag.Bool("l", false, "list what is wrong and change nothing")
	flag.Parse()
	roots := flag.Args()
	if len(roots) == 0 {
		roots = []string{"."}
	}

	wrong := false
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return err
			case d.IsDir() && path != root && skip(d.Name()):
				return filepath.SkipDir
			case d.IsDir() || !strings.HasSuffix(path, ".go"):
				return nil
			}

			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			out, problems, err := style(path, src)
			if err != nil {
				return err
			}

			for _, p := range problems {
				if *list || !p.fixed {
					fmt.Printf("%s:%d: %s\n", path, p.line, p.msg)
					wrong = true
				}
			}

			if *list || string(out) == string(src) {
				return nil
			}

			return os.WriteFile(path, out, 0o644)
		})
		if err != nil {
			log.Fatal(err)
		}
	}

	if wrong {
		os.Exit(1)
	}
}

// skip reports whether ./... leaves dir out, as it does testdata, vendor, and names that start with . or _.
func skip(dir string) bool {
	return strings.HasPrefix(dir, ".") || strings.HasPrefix(dir, "_") || dir == "testdata" || dir == "vendor"
}

const (
	needsBlank = "a blank line follows a closing brace"
	extraBlank = "an if sits directly under the statement it checks"
	tooWide    = "the line is wider than 120 columns"
	tooLong    = "a sentence in this comment has more than 20 words"
	notGofmt   = "the file is not formatted as gofmt formats it"
)

// style returns src with its blank lines and formatting fixed, and the problems it found. Each problem's line is in
// src.
func style(path string, src []byte) ([]byte, []problem, error) {
	out, problems, err := spacing(path, src)
	if err != nil {
		return nil, nil, err
	}

	formatted, err := format.Source(out)
	if err != nil {
		return nil, nil, err
	}

	if string(formatted) != string(out) {
		problems = append(problems, problem{1, notGofmt, true})
	}

	slices.SortFunc(problems, func(p, q problem) int { return p.line - q.line })
	return formatted, problems, nil
}

// spacing returns src with its blank lines fixed, and the problems it found.
func spacing(path string, src []byte) ([]byte, []problem, error) {
	var problems []problem
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}

	lines := strings.Split(string(src), "\n")
	line := func(p token.Pos) int { return fset.Position(p).Line }
	blank := func(n int) bool { return strings.TrimSpace(lines[n-1]) == "" }
	addAfter, drop := map[int]bool{}, map[int]bool{}
	for a, b := range pairs(f) {
		end, next := line(a.End()), line(b.Pos())
		gap := slices.ContainsFunc(lineRange(end+1, next), blank)
		switch {
		case checks(a, b) && gap:
			problems = append(problems, problem{next, extraBlank, true})
			for _, n := range lineRange(end+1, next) {
				drop[n] = drop[n] || blank(n)
			}
		case !checks(a, b) && closesBlock(a, line, lines) && !gap:
			problems = append(problems, problem{next, needsBlank, true})
			addAfter[end] = true
		}
	}

	rows := tableRows(f, line)
	for i, l := range lines {
		if width(l) > maxWidth && !rows[i+1] {
			problems = append(problems, problem{i + 1, tooWide, false})
		}
	}

	for _, n := range longSentences(f, line) {
		problems = append(problems, problem{n, tooLong, false})
	}

	var out []string
	for i, l := range lines {
		if !drop[i+1] {
			out = append(out, l)
		}

		if addAfter[i+1] {
			out = append(out, "")
		}
	}

	return []byte(strings.Join(out, "\n")), problems, nil
}

// pairs yields each statement in a function body with the statement after it. The cases of a switch or select are
// not statements in this sense.
func pairs(f *ast.File) func(yield func(a, b ast.Stmt) bool) {
	return func(yield func(a, b ast.Stmt) bool) {
		ast.Inspect(f, func(n ast.Node) bool {
			var list []ast.Stmt
			switch n := n.(type) {
			case *ast.BlockStmt:
				list = n.List
			case *ast.CaseClause:
				list = n.Body
			case *ast.CommClause:
				list = n.Body
			}

			for i := 1; i < len(list); i++ {
				if clause(list[i]) {
					continue
				}

				if !yield(list[i-1], list[i]) {
					return false
				}
			}

			return true
		})
	}
}

func clause(s ast.Stmt) bool {
	switch s.(type) {
	case *ast.CaseClause, *ast.CommClause:
		return true
	}

	return false
}

func lineRange(from, to int) []int {
	var r []int
	for n := from; n < to; n++ {
		r = append(r, n)
	}

	return r
}

// closesBlock reports whether s ends with a block's closing brace, as an if, a for or a func literal does. The brace
// must start its line. A closing composite literal does not count.
func closesBlock(s ast.Stmt, line func(token.Pos) int, lines []string) bool {
	end := line(s.End())
	if !strings.HasPrefix(strings.TrimSpace(lines[end-1]), "}") {
		return false
	}

	found := false
	ast.Inspect(s, func(n ast.Node) bool {
		if b, ok := n.(*ast.BlockStmt); ok && line(b.Rbrace) == end {
			found = true
		}

		return !found
	})

	return found
}

// checks reports whether next is an if statement, with no init, whose condition uses a name that prev assigns.
func checks(prev, next ast.Stmt) bool {
	as, ok := prev.(*ast.AssignStmt)
	is, isIf := next.(*ast.IfStmt)
	if !ok || !isIf || is.Init != nil {
		return false
	}

	names := map[string]bool{}
	for _, e := range as.Lhs {
		if id, ok := e.(*ast.Ident); ok && id.Name != "_" {
			names[id.Name] = true
		}
	}

	used := false
	ast.Inspect(is.Cond, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && names[id.Name] {
			used = true
		}

		return !used
	})

	return used
}

// tableRows returns the line each element starts on, in every composite literal that spans more than one line.
func tableRows(f *ast.File, line func(token.Pos) int) map[int]bool {
	rows := map[int]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.CompositeLit); ok && line(c.Lbrace) != line(c.Rbrace) {
			for _, e := range c.Elts {
				rows[line(e.Pos())] = true
			}
		}

		return true
	})

	return rows
}

// longSentences returns the line each comment sentence of more than maxWords words starts on. It skips a directive,
// such as //go:embed, and a line indented past one space, as a list item is. It also skips an example's output.
func longSentences(f *ast.File, line func(token.Pos) int) []int {
	var long []int
	for _, g := range f.Comments {
		if output(g) {
			continue
		}

		start, words := 0, 0
		end := func() {
			if words > maxWords {
				long = append(long, start)
			}

			words = 0
		}

		for _, c := range g.List {
			text, ok := strings.CutPrefix(c.Text, "// ")
			if !ok || strings.HasPrefix(text, " ") || strings.HasPrefix(text, "\t") {
				end()
				continue
			}

			for w := range strings.FieldsSeq(text) {
				if words == 0 {
					start = line(c.Pos())
				}

				words++
				if t := strings.TrimRight(w, `"')`); t != "" && strings.ContainsRune(".?!", rune(t[len(t)-1])) {
					end()
				}
			}
		}

		end()
	}

	return long
}

// output reports whether g is the output an example must print. go test compares it, and go doc does not show it.
func output(g *ast.CommentGroup) bool {
	first := strings.ToLower(g.List[0].Text)
	return strings.HasPrefix(first, "// output:") || strings.HasPrefix(first, "// unordered output:")
}

func width(l string) int {
	return len([]rune(l)) + 3*strings.Count(l, "\t")
}
