package kiln

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/fwilkerson/kiln/internal/tui"
	"github.com/fwilkerson/practices/claim"
)

// TestView holds what the view reads from the book and the ledger. A spec the book no longer holds was accepted, or
// rejected when its last build was dropped. A
// build, and a run with no end line, runs only while the kiln fire that started it lives. An interrupt ends the run
// but leaves the build building. Specs come newest build first, after any
// with no build yet. Each run says why it started in the words of the spec's Terms. A build's gauge lines are its
// readings, each judged against the line it holds. A build's sums come from the ledger too.
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
	seven, five := 7.0, 5.0
	line("b-done", entry{Kind: gaugeKind, Gauge: "files", Value: &seven, Max: &five})
	line("b-done", entry{Kind: gaugeKind, Gauge: "score", Min: &five})
	line("b-turned", entry{Kind: builtKind, Spec: "turned"})
	line("b-turned", entry{Kind: outcomeKind, Outcome: outWaits})
	line("b-turned", entry{Kind: outcomeKind, Outcome: outDropped})
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

	got := shownSpecs(specs)
	want := []string{
		"next queued: ",
		"live running: first run ended, gate failed, round 1 of 3 running",
		"lost halted: first run lost",
		"gone halted: first run ended",
		"turned rejected: ",
		"done accepted: first run ended, round 1 of 3 ended, audit, try 2 ended, audit's findings ended, " +
			"conflict with main, round 2 of 3 ended, landing gate failed, round 3 of 3 ended, after a usage limit " +
			"ended, after kiln stopped ended, ? ended",
	}
	if g, w := strings.Join(got, "\n"), strings.Join(want, "\n"); g != w {
		t.Errorf("the view reads\n%s\nwant\n%s", g, w)
	}

	// One of the done build's runs gives no reason, so its rounds are unknown. Each of its runs ended.
	if done := specs[len(specs)-1].Builds[0]; done.Rounds != nil || done.Took == nil {
		t.Errorf("the done build's rounds are %v and its time %v, want unknown rounds and a known time", done.Rounds,
			done.Took)
	}

	readings := shownReadings(specs[len(specs)-1].Builds[0].Readings)
	wantRead := "files 7 ≤ 5 crossed true, at true\nscore ? ≥ 5 crossed false, at true"
	if g, w := strings.Join(readings, "\n"), wantRead; g != w {
		t.Errorf("the done build's readings are\n%s\nwant\n%s", g, w)
	}
}

// shownSpecs words each spec as a line: its name, its state, and each step of its builds.
func shownSpecs(specs []tui.Spec) []string {
	var shown []string
	for _, s := range specs {
		var steps []string
		for _, b := range s.Builds {
			for _, step := range b.Steps {
				steps = append(steps, shownStep(step))
			}
		}

		shown = append(shown, fmt.Sprintf("%s %s: %s", s.Name, s.State, strings.Join(steps, ", ")))
	}

	return shown
}

// shownReadings words each reading as a line of its gauge, value, line, crossing, and whether it has a time.
func shownReadings(readings []tui.Reading) []string {
	var shown []string
	for _, rd := range readings {
		value := "?"
		if rd.Value != nil {
			value = fmt.Sprint(*rd.Value)
		}

		shown = append(shown, fmt.Sprintf("%s %s %s crossed %v, at %v", rd.Gauge, value, rd.Line, rd.Crossed,
			!rd.At.IsZero()))
	}

	return shown
}

func shownStep(step tui.Step) string {
	switch s := step.(type) {
	case *tui.Gate:
		return map[bool]string{true: "gate passed", false: "gate failed"}[s.Passed]
	case *tui.Run:
		why := cmp.Or(s.Why, "?")
		switch {
		case s.Lost:
			return why + " lost"
		case s.End.IsZero() && s.To == -1:
			return why + " running"
		}

		return why + " ended"
	}

	return "?"
}

