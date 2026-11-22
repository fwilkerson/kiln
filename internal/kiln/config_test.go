package kiln

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestAcceptanceForms holds that kiln.json's acceptanceTests reads as a list, and a string as a list of one. An old
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
			c, err := parseConfig([]byte(`{"gate": ["true"], "rounds": 1, "acceptanceTests": ` + tc.acceptance + `}`))
			switch got := fmt.Sprint(c.acceptancePaths("x")); {
			case (err != nil) != tc.refused:
				t.Errorf("refused %v, want %v", err, tc.refused)
			case !tc.refused && got != tc.want:
				t.Errorf("the paths are %s, want %s", got, tc.want)
			}
		})
	}
}

// TestConfigWords holds how kiln words a kiln.json it cannot read, as its own refusal with one fix. It names a key it
// does not read, with the nearest it does. It names a syntax error's line and column, and the type a key takes.
func TestConfigWords(t *testing.T) {
	for name, tc := range map[string]struct{ config, said string }{
		"a key misspelled": {config: `{"gate": ["true"], "rouns": 3}`,
			said: `.kiln/kiln.json holds "rouns", a key kiln does not read: name it "rounds"`},
		"a key in other letters": {config: `{"Gate": ["true"]}`,
			said: `.kiln/kiln.json holds "Gate", a key kiln does not read: name it "gate"`},
		"a key near none": {config: `{"gate": ["true"], "colour": "red"}`,
			said: `.kiln/kiln.json holds "colour", a key kiln does not read: remove it, or name one kiln reads: ` +
				`$schema, gate, rounds, allowedHosts, allowUnixSockets, writablePathCommands, mcpServers, ` +
				`verifySession, acceptanceTests, testHelpers, testFiles, reaudit, notify, timeout`},
		"a trailing comma": {config: "{\n  \"gate\": [\"true\"],\n}",
			said: ".kiln/kiln.json is no JSON at line 2, column 19: a comma with nothing after it: fix its JSON there"},
		"a number as a string": {config: `{"gate": ["true"], "rounds": "3"}`,
			said: ".kiln/kiln.json's rounds is a string: make it a whole number, such as 3"},
		"a string where a list goes": {config: `{"gate": "go test ./..."}`,
			said: `.kiln/kiln.json's gate is a string: make it a list of strings, such as ["go test ./..."]`},
		"a timeout of zero": {config: `{"gate": ["true"], "timeout": "0s"}`,
			said: `.kiln/kiln.json names the timeout "0s", which is no Go duration above zero: name one such as "2h", ` +
				`or none for 1h`},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseConfig([]byte(tc.config)); fmt.Sprint(err) != tc.said {
				t.Errorf("parseConfig answers:\n%v\nwant:\n%s", err, tc.said)
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
		_, err := parseConfig([]byte(`{"gate": ["true"], "allowedHosts": ["` + host + `"]}`))
		switch {
		case (err != nil) != refused:
			t.Errorf("%s: parseConfig answers %v, want refused %v", host, err, refused)
		case refused && !strings.Contains(err.Error(), strconv.Quote(host)):
			t.Errorf("%s: parseConfig answers %v, which does not name the host", host, err)
		}
	}
}

// TestTestFilePatterns holds that kiln.json names each test file pattern as one path element that path.Match reads.
// A refusal names the pattern.
func TestTestFilePatterns(t *testing.T) {
	for pattern, refused := range map[string]bool{
		"*_test.go":    false,
		"testdata":     false,
		"[":            true,
		"uat/testdata": true,
		"":             true,
	} {
		_, err := parseConfig([]byte(`{"gate": ["true"], "testFiles": ["` + pattern + `"]}`))
		switch {
		case (err != nil) != refused:
			t.Errorf("%q: parseConfig answers %v, want refused %v", pattern, err, refused)
		case refused && !strings.Contains(err.Error(), strconv.Quote(pattern)):
			t.Errorf("%q: parseConfig answers %v, which does not name the pattern", pattern, err)
		}
	}
}

// TestTimeout holds what kiln.json may name as its timeout: a Go duration above zero, or none for an hour. A refusal
// names the value.
func TestTimeout(t *testing.T) {
	for name, tc := range map[string]struct {
		key  string
		want time.Duration
	}{
		"none":         {want: time.Hour},
		"two hours":    {key: `, "timeout": "2h"`, want: 2 * time.Hour},
		"zero":         {key: `, "timeout": "0s"`},
		"below zero":   {key: `, "timeout": "-1h"`},
		"no unit":      {key: `, "timeout": "7200"`},
		"a bare count": {key: `, "timeout": 7200`},
	} {
		c, err := parseConfig([]byte(`{"gate": ["true"]` + tc.key + `}`))
		switch refused := tc.want == 0; {
		case (err != nil) != refused:
			t.Errorf("%s: parseConfig answers %v, want refused %v", name, err, refused)
		case refused && !strings.Contains(err.Error(), "timeout"):
			t.Errorf("%s: parseConfig answers %v, which does not name the timeout", name, err)
		case !refused && c.killAfter() != tc.want:
			t.Errorf("%s: kiln kills after %v, want %v", name, c.killAfter(), tc.want)
		}
	}
}

// TestWritablePaths holds that the paths a command of writablePathCommands prints reach a session only when each is
// absolute. Claude Code would read a relative one from where the session runs. A refusal names the path.
func TestWritablePaths(t *testing.T) {
	for command, refused := range map[string]string{
		"echo /tmp/a /tmp/b": "",
		"echo /tmp/a cache":  "cache",
		"echo ./cache":       "./cache",
	} {
		_, err := (&Repo{top: t.TempDir()}).printed(context.Background(), config{}.shell(nil), command)
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
		{"two keys", "{\"gate\": [\"true\"],\n\t\"allowedHosts\": []}", "{\"gate\": [\"true\"],\n\t\"allowedHosts\": [],\n\t\"rounds\": 3}"},
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
