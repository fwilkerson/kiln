package kiln

import (
	"context"
	"fmt"
	"strconv"
	"strings"
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

// TestWritablePaths holds that a writable command's paths reach a session only when each is absolute. Claude Code
// would read a relative one from where the session runs. A refusal names the path.
func TestWritablePaths(t *testing.T) {
	for command, refused := range map[string]string{
		"echo /tmp/a /tmp/b": "",
		"echo /tmp/a cache":  "cache",
		"echo ./cache":       "./cache",
	} {
		_, err := (&Repo{top: t.TempDir()}).printed(context.Background(), command)
		switch {
		case (err != nil) != (refused != ""):
			t.Errorf("%s: printed answers %v, want refused %q", command, err, refused)
		case err != nil && !strings.Contains(err.Error(), strconv.Quote(refused)):
			t.Errorf("%s: printed answers %v, which does not name %q", command, err, refused)
		}
	}
}
