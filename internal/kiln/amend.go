package kiln

import (
	"errors"
	"fmt"
	"strings"
)

const amendSuffix = ".amend.md"

// An amendment file holds a section for each Given, When, Then a build changed, as the build role tells it:
//
//	## Acceptance test 3: Main moves only to work gated on it
//
//	### Old
//
//	- **Given**: ...
//
//	### New
//
//	- **Given**: ...
//
//	### Reason
//
//	...
const (
	oldHeading    = "### Old"
	newHeading    = "### New"
	reasonHeading = "### Reason"
)

// foldAmendments returns the spec with each amendment's old wording replaced by its new. It refuses an amendment that
// lacks its old wording, its new or its reason. It refuses one whose old wording the spec does not hold once.
func foldAmendments(spec, amendments string) (string, error) {
	sections := strings.Split("\n"+amendments, "\n## ")[1:]
	if len(sections) == 0 {
		return "", errors.New("the amendments name no acceptance test: each opens with a line that starts \"## \"")
	}

	for _, section := range sections {
		a, err := parseAmendment(section)
		if err != nil {
			return "", err
		}

		if n := strings.Count(spec, a.old); n != 1 {
			return "", fmt.Errorf("the spec holds the old wording of %q %d times, not once", a.title, n)
		}

		spec = strings.Replace(spec, a.old, a.replacement, 1)
	}

	return spec, nil
}

// amendment is one section of an amendment file: the acceptance test it names, its old wording and its new.
type amendment struct {
	title, old, replacement string
}

// parseAmendment reads one section of an amendment file. It refuses one that lacks its old wording, its new or its
// reason.
func parseAmendment(section string) (amendment, error) {
	title, body, _ := strings.Cut(section, "\n")
	old, rest, okOld := strings.Cut(after(body, oldHeading), newHeading)
	replacement, reason, okNew := strings.Cut(rest, reasonHeading)
	a := amendment{title: title, old: strings.TrimSpace(old), replacement: strings.TrimSpace(replacement)}
	switch {
	case !okOld, !okNew, a.old == "", a.replacement == "", strings.TrimSpace(reason) == "":
		return a, fmt.Errorf("the amendment of %q lacks its %s, %s or %s", title, oldHeading, newHeading,
			reasonHeading)
	}

	return a, nil
}

// after returns what follows the first line of s that is heading, or "" if none is.
func after(s, heading string) string {
	for i := 0; i < len(s); {
		line, rest, _ := strings.Cut(s[i:], "\n")
		if strings.TrimSpace(line) == heading {
			return rest
		}

		i += len(line) + 1
	}

	return ""
}
