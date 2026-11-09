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
	source := strings.Split(strings.ReplaceAll(strings.TrimRight(text, "\n"), "\t", "    "), "\n")
	colored := highlight(source)
	var lines []string
	fenced := false
	for i, line := range source {
		trimmed := strings.TrimSpace(line)
		fence := isFence(trimmed)
		if fenced || fence || strings.HasPrefix(trimmed, "|") || ansi.StringWidth(line) <= width {
			lines = append(lines, colored[i])
		} else {
			lines = append(lines, wrap(colored[i], len(indent.FindString(line)), width)...)
		}

		if fence {
			fenced = !fenced
		}
	}

	return lines
}

// indent is a line's indent, with its list item's marker if it has one.
var indent = regexp.MustCompile(`^ *(?:(?:[-*+]|\d+[.)]) +)?`)

// isFence reports whether a line, its indent trimmed, opens or closes a fenced block.
func isFence(trimmed string) bool {
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

// highlight colors each line of Markdown source by its syntax, and the code in each fence by the fence's language.
// It colors prose a line at a time. So no lexer state, such as an unclosed HTML tag, reaches the next line. A fence
// inside a list item counts too: its code is colored without its indent. Each line it answers has its colors closed.
func highlight(source []string) []string {
	markdown := lexers.Get("markdown")
	var colored []string
	for i := 0; i < len(source); i++ {
		trimmed := strings.TrimSpace(source[i])
		colored = append(colored, paint([]string{source[i]}, markdown)...)
		if !isFence(trimmed) {
			continue
		}

		pad := source[i][:len(source[i])-len(strings.TrimLeft(source[i], " "))]
		var lexer chroma.Lexer
		if lang := strings.Fields(strings.TrimLeft(trimmed, "`~")); len(lang) > 0 {
			lexer = lexers.Get(lang[0])
		}

		end := i + 1
		for end < len(source) && !isFence(strings.TrimSpace(source[end])) {
			end++
		}

		var code []string
		for _, line := range source[i+1 : end] {
			code = append(code, strings.TrimPrefix(line, pad))
		}

		for _, line := range paint(code, lexer) {
			colored = append(colored, pad+line)
		}

		if end < len(source) {
			colored = append(colored, paint([]string{source[end]}, markdown)...)
		}

		i = end
	}

	return colored
}

// paint colors lines as lexer reads them, one answer for each line. With no lexer, or one that fails, they stay
// plain.
func paint(lines []string, lexer chroma.Lexer) []string {
	if lexer == nil || len(lines) == 0 {
		return lines
	}

	// Markdown's lexer reads a heading only up to its newline, so the last line needs one too.
	tokens, err := lexer.Tokenise(nil, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return lines
	}

	var painted []string
	for _, line := range chroma.SplitTokensIntoLines(tokens.Tokens()) {
		var b strings.Builder
		if formatters.TTY256.Format(&b, markdownStyle, chroma.Literator(line...)) != nil {
			return lines
		}

		painted = append(painted, strings.ReplaceAll(b.String(), "\n", ""))
	}

	if len(painted) != len(lines) {
		return lines
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
