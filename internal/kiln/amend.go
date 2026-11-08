package kiln

import (
	"errors"
	"fmt"
	"strings"
)

const amendSuffix = ".amend.md"

func specPath(spec string) string  { return "specs/" + spec + ".md" }
func amendPath(spec string) string { return "specs/" + spec + amendSuffix }

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
		title, body, _ := strings.Cut(section, "\n")
		old, rest, okOld := strings.Cut(after(body, oldHeading), newHeading)
		replacement, reason, okNew := strings.Cut(rest, reasonHeading)
		old, replacement = strings.TrimSpace(old), strings.TrimSpace(replacement)
		switch {
		case !okOld || !okNew || old == "" || replacement == "" || strings.TrimSpace(reason) == "":
			return "", fmt.Errorf("the amendment of %q lacks its %s, %s or %s", title, oldHeading, newHeading,
				reasonHeading)
		case strings.Count(spec, old) != 1:
			return "", fmt.Errorf("the spec holds the old wording of %q %d times, not once", title,
				strings.Count(spec, old))
		}

		spec = strings.Replace(spec, old, replacement, 1)
	}

	return spec, nil
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
