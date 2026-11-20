// Package tui draws kiln's builds in one full-screen view, live and past. It reads, and changes nothing.
//
//	home ──enter──▶ build ──enter──▶ run
//	  │                               ▲
//	  └──── the "now" line, enter ────┘
//
//	home   side panels: specs, newest first, and below them monitors; / filters both; the one selected narrows
//	       home's builds and trend to its own
//	s      on home, a build or a run: the spec, as the build's approval held it, then its amendments
//	esc    back one screen; ? lists every key a screen takes; q quits
//
//	err := tui.Show(ctx, source, "", tui.Terminal{In: os.Stdin, Out: os.Stdout})  // a name selects its spec
//
// A monitor's row shows where it stands in a word, marked as a spec's state is. Selected, the builds panel's title
// says its last run's result in full, and when it last ran and runs next. Its summary heads the panel. Its builds
// are its fixes, which open as a spec's builds do.
//
// The now panel shows the run at work in each lane, a spec's build above a monitor's fix. Each takes two rows. With
// no spec's build at work, a paused spec takes its place, in one row. Enter, or a click, opens the run.
//
// It reads its [Source] again once a second, so a running run grows as its session writes. A run lists its calls
// newest first, below its answer. A session idle for over a minute, with no call open, shows its idle time in the
// developer's yellow. Keys and the mouse both work. A click opens the row it hits, and the wheel scrolls what it is
// over, 3 lines a notch.
//
// Its colors are those of kiln's one-page sheet, where a hue says who acts. The developer's yellow marks where the
// keys go, each key's name, and what waits on them, beside ◆. A session's blue marks what runs, beside ▶. Kiln's
// orange marks a gate, a pause and a stop. Red marks what failed and green what passed, beside ✗ and ✓. On a terminal
// that says its background is light, each hue is darker.
//
// The home screen's trend is the last 10 builds that ended, beside the 10 before them. With a monitor selected, or
// all monitors, it is their fixes, with no tests or rounds to count. Each number is the median of the builds that
// know it. A ▲ or ▼ beside it says it rose or fell from the median before. Red marks a move for the worse, and green
// one for the better. Below it is a bar for each build, oldest first, with the newest in kiln's orange. A number
// kiln could not read shows as ?, never as zero.
//
// Once a build or a monitor has a gauge's reading, a gauges panel spans the width below the trend. It holds a row
// per gauge. Its ✓ or ✗ says whether the newest reading holds the gauge's line. The gauge's name and that line
// follow. In red, the row counts the readings on the line's wrong side, where some are. Its readings of landings
// follow, oldest first, each beside the spec that landed it. Below it, a gauge that monitors name has a row of their
// readings of the whole repository, named "repository". A row too narrow for them all keeps the newest. A reading
// that crossed its line is red. One read against another line shows that line too. An unknown reading shows as ?.
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
	// Text reads the spec as the build's approval held it, followed by the build's amendments, as Markdown. For a
	// fix, it reads how the fix ended.
	Text(ctx context.Context, b *Build) (string, error)
	// Monitors reads each monitor main's tip holds, as a Spec whose Monitor is set and whose builds are its fixes.
	Monitors(ctx context.Context) ([]Spec, error)
}

// Spec is one spec, with its builds, oldest first. Or it is a monitor, with its fixes. A monitor's State is short, as
// a spec's is, and its Result says the rest.
type Spec struct {
	Name   string
	State  State
	Builds []*Build
	// Monitor is set for a monitor.
	Monitor *Monitor
}

// Monitor is where a monitor stands, as kiln status shows it.
type Monitor struct {
	// Result is its last run's result in full, such as "held: waiting on the developer". It is "" if it never ran.
	Result string
	// Summary is its check's summary, the report's summary, the session's error, or why its check couldn't run.
	Summary string
	// Last is when it last ran, or zero if it never ran. Next is when it is due again, and Due is whether it is now.
	Last, Next time.Time
	Due        bool
	// Readings are its gauge's readings of the whole repository, one per run, oldest first. They are none for a
	// monitor that names no gauge.
	Readings []Reading
}

// State is where a spec stands, in the words the view shows.
type State string

const (
	Queued  State = "queued"
	Running State = "running"
	Paused  State = "paused"
	// Halted is a build whose kiln stopped mid-build. A spec's build goes on at the next kiln fire. kiln monitor run
	// drops a fix, or lands an accepted fix.
	Halted  State = "halted"
	Stopped State = "stopped"
	UAT     State = "UAT"
	// Waits is a build that waits on its branch, with amendments.
	Waits    State = "waits"
	Accepted State = "accepted"
	// Rejected is a spec whose amendments the developer turned away. Its last build was dropped, and never landed.
	Rejected State = "rejected"
)

// A monitor's states, beside Running, Stopped, Waits and Rejected, which its last fix may stand at. A held monitor
// stands where the fix that holds it ended.
const (
	Healthy  State = "healthy"
	Landed   State = "landed"
	NeedsYou State = "needs you"
	// Failed is a fix whose session failed.
	Failed State = "failed"
	// CouldNotRun is a monitor whose check couldn't run, or whose fix's gated check couldn't.
	CouldNotRun State = "couldn't run"
	// Limited is a fix that a usage limit ended.
	Limited   State = "usage limit"
	Unchanged State = "unchanged"
	// CutShort is a fix that kiln dropped before it ended.
	CutShort State = "cut short"
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
	// Fix is a monitor's fix, whose Spec names the monitor. Its Summary and Report say how it ended.
	Fix             bool
	Summary, Report string
	// Ended is how a fix ended, as its monitor's State would say. It is "" for a spec's build, or a fix not ended.
	Ended State
	// Steps are its session runs and gates, in the order they ran.
	Steps []Step
	// Took, Cost, Turns, Failed and Rounds are its sums, as kiln adds them up. Each is nil while unknown.
	Took                  *time.Duration
	Cost                  *float64
	Turns, Failed, Rounds *int
	// Readings are the gauges' readings of its landing.
	Readings []Reading
}

// Reading is one gauge's reading of a build's landing, or of the whole repository in a monitor's run.
type Reading struct {
	Gauge string
	// At is when kiln wrote it, which orders the readings of one gauge as their landings were.
	At time.Time
	// Value is nil when kiln could not take the reading.
	Value *float64
	// Line is the line it was read against, such as ≥ 80. Crossed is whether it fell on the line's wrong side.
	Line    string
	Crossed bool
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
	// Lost is a run with no end, whose kiln fire is gone.
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

// Show draws the view on the terminal until the developer quits. It opens on home, with the spec name selected
// unless name is "". It refuses a terminal whose Out is not one, and draws nothing then.
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
