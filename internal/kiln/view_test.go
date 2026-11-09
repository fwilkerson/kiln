package kiln

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/fwilkerson/kiln/internal/tui"
)

// TestView holds what the view reads from the book and the ledger. A spec the book no longer holds was accepted. A
// build, and a run with no end line, runs only while the kiln run that started it lives. An interrupt ends the run
// but leaves the build building. Specs come newest build first, after any
// with no build yet. Each run says why it started in the words of the spec's Terms.
func TestView(t *testing.T) {
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}

	r := &Repo{store: store{dir: t.TempDir()}}
	err := r.store.update(func(b *book) error {
		b.Specs = []*record{{Name: "live", State: building, Build: "b-live", PID: os.Getpid()},
			{Name: "gone", State: building, Build: "b-gone", PID: dead.Process.Pid},
			{Name: "lost", State: building, Build: "b-lost", PID: dead.Process.Pid},
			{Name: "next", State: queued, Build: "b-next"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	at := time.Now().Add(-time.Hour)
	line := func(build string, e entry) {
		at = at.Add(time.Minute)
		e.Build, e.At = build, at
		if err := r.store.write(e); err != nil {
			t.Fatal(err)
		}
	}

	run := func(build string, why reason, pid int, ended bool) {
		line(build, entry{Kind: startKind, Role: "build", Session: "s-" + build, PID: pid, reason: why})
		if ended {
			line(build, entry{Kind: endKind, Session: "s-" + build, Ended: endedResult})
		}
	}

	line("b-done", entry{Kind: builtKind, Spec: "done"})
	for _, why := range []reason{{Why: whyFirst}, {Why: whyGate, Round: 1, Rounds: 3}, {Why: whyAudit, Try: 2},
		{Why: whyFindings}, {Why: whyConflict, Round: 2, Rounds: 3}, {Why: whyLanding, Round: 3, Rounds: 3},
		{Why: whyLimit}, {Why: whyInterrupted}, {}} {
		run("b-done", why, os.Getpid(), true)
	}

	line("b-done", entry{Kind: outcomeKind, Outcome: outAccepted})
	line("b-gone", entry{Kind: builtKind, Spec: "gone"})
	line("b-lost", entry{Kind: builtKind, Spec: "lost"})
	run("b-lost", reason{Why: whyFirst}, dead.Process.Pid, false)
	run("b-gone", reason{Why: whyFirst}, dead.Process.Pid, true)
	line("b-live", entry{Kind: builtKind, Spec: "live"})
	run("b-live", reason{Why: whyFirst}, os.Getpid(), true)
	line("b-live", entry{Kind: gateKind, End: at, Passed: false})
	run("b-live", reason{Why: whyGate, Round: 1, Rounds: 3}, os.Getpid(), false)

	specs, err := view{r}.Specs(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, s := range specs {
		var steps []string
		for _, b := range s.Builds {
			for _, step := range b.Steps {
				steps = append(steps, shownStep(step))
			}
		}

		got = append(got, fmt.Sprintf("%s %s: %s", s.Name, s.State, strings.Join(steps, ", ")))
	}

	want := []string{
		"next queued: ",
		"live running: first run ended, gate failed, round 1 of 3 running",
		"lost halted: first run lost",
		"gone halted: first run ended",
		"done accepted: first run ended, round 1 of 3 ended, audit, try 2 ended, audit's findings ended, " +
			"conflict with main, round 2 of 3 ended, landing gate failed, round 3 of 3 ended, after a usage limit " +
			"ended, after kiln stopped ended, why unknown ended",
	}
	if g, w := strings.Join(got, "\n"), strings.Join(want, "\n"); g != w {
		t.Errorf("the view reads\n%s\nwant\n%s", g, w)
	}
}

func shownStep(step tui.Step) string {
	switch s := step.(type) {
	case *tui.Gate:
		return map[bool]string{true: "gate passed", false: "gate failed"}[s.Passed]
	case *tui.Run:
		switch {
		case s.Lost:
			return s.Why + " lost"
		case s.End.IsZero() && s.To == -1:
			return s.Why + " running"
		}

		return s.Why + " ended"
	}

	return "?"
}
