package kiln

import (
	"fmt"
	"testing"
)

// TestAcceptanceForms holds that kiln.json's acceptance reads as a list, and a string as a list of one. An old
// commit holds the string.
func TestAcceptanceForms(t *testing.T) {
	for name, tc := range map[string]struct {
		acceptance, want string
		refused          bool
	}{
		"a string": {acceptance: `"uat/{spec}/"`, want: "[uat/x/]"},
		"a list":   {acceptance: `["uat/{spec}/", "integration/{spec}/"]`, want: "[uat/x/ integration/x/]"},
		"neither":  {acceptance: `{"path": "uat/{spec}/"}`, refused: true},
		"numbers":  {acceptance: `[1]`, refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			c, err := parseConfig([]byte(`{"gate": ["true"], "rounds": 1, "acceptance": ` + tc.acceptance + `}`))
			switch got := fmt.Sprint(c.acceptancePaths("x")); {
			case (err != nil) != tc.refused:
				t.Errorf("refused %v, want %v", err, tc.refused)
			case !tc.refused && got != tc.want:
				t.Errorf("the paths are %s, want %s", got, tc.want)
			}
		})
	}
}
