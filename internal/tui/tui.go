// Package tui draws kiln's builds in one full-screen view, live and past. It reads, and changes nothing.
//
//	home ──enter──▶ spec ──enter──▶ build ──enter──▶ run ──s──▶ the spec, as the build's approval held it
//	  │  side panel: specs, newest first, / filters      ▲
//	  └──────────── the "now" line, enter ───────────────┘   esc goes back one screen at every step
//
//	err := tui.Show(ctx, source, "", tui.Terminal{In: os.Stdin, Out: os.Stdout})  // "" opens home, a name its spec
//
// It reads its [Source] again once a second, so a running run grows as its session writes. A run lists its calls
// newest first, below its answer. Keys and the mouse both work, and ? lists every key a screen takes. A click opens
// the row it hits, and the wheel scrolls what it is over. Red marks what failed, yellow what runs or waits, and green
// what passed. A glyph beside each color says the same: ✗, ▶ and ✓. Cyan marks the panel the keys go to, and each
// key's name. It marks a spec that waits on the developer too, beside ◆.
//
// The home screen's trend is the last 10 builds that ended, beside the 10 before them. Each number is the median of
// the builds that know it. Below it is a bar for each build, oldest first, with the newest in cyan. A number kiln
// could not read shows as ?, never as zero.
package tui

import (
	"cmp"
	"context"
	"errors"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"

	"github.com/fwilkerson/kiln/internal/claude"
)

// Source is what the view reads. Each call reads afresh, and none writes.
type Source interface {
	// Specs reads every spec kiln holds or has built, the newest build first.
	Specs(ctx context.Context) ([]Spec, error)
	// Recount reads the run's stretch of its session's stream.
	Recount(r *Run) (claude.Recounted, error)
	// Text reads the spec as the build's approval held it, followed by the build's amendments, as Markdown.
	Text(ctx context.Context, b *Build) (string, error)
}

// Spec is one spec, with its builds, oldest first.
type Spec struct {
	Name   string
	State  State
	Builds []*Build
}

// State is where a spec stands, in the words the view shows.
type State string

const (
	Queued  State = "queued"
	Running State = "running"
	Paused  State = "paused"
	// Halted is a build whose kiln run stopped mid-build. It goes on at the next kiln run.
	Halted  State = "halted"
	Stopped State = "stopped"
	UAT     State = "UAT"
	// Waits is a build that waits on its branch, with amendments.
	Waits    State = "waits"
	Accepted State = "accepted"
	// Rejected is a spec whose amendments the developer turned away. Its last build was dropped, and never landed.
	Rejected State = "rejected"
)

// Build is one build of a spec, from one approval.
type Build struct {
	Spec     string
	Approved string
	Started  time.Time
	// Size counts the spec's acceptance tests.
	Size int
	// Outcome is how the build ended, as its ledger says, or "" while it has not ended.
	Outcome    string
	Amendments string
	// Steps are its session runs and gates, in the order they ran.
	Steps []Step
}

// Step is a session run, a *Run, or a gate, a *Gate.
type Step interface{ step() }

// Gate is one run of the gate.
type Gate struct {
	Start, End time.Time
	Passed     bool
	// Landing is the gate on the work rebased onto main.
	Landing bool
}

// Run is one session run: one start or resume of a session.
type Run struct {
	Role    string
	Session string
	// Why says why it started, in the words the view shows, such as "round 1 of 3". It is "" when the ledger does not
	// say, as for a run kiln started before it kept the reason. Then the build's rounds are unknown too.
	Why string
	// Round is the round it runs in, of Rounds, or 0 outside any round.
	Round, Rounds int
	Start         time.Time
	// End is when it ended, or zero while it has not.
	End time.Time
	// Lost is a run with no end, whose kiln run is gone.
	Lost   bool
	Totals claude.Totals
	// Changed names the paths an audit run changed in the build's tree.
	Changed []string
	// From and To are its bytes of the session's stream. To is -1 while it runs.
	From, To int64
}

func (*Run) step()  {}
func (*Gate) step() {}

// why is why the run started, as Why says it, or that it is unknown.
func (r *Run) why() string { return cmp.Or(r.Why, "why ?") }

// running reports whether the run is still running.
func (r *Run) running() bool { return r.End.IsZero() && !r.Lost }

var errNoTerminal = errors.New("its output is not a terminal, and it draws only on one")

// Terminal is where the view draws, Out, and what it reads keys and the mouse from, In.
type Terminal struct {
	In, Out *os.File
}

// Show draws the view on the terminal until the developer quits. It opens on the spec name, or on home when name is
// "". It refuses a terminal whose Out is not one, and draws nothing then.
func Show(ctx context.Context, src Source, name string, t Terminal) error {
	if !term.IsTerminal(t.Out.Fd()) {
		return errNoTerminal
	}

	_, err := tea.NewProgram(newModel(src, name, time.Second), tea.WithContext(ctx), tea.WithInput(t.In),
		tea.WithOutput(t.Out)).Run()
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}

	return err
}