// TestBeforeSpec3 holds how the view reads lines kiln wrote before spec 3, whose end lines hold no stream length. A
// run's bytes then end where its session's next run starts, or at the stream's end.
func TestBeforeSpec3(t *testing.T) {
	r := &Repo{store: store{dir: t.TempDir()}}
	at := time.Now().Add(-time.Hour)
	for _, e := range []entry{
		{Kind: builtKind, Spec: "old"},
		{Kind: startKind, Role: "build", Session: "build"},
		{Kind: endKind, Session: "build", Ended: endedResult},
		{Kind: startKind, Role: "audit", Session: "audit"},
		{Kind: endKind, Session: "audit", Ended: endedResult},
		{Kind: startKind, Role: "build", Session: "build", Streamed: 3000},
		{Kind: endKind, Session: "build", Ended: endedResult},
		{Kind: outcomeKind, Outcome: outLanded},
	} {
		at = at.Add(time.Minute)
		e.Build, e.At = "b-old", at
		if err := r.store.write(e); err != nil {
			t.Fatal(err)
		}
	}

	specs, err := view{r}.Specs(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, step := range specs[0].Builds[0].Steps {
		if run, ok := step.(*tui.Run); ok {
			got = append(got, fmt.Sprintf("%s %d to %d", run.Role, run.From, run.To))
		}
	}

	if g, w := strings.Join(got, ", "), "build 0 to 3000, audit 0 to -1, build 3000 to -1"; g != w {
		t.Errorf("the runs' bytes are %s, want %s", g, w)
	}
}

// TestStateOf holds where the view says a spec stands. A pause that a kiln fire took ends with that run, so the next
// kiln fire's build runs. An accepted spec that waits for the kiln fire building now is queued, not halted.
func TestStateOf(t *testing.T) {
	for name, tc := range map[string]struct {
		given record
		// next is true when a kiln fire, this test's process, takes the spec from the queue.
		next bool
		want tui.State
	}{
		"a build paused by its own kiln fire is paused": {given: record{State: building, Session: "s",
			runner: runner{PID: os.Getpid(), Paused: time.Now().Add(time.Hour)}}, want: tui.Paused},
		"a pause ends with the kiln fire that took it": {given: record{State: queued, Session: "s",
			runner: runner{Paused: time.Now().Add(-time.Hour)}}, next: true, want: tui.Running},
		"an accepted spec waits for the kiln fire building now": {given: record{State: queued, Session: "s",
			Accepted: true}, want: tui.Queued},
	} {
		t.Run(name, func(t *testing.T) {
			rec := tc.given
			rec.Name = "x"
			b := book{Specs: []*record{&rec}}
			if tc.next {
				b.next(specsLane, "", os.Getpid())
			}

			if got := stateOf(&rec); got != tc.want {
				t.Errorf("the view says %s, want %s", got, tc.want)
			}
		})
	}
}

// TestMonitorReadings holds that the view reads a monitor's readings of its gauge from its monitor lines, oldest
// first. A line from before the monitor named the gauge is no reading, nor is one of another monitor.
func TestMonitorReadings(t *testing.T) {
	r, repo := scratchRepo(t)
	config := `{"gate": ["true"], "rounds": 1, "acceptance": "acceptance/{spec}/"}`
	shell(t, repo, "mkdir .kiln && echo '"+config+"' >.kiln/kiln.json")
	writeMonitor(t, repo, "m", map[string]string{"gauge": "echo 0", "monitor.json": `{"max": 0}`, "command": "true"})
	writeMonitor(t, repo, "other", map[string]string{"command": "true"})
	shell(t, repo, "git add . && git commit --quiet -m start")
	at, zero, two := time.Now().Add(-time.Hour), 0.0, 2.0
	for i, e := range []entry{
		{Monitor: "m", Found: foundHealthy},
		{Monitor: "m", Found: foundFix, Gauge: "m", Value: &two},
		{Monitor: "other", Found: foundHealthy, Gauge: "m", Value: &zero},
		{Monitor: "m", Found: foundCouldNot, Gauge: "m"},
		{Monitor: "m", Found: foundHealthy, Gauge: "m", Value: &zero},
	} {
		e.Kind, e.At, e.Max = monitorKind, at.Add(time.Duration(i)*time.Minute), &zero
		if err := r.store.write(e); err != nil {
			t.Fatal(err)
		}
	}

	monitors, err := view{r}.Monitors(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	var read []string
	for _, rd := range monitors[0].Monitor.Readings {
		read = append(read, fmt.Sprintf("%s %s %s crossed %v", rd.Gauge, valueOf(rd.Value), rd.Line, rd.Crossed))
	}

	want := "m 2 ≤ 0 crossed true\nm ? ≤ 0 crossed false\nm 0 ≤ 0 crossed false"
	if got := strings.Join(read, "\n"); got != want || len(monitors[1].Monitor.Readings) > 0 {
		t.Errorf("m's readings are\n%s\nwant\n%s\nand other's are %v, want none", got, want,
			monitors[1].Monitor.Readings)
	}
}

// TestCutShortFix holds that a fix whose kiln is gone stands halted, in the view and in kiln status. Status names
// kiln monitor run, which drops the fix and runs the monitor again, or lands an accepted fix.
func TestCutShortFix(t *testing.T) {
	for name, tc := range map[string]struct {
		accepted bool
		next     string
	}{
		"a fix":                     {next: "next: run kiln monitor run m, which drops the fix"},
		"an accepted fix's landing": {accepted: true, next: "next: run kiln monitor run m, which lands it"},
	} {
		t.Run(name, func(t *testing.T) {
			r := cutShortFix(t, tc.accepted)
			monitors, err := view{r}.Monitors(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			var status strings.Builder
			if err := r.monitorStatus(t.Context(), &status); err != nil {
				t.Fatal(err)
			}

			got := status.String()
			if !claim.Holds(monitors[0].State == tui.Halted, strings.Contains(got, "m: halted"),
				strings.Contains(got, tc.next)) {
				t.Errorf("the view says %s, and status says\n%s\nwant halted, and %q", monitors[0].State, got,
					tc.next)
			}
		})
	}
}

// cutShortFix is a repository whose monitor m has a fix the book holds building, for a kiln that has exited. The
// developer accepted the fix when accepted is true.
func cutShortFix(t *testing.T, accepted bool) *Repo {
	t.Helper()
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}

	r, repo := scratchRepo(t)
	config := `{"gate": ["true"], "rounds": 1, "acceptance": "acceptance/{spec}/"}`
	shell(t, repo, "mkdir .kiln && echo '"+config+"' >.kiln/kiln.json")
	writeMonitor(t, repo, "m", map[string]string{"command": "true"})
	shell(t, repo, "git add . && git commit --quiet -m start")
	err := r.store.update(func(b *book) error {
		b.Specs = []*record{{Name: "m", State: building, Build: "b", Monitor: true, Accepted: accepted,
			PID: dead.Process.Pid}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	at := time.Now().Add(-time.Hour)
	for _, e := range []entry{{Kind: monitorKind, Monitor: "m", Found: foundFix}, {Kind: builtKind, Spec: "m",
		Monitor: "m"}, {Kind: startKind, Role: "build", Session: "s", PID: dead.Process.Pid}} {
		e.Build, e.At = "b", at
		if err := r.store.write(e); err != nil {
			t.Fatal(err)
		}
	}

	return r
}

// valueOf is a reading's value, or ? where kiln could not take it.
func valueOf(v *float64) string {
	if v == nil {
		return "?"
	}

	return fmt.Sprint(*v)
}
