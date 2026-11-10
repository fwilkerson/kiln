package kiln

import (
	_ "embed"
	"fmt"
	"regexp"
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

// formOf refuses a spec that leaves the template's form. Each of the template's ## sections appears in the spec, in
// the template's order. The spec may add sections of its own between them. Each ### under Acceptance tests is
// "### N. Title", numbered from 1, and holds a Given, a When and a Then, in that order.
func formOf(spec string) error {
	have := sections(spec)
	next := 0
	for _, s := range have {
		if next < len(templateSections) && s.title == templateSections[next].title {
			next++
		}

		if s.title == testsSection {
			if err := testsForm(s.lines); err != nil {
				return err
			}
		}
	}

	if next < len(templateSections) {
		return fmt.Errorf("it lacks the section ## %s, or holds it out of the template's order",
			templateSections[next].title)
	}

	return nil
}

var (
	testHeading = regexp.MustCompile(`^### (\d+)\. \S`)
	clauses     = []string{"Given", "When", "Then"}
)

// testsForm refuses the lines of an Acceptance tests section that leave its form. Each test is numbered in turn from
// 1, and holds a Given, a When and a Then.
func testsForm(lines []string) error {
	var tests [][]string
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

		m := testHeading.FindStringSubmatch(line)
		if m == nil || m[1] != strconv.Itoa(len(tests)+1) {
			return fmt.Errorf("its acceptance test %q is not ### %d. and a title", heading, len(tests)+1)
		}

		tests = append(tests, nil)
	}

	if len(tests) == 0 {
		return fmt.Errorf("it holds no acceptance test")
	}

	for i, test := range tests {
		if err := clausesOf(test); err != nil {
			return fmt.Errorf("its acceptance test %d %w", i+1, err)
		}
	}

	return nil
}

// clausesOf refuses a test's lines unless they hold a Given, a When and a Then, in that order.
func clausesOf(lines []string) error {
	next := 0
	for _, line := range lines {
		if next < len(clauses) && strings.HasPrefix(line, "- **"+clauses[next]+"**:") {
			next++
		}
	}

	if next < len(clauses) {
		return fmt.Errorf("holds no %s, after the clauses before it", clauses[next])
	}

	return nil
}
