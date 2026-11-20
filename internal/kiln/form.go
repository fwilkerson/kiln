package kiln

import (
	_ "embed"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// template is /shape's template, as kiln embeds it. A project's own copy does not change the form kiln checks.
//
//go:embed defaults/skills/shape/template.md
var template string

// templateSections are the template's ## sections, in its order.
var templateSections = sections(template)

const testsSection = "Acceptance tests"

// section is a ## section of a spec: its title, and the lines under it.
type section struct {
	title string
	lines []string
}

// sections reads the ## sections of Markdown text, outside its fenced blocks. Text above the first one is in none.
func sections(text string) []section {
	var all []section
	fenced := false
	for line := range strings.Lines(text) {
		line = strings.TrimRight(line, "\r\n")
		if isFence(line) {
			fenced = !fenced
		}

		title, ok := strings.CutPrefix(line, "## ")
		switch {
		case ok && !fenced:
			all = append(all, section{title: strings.TrimSpace(title)})
		case len(all) > 0:
			all[len(all)-1].lines = append(all[len(all)-1].lines, line)
		}
	}

	return all
}

func isFence(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

// formOf refuses a spec that leaves the template's form, for each way it leaves it. Each of the template's ##
// sections appears in the spec, in the template's order. The spec may add sections of its own between them. Each ###
// under Acceptance tests is "### N. Title", numbered from 1, and holds a Given, a When and a Then, in that order.
func formOf(spec string) refusals {
	have := sections(spec)
	var refused refusals
	for _, s := range have {
		if s.title == testsSection {
			refused = append(refused, testsForm(s.lines)...)
		}
	}

	// Each section is sought after the last one found, so a section out of order reads as missing.
	from := 0
	for i, want := range templateSections {
		at := slices.IndexFunc(have[from:], func(s section) bool { return s.title == want.title })
		if at < 0 {
			refused = append(refused, fmt.Sprintf("it lacks the section ## %s, or holds it out of the "+
				"template's order: put it %s", want.title, placeOf(i)))
			continue
		}

		from += at + 1
	}

	return refused
}

// placeOf says where the template's section i goes, by the section before it.
func placeOf(i int) string {
	if i == 0 {
		return "first, under the title"
	}

	return "after ## " + templateSections[i-1].title
}

var (
	testHeading = regexp.MustCompile(`^### (\d+)\. \S`)
	clauses     = []string{"Given", "When", "Then"}
)

// testsForm refuses the lines of an Acceptance tests section that leave its form, for each way they leave it. Each
// test is numbered in turn from 1, and holds a Given, a When and a Then.
func testsForm(lines []string) refusals {
	tests, refused := splitTests(lines)
	if len(tests) == 0 {
		return append(refused, "it holds no acceptance test: write one, as ### 1. and a title, with a Given, a "+
			"When and a Then")
	}

	for i, test := range tests {
		if why := clausesOf(test); why != "" {
			refused = append(refused, fmt.Sprintf("its acceptance test %d %s", i+1, why))
		}
	}

	return refused
}

// splitTests returns the lines of each test in an Acceptance tests section. A ### heading outside a code fence starts
// a test. It refuses each heading that is not the next test's number and a title, and counts it as that test.
func splitTests(lines []string) ([][]string, refusals) {
	var tests [][]string
	var refused refusals
	fenced := false
	for _, line := range lines {
		if isFence(line) {
			fenced = !fenced
		}

		heading, ok := strings.CutPrefix(line, "### ")
		if !ok || fenced {
			if len(tests) > 0 {
				tests[len(tests)-1] = append(tests[len(tests)-1], line)
			}

			continue
		}

		if m := testHeading.FindStringSubmatch(line); m == nil || m[1] != strconv.Itoa(len(tests)+1) {
			refused = append(refused, fmt.Sprintf("its acceptance test %q is not ### %d. and a title: number "+
				"it %d, or add a test at the end in place of a renumbered one", heading, len(tests)+1, len(tests)+1))
		}

		tests = append(tests, nil)
	}

	return tests, refused
}

// clausesOf says how a test's lines leave the form, or "". They hold a Given, a When and a Then, in that order.
func clausesOf(lines []string) string {
	next := 0
	for _, line := range lines {
		if next < len(clauses) && strings.HasPrefix(line, "- **"+clauses[next]+"**:") {
			next++
		}
	}

	if next < len(clauses) {
		return fmt.Sprintf("holds no %s, after the clauses before it: write it as - **%s**: on a line of its own",
			clauses[next], clauses[next])
	}

	return ""
}
