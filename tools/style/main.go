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

	r := &run{list: *list}
	for _, root := range roots {
		if err := filepath.WalkDir(root, r.visit(root)); err != nil {
			log.Fatal(err)
		}
	}

	if r.wrong {
		os.Exit(1)
	}
}

// run is one run of style over its roots.
type run struct {
	// list is -l: list every problem, and change nothing.
	list bool
	// wrong is true once a problem was listed.
	wrong bool
}

// visit styles each Go file under root that ./... would hold.
func (r *run) visit(root string) fs.WalkDirFunc {
	return func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case skips(root, path, d):
			return filepath.SkipDir
		case d.IsDir() || !strings.HasSuffix(path, ".go"):
			return nil
		}

		return r.file(path)
	}
}

// skips reports whether the walk from root leaves out d at path, a directory below root that ./... leaves out.
func skips(root, path string, d fs.DirEntry) bool {
	return d.IsDir() && path != root && skip(d.Name())
}

// skip reports whether ./... leaves dir out, as it does testdata, vendor, and names that start with . or _.
func skip(dir string) bool {
	return strings.HasPrefix(dir, ".") || strings.HasPrefix(dir, "_") || dir == "testdata" || dir == "vendor"
}

// file styles the file at path. It reports what it found, and writes the fixes back unless r lists.
func (r *run) file(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	out, problems, err := style(path, src)
	if err != nil {
		return err
	}

	r.report(path, problems)
	if r.list || string(out) == string(src) {
		return nil
	}

	return os.WriteFile(path, out, 0o644)
}

// report prints each problem in path that r lists, or that style could not fix.
func (r *run) report(path string, problems []problem) {
	for _, p := range problems {
		if r.list || !p.fixed {
			fmt.Printf("%s:%d: %s\n", path, p.line, p.msg)
			r.wrong = true
		}
	}
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
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}

	s := source{fset: fset, file: f, lines: strings.Split(string(src), "\n")}
	fix := fixes{addAfter: map[int]bool{}, drop: map[int]bool{}}
	problems := s.blankLines(fix)
	problems = append(problems, s.wideLines()...)
	for _, n := range longSentences(f, s.line) {
		problems = append(problems, problem{n, tooLong, false})
	}

	return fix.apply(s.lines), problems, nil
}

// source is a parsed Go file, with its lines.
type source struct {
	fset  *token.FileSet
	file  *ast.File
	lines []string
}

func (s source) line(p token.Pos) int { return s.fset.Position(p).Line }

// fixes are the lines, by number, that spacing drops, and those it adds a blank line after.
type fixes struct {
	addAfter, drop map[int]bool
}

// apply returns lines with fix made.
func (fix fixes) apply(lines []string) []byte {
	var out []string
	for i, l := range lines {
		if !fix.drop[i+1] {
			out = append(out, l)
		}

		if fix.addAfter[i+1] {
			out = append(out, "")
		}
	}

	return []byte(strings.Join(out, "\n"))
}

// blankLines returns the problems in the blank lines between statements, and keeps how to fix each in fix.
func (s source) blankLines(fix fixes) []problem {
	var problems []problem
	for a, b := range pairs(s.file) {
		if p, ok := s.spaced(a, b, fix); ok {
			problems = append(problems, p)
		}
	}

	return problems
}

// spaced returns the problem in the blank lines between a and b, the statement after it, and false if none.
func (s source) spaced(a, b ast.Stmt, fix fixes) (problem, bool) {
	end, next := s.line(a.End()), s.line(b.Pos())
	blanks := s.blanks(a, b)
	gap := len(blanks) > 0
	checked := checks(a, b)
	switch {
	case checked && gap:
		for _, n := range blanks {
			fix.drop[n] = true
		}

		return problem{next, extraBlank, true}, true
	case checked, gap, !s.closesBlock(a):
		return problem{}, false
	}

	fix.addAfter[end] = true
	return problem{next, needsBlank, true}, true
}

