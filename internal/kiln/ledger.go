package kiln

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
)

// The ledger is kiln's record of every build, session run and gate, one JSON line each, appended and never
// rewritten. It is kept beside the book, so it lives and dies with the clone, as the sessions' streams do.
//
//	build     when kiln fire starts a build: its spec, the commit it was approved at, its size
//	start     before each session run: its role, its session, the length of the session's stream, why it started,
//	          the process ID of the kiln fire that started it, and the commit an audit run read
//	end       after each session run: how it ended, its totals, the stream's length, and what an audit changed
//	gate      after each gate: when it ended, whether it passed, whether it gated a landing, and whether it ran the
//	          commands at main too
//	outcome   when the build lands, stops, waits on its branch, is dropped or is accepted; a wait holds its amendments
//	gauge     after a landing, one per gauge whose command printed something or failed: its name, the landed
//	          commit, its value or none, and its line and side
//	monitor   when kiln monitor run runs a monitor: its name, main's tip, git's ID of its directory, a hash of what
//	          its check or gauge printed, and what it found: healthy, couldn't run, refused, held, or a fix, which
//	          names its build. A monitor with a gauge adds the gauge's name, its reading of the whole repository, and
//	          its line
//
// A fix's build writes a build's lines, under its monitor's name, and its build line names the monitor. It ends
// landed, stopped, waits on its branch, needs you, session failed, or usage limit. Or it ends couldn't run, unchanged,
// rejected or dropped. It holds a summary or a report where it has one.
//
// Each line names its build, but a monitor line that started no fix. At is when it was written, or when its run or
// gate started. A run is the bytes of its session's stream from its start line's length to its end line's.
const ledgerFile = "ledger.jsonl"

// kind is the kind of a ledger's line.
type kind string

const (
	builtKind   kind = "build"
	startKind   kind = "start"
	endKind     kind = "end"
	gateKind    kind = "gate"
	outcomeKind kind = "outcome"
	gaugeKind   kind = "gauge"
	monitorKind kind = "monitor"
)

// How a session run ended, in an end line.
const (
	endedResult   = "result"
	endedNoResult = "no result"
	endedLimit    = "usage limit"
)

// How a build ended, in an outcome line.
const (
	outLanded   = "landed"
	outStopped  = "stopped"
	outWaits    = "waits on its branch"
	outDropped  = "dropped"
	outAccepted = "accepted"
	// A fix's outcomes, beside landed, stopped, waits on its branch and dropped.
	outNeedsYou  = "needs you"
	outFailed    = "session failed"
	outLimit     = "usage limit"
	outCouldNot  = "couldn't run"
	outUnchanged = "unchanged"
	outRejected  = "rejected"
)

// What a monitor's run found, in a monitor line. A run that started a fix names its build.
const (
	foundHealthy  = "healthy"
	foundCouldNot = "couldn't run"
	foundHeld     = "held"
	foundFix      = "fix"
	foundRefused  = "refused"
)

// entry is one line of the ledger. Its kind says which of its fields it holds.
type entry struct {
	Kind  kind      `json:"kind"`
	Build string    `json:"build"`
	At    time.Time `json:"at"`
	// A build line's.
	Spec     Spec    `json:"spec,omitempty"`
	Approved git.Rev `json:"approved,omitempty"`
	Size     int     `json:"size,omitempty"`
	// A start line's, and Session an end line's.
	Role    string `json:"role,omitempty"`
	Session string `json:"session,omitempty"`
	PID     int    `json:"pid,omitempty"`
	reason
	// Read is the commit an audit run read, in its start line.
	Read git.Rev `json:"read,omitempty"`
	// Streamed is the length of the session's stream. A start line holds it before the run, and an end line after.
	Streamed int64 `json:"streamed,omitempty"`
	// An end line's. Changed names the paths an audit run changed in the build's tree, which kiln then put back.
	Ended   string         `json:"ended,omitempty"`
	Totals  *claude.Totals `json:"totals,omitempty"`
	Changed []string       `json:"changed,omitempty"`
	// A gate line's. At is when it started. Landing is the gate on the work rebased onto main. RanMain is whether it
	// ran the commands at main too. A line from before kiln wrote it holds none.
	End     time.Time `json:"end,omitzero"`
	Passed  bool      `json:"passed,omitempty"`
	Landing bool      `json:"landing,omitempty"`
	RanMain *bool     `json:"ran_main,omitempty"`
	// An outcome line's. Amendments is the text of a waiting build's amendments.
	Outcome    string `json:"outcome,omitempty"`
	Amendments string `json:"amendments,omitempty"`
	// A gauge line's, and Gauge, Value and the line a monitor line's that read a gauge. Landed is the commit a gauge
	// line read. Value is nil when kiln could not take the reading.
	Gauge  string   `json:"gauge,omitempty"`
	Landed git.Rev  `json:"landed,omitempty"`
	Value  *float64 `json:"value,omitempty"`
	gaugeLine
	// A monitor line's, and Monitor a fix's build line's too. Main is main's tip when the run started. Entry is git's
	// ID of the monitor's directory, and Printed a hash of what its check or gauge printed. Found is what it found.
	Monitor Spec    `json:"monitor,omitempty"`
	Main    git.Rev `json:"main,omitempty"`
	Entry   string  `json:"entry,omitempty"`
	Printed string  `json:"printed,omitempty"`
	Found   string  `json:"found,omitempty"`
	// Past marks a monitor line whose check or gauge ran past kiln.json's timeout, or was held for that.
	Past bool `json:"past,omitempty"`
	// Summary is a monitor line's check's summary, or why it couldn't run. A fix's outcome line holds the report's
	// summary, the session's error, or why its gated check couldn't run. Report is the rest of an agent's report.
	Summary string `json:"summary,omitempty"`
	Report  string `json:"report,omitempty"`
}

