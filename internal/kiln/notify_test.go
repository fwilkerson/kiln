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
// kiln.json. Without notify, kiln prints OSC 9. A program that fails, hangs or is missing changes no result.
func TestNotify(t *testing.T) {
	for name, tc := range map[string]struct {
		notify string
		// wait cuts the run short, standing in for notifyWait, or is 0.
		wait time.Duration
		said string
	}{
		"no notify prints OSC 9":         {said: "\x1b]9;kiln: x stopped\a"},
		"the program reads the notice":   {notify: "printf '%s' \"$KILN_NOTICE\" >.git/notice; echo heard", said: "heard\n"},
		"a program that fails is told":   {notify: "echo broken >&2; exit 3", said: "broken\nx: kiln could not notify: "},
		"a missing program is told":      {notify: ".kiln/missing", said: "x: kiln could not notify: "},
		"a program that hangs is killed": {notify: "sleep 60", wait: time.Second, said: "x: kiln could not notify: "},
	} {
		t.Run(name, func(t *testing.T) {
			b, repo, _ := landingBuild(t)
			if tc.notify != "" {
				commitNotify(t, repo, tc.notify)
			}

			b.stop("the gate still failed after its last round:\nFAIL TestX", nil)
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
			if !claim.Holds(strings.Contains(out.String(), tc.said), strings.Contains(out.String(), "next: "),
				k.find("x").State == stopped, time.Since(start) < 30*time.Second) {
				t.Errorf("x is %s after %s, and kiln printed:\n%q\nwant x stopped, and %q before the summary",
					thenOf(k.find("x")), time.Since(start), out.String(), tc.said)
			}
		})
	}
}

// TestNotice holds the notice a stopped spec's build hands the notify program: its spec, state, why, and next step.
func TestNotice(t *testing.T) {
	b, repo, _ := landingBuild(t)
	commitNotify(t, repo, `printf %s "$KILN_NOTICE" >.git/notice`)
	b.stop("the gate still failed after its last round:\nFAIL TestX", []string{"TestX"})
	b.waits(t.Context())
	raw, err := os.ReadFile(filepath.Join(repo, ".git", "notice"))
	var n map[string]any
	if err == nil {
		err = json.Unmarshal(raw, &n)
	}

	if err != nil {
		t.Fatal(err)
	}

	summary, _ := n["summary"].(string)
	_, committed := n["commit"]
	if !claim.Holds(n["kind"] == "spec", n["name"] == "x", n["state"] == "stopped", n["top"] == b.r.top,
		strings.HasPrefix(summary, "the gate still failed after its last round:\n"),
		strings.Contains(summary, "failing: TestX"), strings.Contains(n["next"].(string), "kiln queue x"), !committed) {
		t.Errorf("the notice is %s; want spec x stopped, at %s, why and what failed, the next step, and no commit",
			raw, b.r.top)
	}
}

// commitNotify commits to main, in repo, a kiln.json whose notify is command.
func commitNotify(t *testing.T, repo, command string) {
	raw, err := json.Marshal(map[string]any{"gate": []string{"true"}, "rounds": 1, "notify": command})
	if err == nil {
		err = os.WriteFile(filepath.Join(repo, configFile), raw, 0o644)
	}

	if err != nil {
		t.Fatal(err)
	}

	shell(t, repo, "git commit --quiet -am notify")
}
