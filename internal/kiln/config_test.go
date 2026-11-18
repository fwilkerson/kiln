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

// TestHosts holds that kiln.json names each host bare, as the sandbox reads one. A refusal names the host.
func TestHosts(t *testing.T) {
	for host, refused := range map[string]bool{
		"proxy.golang.org":          false,
		"*.example.org":             false,
		"https://proxy.golang.org/": true,
		"proxy.golang.org:443":      true,
	} {
		_, err := parseConfig([]byte(`{"gate": ["true"], "hosts": ["` + host + `"]}`))
		switch {
		case (err != nil) != refused:
			t.Errorf("%s: parseConfig answers %v, want refused %v", host, err, refused)
		case refused && !strings.Contains(err.Error(), strconv.Quote(host)):
			t.Errorf("%s: parseConfig answers %v, which does not name the host", host, err)
		}
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

// TestAddKeys holds that kiln init adds rounds after kiln.json's last key, indented as that key, and keeps each other
// byte.
func TestAddKeys(t *testing.T) {
	for _, tc := range []struct{ name, old, want string }{
		{"one key", "{\n  \"gate\": [\"true\"]\n}\n", "{\n  \"gate\": [\"true\"],\n  \"rounds\": 3\n}\n"},
		{"two keys", "{\"gate\": [\"true\"],\n\t\"hosts\": []}", "{\"gate\": [\"true\"],\n\t\"hosts\": [],\n\t\"rounds\": 3}"},
		{"no key", "{}", `{"rounds": 3}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := addKeys([]byte(tc.old), []byte(`{"rounds": 3}`))
			if err != nil || string(got) != tc.want {
				t.Errorf("addKeys(%q) = %q, %v; want %q", tc.old, got, err, tc.want)
			}
		})
	}
}
