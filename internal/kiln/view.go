package kiln

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"syscall"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/tui"
)

// TUI shows kiln's builds in a full-screen view, live and past, until the developer quits. It opens on the spec
// name, or on home when name is "". It reads what kiln run writes, takes no lock, and writes nothing. It refuses an
// out that is not a terminal.
func (r *Repo) TUI(ctx context.Context, name string, in, out *os.File) error {
	return tui.Show(ctx, view{r}, name, in, out)
}

// view reads the book, the ledger and the sessions' streams for the TUI.
type view struct {
	r *Repo
}

// Specs answers each spec in the book. A spec the ledger holds and the book no longer does was accepted or rejected.
// Its last build's outcome says which. The newest build comes first, and a spec with no build yet before them all.
func (v view) Specs(context.Context) ([]tui.Spec, error) {
	b, err := v.r.store.read()
	if err != nil {
		return nil, err
	}

	entries, err := v.r.store.entries()
	if err != nil {
		return nil, err
	}

	var specs []*tui.Spec
	named := map[string]*tui.Spec{}
	for _, rec := range b.Specs {
		named[rec.Name] = &tui.Spec{Name: rec.Name, State: stateOf(rec)}
		specs = append(specs, named[rec.Name])
	}

	for _, s := range collect(entries) {
		spec := named[s.Spec]
		if spec == nil {
			named[s.Spec] = &tui.Spec{Name: s.Spec}
			spec = named[s.Spec]
			specs = append(specs, spec)
		}

		if b.find(s.Spec) == nil {
			spec.State = tui.Rejected
			if s.outcome == outAccepted {
				spec.State = tui.Accepted
			}
		}

		spec.Builds = append(spec.Builds, s.shown())
	}

	shown := make([]tui.Spec, len(specs))
	for i, s := range specs {
		shown[i] = *s
	}

	slices.SortStableFunc(shown, func(a, b tui.Spec) int {
		if len(a.Builds) == 0 || len(b.Builds) == 0 {
			return len(a.Builds) - len(b.Builds)
		}

		return b.Builds[len(b.Builds)-1].Started.Compare(a.Builds[len(a.Builds)-1].Started)
	})

	return shown, nil
}

// Recount reads the run's bytes of its session's stream.
func (v view) Recount(r *tui.Run) (claude.Recounted, error) {
	return v.r.claude.Recount(r.Session, r.From, r.To)
}

// Text reads the spec as the build's approval held it. A spec main no longer holds shows as the build saw it. Its
// amendments follow it.
func (v view) Text(ctx context.Context, b *tui.Build) (string, error) {
	spec, found, err := v.r.git.Show(ctx, b.Approved, specPath(b.Spec))
	switch {
	case err != nil:
		return "", err
	case !found:
		return "", fmt.Errorf("the commit %s holds no %s", short(b.Approved), specPath(b.Spec))
	case b.Amendments == "":
		return string(spec), nil
	}

	return string(spec) + "\n\n---\n\n" + b.Amendments, nil
}

// stateOf is where a spec stands. A build is building only while the kiln run that marked it so lives. A kiln run
// stopped by an interrupt, or killed, leaves the book saying building. A queued build waits for a kiln run, which may
// be running already.
func stateOf(rec *record) tui.State {
	switch {
	case rec.State == building && !alive(rec.PID):
		return tui.Halted
	case rec.State == building && !rec.Paused.IsZero():
		return tui.Paused
	}

	return map[state]tui.State{queued: tui.Queued, building: tui.Running, stopped: tui.Stopped, landed: tui.UAT,
		amended: tui.Waits}[rec.State]
}

// shown is the build as the TUI shows it: its runs and gates in the order they ran.
func (s *buildStats) shown() *tui.Build {
	b := &tui.Build{Spec: s.Spec, Approved: s.Approved, Started: s.At, Size: s.Size, Outcome: s.outcome,
		Amendments: s.amendments}
	gates := 0
	for _, r := range s.runs {
		for ; gates < r.gates && gates < len(s.gates); gates++ {
			b.Steps = append(b.Steps, gateOf(s.gates[gates]))
		}

		run := &tui.Run{Role: r.role, Session: r.session, Why: r.why.shown(), Round: r.why.Round,
			Rounds: r.why.Rounds, Start: r.start, End: r.end, Totals: r.totals, Changed: r.changed, From: r.from,
			To: r.to}
		if r.end.IsZero() {
			run.To, run.Lost = -1, !alive(r.pid)
		}

		b.Steps = append(b.Steps, run)
	}

	for _, g := range s.gates[gates:] {
		b.Steps = append(b.Steps, gateOf(g))
	}

	return b
}

func gateOf(e entry) *tui.Gate {
	return &tui.Gate{Start: e.At, End: e.End, Passed: e.Passed, Landing: e.Landing}
}

// alive reports whether the process pid lives. A run's start line names the kiln run that started it.
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return pid > 0 && (err == nil || errors.Is(err, syscall.EPERM))
}

// shown is why a run started, in the words the view shows. It is "" when its start line does not say.
func (w reason) shown() string {
	round := fmt.Sprintf("round %d of %d", w.Round, w.Rounds)
	if w.Rounds == 0 {
		round = fmt.Sprintf("round %d", w.Round)
	}

	switch w.Why {
	case whyFirst:
		return "first run"
	case whyGate:
		return round
	case whyAudit:
		return fmt.Sprintf("audit, try %d", w.Try)
	case whyFindings:
		return "audit's findings"
	case whyConflict:
		return "conflict with main, " + round
	case whyLanding:
		return "landing gate failed, " + round
	case whyLimit:
		return "after a usage limit"
	case whyInterrupted:
		return "after kiln stopped"
	}

	return ""
}