// reason is why a session run started, in its start line. Round and Rounds are set when kiln sent the work back,
// and Try counts an audit's tries from 1.
type reason struct {
	Why    string `json:"why,omitempty"`
	Round  int    `json:"round,omitempty"`
	Rounds int    `json:"rounds,omitempty"`
	Try    int    `json:"try,omitempty"`
}

// Why a session run started, in a start line.
const (
	whyFirst       = "first"
	whyGate        = "gate"
	whyAudit       = "audit"
	whyFindings    = "findings"
	whyConflict    = "conflict"
	whyLanding     = "landing"
	whyUnchanged   = "unchanged"
	whyLimit       = "limit"
	whyInterrupted = "interrupted"
)

// write appends e to the ledger, as one line.
func (s store) write(e entry) error {
	if e.At.IsZero() {
		e.At = time.Now()
	}

	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}

	f, err := os.OpenFile(filepath.Join(s.dir, ledgerFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}

	_, err = f.Write(append(raw, '\n'))
	return errors.Join(err, f.Close())
}

// entries reads the ledger. It skips a line it cannot read, such as one cut short when kiln died writing it.
func (s store) entries() ([]entry, error) {
	f, err := os.Open(filepath.Join(s.dir, ledgerFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	defer f.Close()

	var entries []entry
	lines := bufio.NewScanner(f)
	lines.Buffer(nil, 64<<20)
	for lines.Scan() {
		var e entry
		if json.Unmarshal(lines.Bytes(), &e) == nil && e.named() {
			entries = append(entries, e)
		}
	}

	return entries, lines.Err()
}

// named is whether e names its build, or is a monitor line, which may start none.
func (e entry) named() bool {
	return e.Build != "" || e.Kind == monitorKind
}

// newBuild returns a new build's ID.
func newBuild() string {
	return strings.ToLower(rand.Text()[:12])
}

// record writes e as a line of the build's. A build with no ID writes none: its spec was approved before kiln
// kept a ledger.
func (b *build) record(e entry) {
	e.Build = b.now().Build
	if e.Build == "" {
		return
	}

	if err := b.r.store.write(e); err != nil {
		b.say("kiln could not write its ledger: %v", err)
	}
}

// begin writes the build's first line, with its size.
func (b *build) begin(ctx context.Context) {
	spec, _, err := b.r.specAt(ctx, b.rec.Approved, b.rec.Name)
	if err != nil {
		b.say("kiln could not read the spec's size: %v", err)
	}

	b.record(entry{Kind: builtKind, Spec: b.rec.Name, Approved: b.rec.Approved, Size: size(string(spec))})
}

var acceptanceTest = regexp.MustCompile(`(?m)^###\s+\d+\.`)

// size counts the numbered acceptance tests of a spec.
func size(spec string) int {
	_, tests, _ := strings.Cut(spec, "\n## Acceptance tests")
	tests, _, _ = strings.Cut(tests, "\n## ")
	return len(acceptanceTest.FindAllString(tests, -1))
}

// recount ends each run of the lane l that has a start line but no end line. kiln leaves one when it dies mid-run. It
// totals the run from its session's stream, past the length the start line holds. It ends the run as the stream
// shows: with a result only if the stream holds one. A lost audit run's end line names what it changed in its
// worktree, as talk's would. A kiln calls it as it takes the lane's lock, before any build goes on there. So no run
// of l is still running, while the other lane's may be.
func (r *Repo) recount(ctx context.Context, out io.Writer, l lane) {
	say := func(format string, args ...any) { fmt.Fprintf(out, "kiln: "+format+"\n", args...) }
	entries, err := r.store.entries()
	if err != nil {
		say("kiln could not read its ledger: %v", err)
		return
	}

	lost := lostRuns(entries)
	for _, start := range lost.starts {
		if lost.lanes[start.Build] != l {
			continue
		}

		if err := r.store.write(r.lostEnd(ctx, start, lost.specs[start.Build], say)); err != nil {
			say("kiln could not write its ledger: %v", err)
		}
	}
}

// lost is the start lines of the runs that have no end line. They are in the order their sessions first ran. It keeps
// each build's spec, and its lane: a build line that names a monitor is a fix's.
type lost struct {
	starts []entry
	specs  map[string]Spec
	lanes  map[string]lane
}

func lostRuns(entries []entry) lost {
	open := map[string]entry{}
	var order []string
	l := lost{specs: map[string]Spec{}, lanes: map[string]lane{}}
	for _, e := range entries {
		switch e.Kind {
		case builtKind:
			l.specs[e.Build] = e.Spec
			l.lanes[e.Build] = laneOf(&record{Monitor: e.Monitor != ""})
		case startKind:
			order = append(order, e.Session)
			open[e.Session] = e
		case endKind:
			delete(open, e.Session)
		}
	}

	l.starts = inOrder(order, open)
	return l
}

// inOrder is the start lines open holds, by session, in the order of order. A session that ran more than once is in
// order more than once. Its start goes in its first place.
func inOrder(order []string, open map[string]entry) []entry {
	var starts []entry
	for _, session := range order {
		if start, ok := open[session]; ok {
			starts = append(starts, start)
			delete(open, session)
		}
	}

	return starts
}

// lostEnd is the end line of the run whose start line is start, totaled from its session's stream. spec is its
// build's spec.
func (r *Repo) lostEnd(ctx context.Context, start entry, spec Spec, say func(format string, args ...any)) entry {
	counted, err := r.claude.Recount(start.Session, start.Streamed, -1)
	if err != nil {
		say("kiln could not read session %s's stream: %v", start.Session, err)
	}

	ended := endedNoResult
	if counted.Totals.Turns != nil {
		ended = endedResult
	}

	streamed, _ := r.claude.Streamed(start.Session)
	e := entry{Kind: endKind, Build: start.Build, At: counted.Written, Session: start.Session, Ended: ended,
		Totals: &counted.Totals, Streamed: streamed}
	if counted.Written.Before(start.At) {
		e.At = start.At
	}

	if start.Role == auditRole && start.Read != "" {
		if e.Changed, err = r.undo(ctx, r.auditTree(spec), start.Read); err != nil {
			say("kiln could not read what the audit of %s changed: %v", spec, err)
		}

		// Once read, the audit's worktree goes, as it does when the audit ends.
		if err := r.git.Remove(ctx, []git.Worktree{r.auditTree(spec)}); err != nil {
			say("kiln could not remove the audit's worktree of %s: %v", spec, err)
		}
	}

	return e
}

// how names how a session run ended, for its end line.
func how(ended claude.Ended) string {
	switch {
	case ended.Limit != nil:
		return endedLimit
	case ended.Totals.Turns == nil:
		return endedNoResult
	}

	return endedResult
}

// summary is the build's time, cost, turns and failed calls, as the ledger holds them so far.
func (r *Repo) summary(build string) string {
	s, err := r.stats(build)
	switch {
	case err != nil:
		return fmt.Sprintf("time, cost, turns and failed calls unknown: kiln could not read its ledger: %v", err)
	case s == nil:
		return "the ledger holds no line of this build"
	}

	return s.brief()
}

// stats is the build as the ledger holds it so far. It is nil if the ledger holds no line of it.
func (r *Repo) stats(build string) (*buildStats, error) {
	entries, err := r.store.entries()
	if err != nil {
		return nil, err
	}

	for _, s := range collect(entries) {
		if s.Build == build {
			return s, nil
		}
	}

	return nil, nil
}