// blanks returns the blank lines between a and b, the statement after it.
func (s source) blanks(a, b ast.Stmt) []int {
	var blanks []int
	for n := s.line(a.End()) + 1; n < s.line(b.Pos()); n++ {
		if strings.TrimSpace(s.lines[n-1]) == "" {
			blanks = append(blanks, n)
		}
	}

	return blanks
}

// wideLines returns a problem for each line wider than maxWidth that is not a table's row.
func (s source) wideLines() []problem {
	var problems []problem
	rows := tableRows(s.file, s.line)
	for i, l := range s.lines {
		if width(l) > maxWidth && !rows[i+1] {
			problems = append(problems, problem{i + 1, tooWide, false})
		}
	}

	return problems
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

// closesBlock reports whether s ends with a block's closing brace, as an if, a for or a func literal does. The brace
// must start its line. A closing composite literal does not count.
func (s source) closesBlock(st ast.Stmt) bool {
	end := s.line(st.End())
	if !strings.HasPrefix(strings.TrimSpace(s.lines[end-1]), "}") {
		return false
	}

	found := false
	ast.Inspect(st, func(n ast.Node) bool {
		if b, ok := n.(*ast.BlockStmt); ok && s.line(b.Rbrace) == end {
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
	switch {
	case !ok, !isIf, is.Init != nil:
		return false
	}

	return uses(is.Cond, assigned(as))
}

// assigned returns the names as assigns, but the blank one.
func assigned(as *ast.AssignStmt) map[string]bool {
	names := map[string]bool{}
	for _, e := range as.Lhs {
		if id, ok := e.(*ast.Ident); ok && id.Name != "_" {
			names[id.Name] = true
		}
	}

	return names
}

// uses reports whether cond uses one of names.
func uses(cond ast.Expr, names map[string]bool) bool {
	used := false
	ast.Inspect(cond, func(n ast.Node) bool {
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
	var c counter
	for _, g := range f.Comments {
		if !output(g) {
			c.group(g, line)
		}
	}

	return c.long
}

// counter counts the words of each comment sentence, and keeps the line each long one starts on.
type counter struct {
	start, words int
	long         []int
}

// group counts the sentences of g. A sentence ends with g, and at a line that holds no prose.
func (c *counter) group(g *ast.CommentGroup, line func(token.Pos) int) {
	for _, comment := range g.List {
		text, ok := prose(comment)
		if !ok {
			c.end()
			continue
		}

		at := line(comment.Pos())
		for w := range strings.FieldsSeq(text) {
			c.word(w, at)
		}
	}

	c.end()
}

// word counts w, a word on the line at.
func (c *counter) word(w string, at int) {
	if c.words == 0 {
		c.start = at
	}

	c.words++
	if endsSentence(w) {
		c.end()
	}
}

// end ends the sentence counted so far.
func (c *counter) end() {
	if c.words > maxWords {
		c.long = append(c.long, c.start)
	}

	c.words = 0
}

// prose returns the text of comment, and false for a directive or a line indented past one space.
func prose(comment *ast.Comment) (string, bool) {
	text, ok := strings.CutPrefix(comment.Text, "// ")
	return text, ok && !strings.HasPrefix(text, " ") && !strings.HasPrefix(text, "\t")
}

// endsSentence reports whether w ends with a period, a question mark or an exclamation mark, before any closing quote.
func endsSentence(w string) bool {
	t := strings.TrimRight(w, `"')`)
	return t != "" && strings.ContainsRune(".?!", rune(t[len(t)-1]))
}

// output reports whether g is the output an example must print. go test compares it, and go doc does not show it.
func output(g *ast.CommentGroup) bool {
	first := strings.ToLower(g.List[0].Text)
	return strings.HasPrefix(first, "// output:") || strings.HasPrefix(first, "// unordered output:")
}

func width(l string) int {
	return len([]rune(l)) + 3*strings.Count(l, "\t")
}
