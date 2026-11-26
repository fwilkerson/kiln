package kiln

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fwilkerson/practices/claim"
)

// TestNotify holds that a spec's build that waits on the developer hands its notice to the notify of main's
// kiln.json. kiln prints OSC 9 without notify, and in place of a program that fails, hangs or is missing. Neither
// changes the build's result.
func TestNotify(t *testing.T) {
	t.Parallel()
	const osc = "\x1b]9;kiln: x stopped\a"
	for name, tc := range map[string]struct {
		// config is main's kiln.json, or "" for one with no notify.
		config string
		// wait cuts the run short, standing in for notifyWait, or is 0.
		wait time.Duration
		said string
		// printed is whether kiln printed OSC 9.
		printed bool
	}{
		"no notify prints OSC 9": {said: osc, printed: true},
		"the program reads the notice": {config: withNotify(`printf '%s' "$KILN_NOTICE" >.git/notice; echo heard`),
			said: "heard\n"},
		"a program that fails is told": {config: withNotify("echo broken >&2; exit 3"),
			said: "broken\nx: kiln could not notify: ", printed: true},
		"a missing program is told": {config: withNotify(".kiln/missing"), said: "x: kiln could not notify: ",
			printed: true},
		"a program that hangs is killed": {config: withNotify("sleep 60"), wait: time.Second,
			said: "x: kiln could not notify: ", printed: true},
		"an unreadable kiln.json is told": {config: `{"gate": ["true"], "rounds": 1, "notfy": "true"}`,
			said: "x: kiln could not notify: ", printed: true},
	} {
		t.Run(name, func(t *testing.T) {
			b, repo, _ := landingBuild(t)
			if tc.config != "" {
				commitConfig(t, repo, tc.config)
			}

			b.stop(stop{Why: "the gate still failed after its last round:\nFAIL TestX"})
			var out strings.Builder
			b.out = &out
			ctx := t.Context()
			if tc.wait > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.wait)
				defer cancel()
			}

			start := time.Now()
			b.waits(ctx)
			k, _ := b.r.store.read()
			said := out.String()
			if !claim.Holds(strings.Contains(said, tc.said), strings.Contains(said, osc) == tc.printed,
				strings.Contains(said, "next: "), k.find("x").State == stopped, time.Since(start) < 30*time.Second) {
				t.Errorf("x is %s after %s, and kiln printed:\n%q\nwant x stopped, %q before the summary, and "+
					"OSC 9 printed %v", thenOf(k.find("x")), time.Since(start), said, tc.said, tc.printed)
			}
		})
	}
}

// TestNotice holds the notice a stopped spec's build hands the notify program: its spec, state, why, and next step.
func TestNotice(t *testing.T) {
	b, repo, _ := landingBuild(t)
	commitConfig(t, repo, withNotify(`printf %s "$KILN_NOTICE" >.git/notice && printf %s "$KILN" >.git/kiln-path`))
	b.stop(stop{Why: "the gate still failed after its last round:\nFAIL TestX", Failing: []string{"TestX"}})
	b.waits(t.Context())
	raw, err := os.ReadFile(filepath.Join(repo, ".git", "notice"))
	var n map[string]any
	if err == nil {
		err = json.Unmarshal(raw, &n)
	}

	kiln, _ := os.ReadFile(filepath.Join(repo, ".git", "kiln-path"))

	if err != nil {
		t.Fatal(err)
	}

	summary, _ := n["summary"].(string)
	_, committed := n["commit"]
	if !claim.Holds(n["kind"] == "spec", n["name"] == "x", n["state"] == "stopped", n["top"] == b.r.top,
		string(kiln) == b.r.exe, strings.HasPrefix(summary, "the gate still failed after its last round:\n"),
		strings.Contains(summary, "failing: TestX"), strings.Contains(n["next"].(string), "run kiln tend x"),
		strings.Contains(n["next"].(string), "kiln queue x"), !committed) {
		t.Errorf("the notice is %s, and $KILN %q; want spec x stopped, at %s, why and what failed, the next step, "+
			"no commit, and this kiln in $KILN", raw, kiln, b.r.top)
	}
}

// withNotify is a kiln.json whose notify is command.
func withNotify(command string) string {
	raw, _ := json.Marshal(map[string]any{"gate": []string{"true"}, "rounds": 1, "notify": command})
	return string(raw)
}

// commitConfig commits config to main, in repo, as its kiln.json.
func commitConfig(t *testing.T, repo, config string) {
	if err := os.WriteFile(filepath.Join(repo, configFile), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	shell(t, repo, "git commit --quiet -am config")
}
