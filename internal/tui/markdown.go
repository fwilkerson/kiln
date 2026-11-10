package tui

import (
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/x/ansi"
)

// markdown draws Markdown source as written, colored by its syntax, in lines that fit width columns. A line of prose
// wider than that wraps under its own text. A table's row or a fenced line never wraps, since it would no longer
// line up. The screen cuts it at its edge, and h and l scroll it.
func markdown(text string, width int) []string {
	source := sourceLines(strings.Split(strings.ReplaceAll(strings.TrimRight(text, "\n"), "\t", "    "), "\n"))
	colored := source.highlight()
	var drawn []string
	fenced := false
	for i, line := range source {
		trimmed := strings.TrimSpace(line)
		fence := isFence(trimmed)
		switch {
		case fenced, fence, strings.HasPrefix(trimmed, "|"), ansi.StringWidth(line) <= width:
			drawn = append(drawn, colored[i])
		default:
			drawn = append(drawn, wrap(colored[i], len(indent.FindString(line)), width)...)
		}

		if fence {
			fenced = !fenced
		}
	}

	return drawn
}

// sourceLines are lines of Markdown source, or of the code in one of its fences.
type sourceLines []string

// indent is a line's indent, with its list item's marker if it has one.
var indent = regexp.MustCompile(`^ *(?:(?:[-*+]|\d+[.)]) +)?`)

// isFence reports whether a line, its indent trimmed, opens or closes a fenced block.
func isFence(trimmed string) bool {
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

// highlight colors each line of Markdown source by its syntax, and the code in each fence by the fence's language.
// It colors prose a line at a time. So no lexer state, such as an unclosed HTML tag, reaches the next line. A fence
// inside a list item counts too: its code is colored without its indent. Each line it answers has its colors closed.
func (source sourceLines) highlight() []string {
	markdown := lexers.Get("markdown")
	var colored []string
	for i := 0; i < len(source); i++ {
		colored = append(colored, source[i:i+1].paint(markdown)...)
		if !isFence(strings.TrimSpace(source[i])) {
			continue
		}

		end := source.closing(i)
		colored = append(colored, fencedCode(source[i], source[i+1:end])...)
		if end < len(source) {
			colored = append(colored, source[end:end+1].paint(markdown)...)
		}

		i = end
	}

	return colored
}

// closing is the index of the line that closes the fence opened at open, or len(source) when none does.
func (source sourceLines) closing(open int) int {
	end := open + 1
	for end < len(source) && !isFence(strings.TrimSpace(source[end])) {
		end++
	}

	return end
}

// fencedCode colors the code inside a fence by the language its opening line names. It colors the code without the
// fence's indent, then puts the indent back.
func fencedCode(fence string, code sourceLines) []string {
	pad := fence[:len(fence)-len(strings.TrimLeft(fence, " "))]
	var lexer chroma.Lexer
	if lang := strings.Fields(strings.TrimLeft(strings.TrimSpace(fence), "`~")); len(lang) > 0 {
		lexer = lexers.Get(lang[0])
	}

	unindented := make(sourceLines, len(code))
	for i, line := range code {
		unindented[i] = strings.TrimPrefix(line, pad)
	}

	painted := unindented.paint(lexer)
	for i, line := range painted {
		painted[i] = pad + line
	}

	return painted
}

// paint colors the lines as lexer reads them, one answer for each line. With no lexer, or one that fails, they stay
// plain.
func (ls sourceLines) paint(lexer chroma.Lexer) []string {
	if lexer == nil || len(ls) == 0 {
		return ls
	}

	// Markdown's lexer reads a heading only up to its newline, so the last line needs one too.
	tokens, err := lexer.Tokenise(nil, strings.Join(ls, "\n")+"\n")
	if err != nil {
		return ls
	}

	var painted []string
	for _, line := range chroma.SplitTokensIntoLines(tokens.Tokens()) {
		var b strings.Builder
		if formatters.TTY256.Format(&b, markdownStyle, chroma.Literator(line...)) != nil {
			return ls
		}

		painted = append(painted, strings.ReplaceAll(b.String(), "\n", ""))
	}

	if len(painted) != len(ls) {
		return ls
	}

	return painted
}

// markdownStyle is Chroma's monokai with no background, so no token is drawn on one. Its text is drawn in the
// terminal's own color, which reads on a light terminal too. A top heading gets a color, which monokai leaves out.
var markdownStyle = func() *chroma.Style {
	monokai := styles.Get("monokai")
	plain := monokai.Get(chroma.Text).Colour
	b := chroma.NewStyleBuilder("kiln")
	for _, t := range monokai.Types() {
		e := monokai.Get(t)
		e.Background = 0
		if e.Colour == plain {
			e.Colour = 0
		}

		b.AddEntry(t, e)
	}

	b.AddEntry(chroma.GenericHeading, chroma.StyleEntry{Colour: monokai.Get(chroma.Keyword).Colour, Bold: chroma.Yes})
	s, err := b.Build()
	if err != nil {
		panic(err)
	}

	return s
}()

// wrap breaks a colored line at its spaces to fit width columns. Each row after the first starts past the line's
// first indent columns, under its text. It carries on the colors the row before left open.
func wrap(line string, indent, width int) []string {
	if width-indent < minWrapped {
		indent = 0
	}

	head := ansi.Cut(line, 0, indent)
	rows := strings.Split(ansi.Wrap(ansi.Cut(line, indent, ansi.StringWidth(line)), max(1, width-indent), ""), "\n")
	open := ""
	for i, r := range rows {
		r = open + r
		open = unclosed(r)
		if open != "" {
			r += ansi.ResetStyle
		}

		if i == 0 {
			rows[i] = head + r
		} else {
			rows[i] = strings.Repeat(" ", indent) + r
		}
	}

	return rows
}

// minWrapped is the fewest columns a wrapped row keeps for its text. A deeper indent is not kept.
const minWrapped = 20

var sgr = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// unclosed is the styles s leaves in force at its end: those after its last reset.
func unclosed(s string) string {
	var open []string
	for _, seq := range sgr.FindAllString(s, -1) {
		if seq == ansi.ResetStyle || seq == "\x1b[m" {
			open = nil
		} else {
			open = append(open, seq)
		}
	}

	return strings.Join(open, "")
}
