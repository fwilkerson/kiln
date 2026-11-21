package kiln

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/fwilkerson/kiln/internal/git"
)

// gaugeLine is a gauge's line. A reading below AtLeast, or above AtMost, crossed it. A monitor.json beside a gauge
// sets exactly one of them. The ledger keeps them by the names its lines have always had, min and max.
type gaugeLine struct {
	// AtLeast is the line a reading below crosses.
	AtLeast *float64 `json:"min,omitempty"`
	// AtMost is the line a reading above crosses.
	AtMost *float64 `json:"max,omitempty"`
}

// crossed reports whether value is on the wrong side of the line. An unknown value, nil, never is.
func (l gaugeLine) crossed(value *float64) bool {
	switch {
	case value == nil:
		return false
	case l.AtLeast != nil:
		return *value < *l.AtLeast
	case l.AtMost != nil:
		return *value > *l.AtMost
	}

	return false
}

// sides is how many of AtLeast and AtMost the line names.
func (l gaugeLine) sides() int {
	n := 0
	for _, side := range []*float64{l.AtLeast, l.AtMost} {
		if side != nil {
			n++
		}
	}

	return n
}

// text shows the line and its side, such as ≥ 80.
func (l gaugeLine) text() string {
	switch {
	case l.AtLeast != nil:
		return "≥ " + number(*l.AtLeast)
	case l.AtMost != nil:
		return "≤ " + number(*l.AtMost)
	}

	return "unset"
}

// number shows v in as few digits as it takes.
func number(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// gaugeTree is the worktree, under the cache directory, where kiln reads the gauges. A spec's name never starts with
// a dot, so it is no build's tree.
const gaugeTree = ".gauges"

// baseVar names the commit a reading starts at, which kiln gives each gauge. After a landing, it is main's
// tip before the landing. In a monitor's run, it is git's empty tree. In a fix's gate, it is where the fix left main.
const baseVar = "KILN_BASE"

// readGauges reads each gauge at main's tip, once the build has landed. It writes each reading to the ledger, and
// prints it. A gauge never stops the build. It answers only the context's error, once kiln is interrupted, and writes
// no reading for the gauge it cut short.
func (b *build) readGauges(ctx context.Context) error {
	if b.landed == "" {
		return nil
	}

	err := b.readEach(ctx)
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err != nil {
		b.say("kiln could not read the gauges: %v", err)
	}

	return nil
}

// readEach reads the gauges, one at a time, in a worktree of their own at the landed commit. It holds commandsLock
// while it reads. So no gate runs beside a gauge, and one kiln at a time uses the worktree.
func (b *build) readEach(ctx context.Context) error {
	c, gauges, err := b.r.gauges(ctx)
	if err != nil || len(gauges) == 0 {
		return err
	}

	held, err := b.r.lockCommands(ctx, b.rec.Name, b.out)
	if err != nil {
		return err
	}

	defer held.release()

	tree, treeErr := held.scratchTree(ctx, gaugeTree, b.landed)
	if ctx.Err() != nil {
		return ctx.Err()
	}

	for _, m := range gauges {
		// A landing kiln could not check out reads unknown on each gauge, never as missing.
		rd := gaugeRead{why: fmt.Sprintf("kiln could not check out the landing: %v", treeErr)}
		if treeErr == nil {
			if rd, err = c.shell(b.r.env).readGauge(ctx, tree, m, b.onto); err != nil {
				return err
			}
		}

		if rd.nothing {
			// No reading is written, so the last one kiln took still stands, with its landing.
			b.say("gauge %s: nothing to read in this landing, as %s", m.Name, rd.why)
			continue
		}

		why := ""
		if rd.why != "" {
			why = ", since " + rd.why
		}

		e := m.reading(rd.value)
		e.Kind, e.Landed = gaugeKind, b.landed
		b.record(e)
		b.say("gauge %s%s", shownReading(e), why)
	}

	return nil
}

// scratchTree checks out at in the worktree name, under the cache directory, where a check or a gauge runs. A command
// may leave a file git can't remove, such as one in a read-only directory. Then the worktree is made anew. So one
// command can't stop every later run there.
func (held commands) scratchTree(ctx context.Context, name string, at git.Rev) (git.Worktree, error) {
	r := held.r
	tree := git.Worktree(filepath.Join(r.trees, name))
	err := r.git.Detached(ctx, tree, at)
	if err == nil || ctx.Err() != nil {
		return tree, err
	}

	if err := removeTree(string(tree)); err != nil {
		return tree, err
	}

	return tree, r.git.Detached(ctx, tree, at)
}

// removeTree removes dir and all it holds, though a directory in it be read-only.
func removeTree(dir string) error {
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o700)
		}

		return nil
	})

	return os.RemoveAll(dir)
}

// gauges are kiln.json at main's tip, and the monitors there that hold a gauge, by name. A monitor kiln refuses is
// left out.
func (r *Repo) gauges(ctx context.Context) (config, []monitor, error) {
	_, c, reads, err := r.mainMonitors(ctx)
	var gauges []monitor
	for _, m := range runnable(reads) {
		if m.finder == gaugeScript {
			gauges = append(gauges, m)
		}
	}

	return c, gauges, err
}

// gaugeRead is what one run of a gauge read.
type gaugeRead struct {
	// value is the reading, or nil when kiln could not take it. why then says why. nothing is whether the command
	// exited 0 and printed nothing, as a gauge does that read no file.
	value   *float64
	why     string
	nothing bool
	// failed is whether the command failed, and last the last line of its stderr, or of its stdout. past is the
	// *ranPast of a command kiln killed at its limit, or nil.
	failed bool
	last   string
	past   error
	// printed is the command's stdout, which a monitor's hold compares, and found what it printed above its number.
	printed, found string
	// both is all the command wrote, to stdout and stderr.
	both string
}

// readGauge runs m's gauge in tree, with base in $KILN_BASE, and answers what it read. It answers an error only once
// kiln is interrupted. A gauge that ran past s's limit failed, and its reading says so.
func (s projectShell) readGauge(ctx context.Context, tree git.Worktree, m monitor, base git.Rev) (gaugeRead, error) {
	out, err := s.with(baseVar+"="+string(base)).run(ctx, tree, m.path(gaugeScript))
	if ctx.Err() != nil {
		return gaugeRead{}, ctx.Err()
	}

	rd := gaugeRead{last: out.why(), printed: out.stdout, both: out.both}
	_, rd.found = lastOf(out.stdout)
	value, ok := readingOf(out.stdout)
	_, past := errors.AsType[*ranPast](err)
	switch {
	case past:
		rd.failed, rd.last, rd.why, rd.past = true, "", "its command "+err.Error(), err
	case err != nil:
		rd.failed, rd.why = true, "its command failed: "+cmpOr(rd.last, err.Error())
	case strings.TrimSpace(out.stdout) == "":
		rd.nothing, rd.why = true, "its command printed nothing"
	case !ok:
		rd.why = "its command's last line is no number"
	default:
		rd.value = &value
	}

	return rd, nil
}

// summary is a monitor's summary of rd, the reading e. A failed command's summary is its last line, or its exit
// status.
func (rd gaugeRead) summary(e entry) string {
	switch {
	case rd.failed && rd.last != "":
		return rd.last
	case rd.value == nil:
		return e.Gauge + ": " + rd.why
	}

	return shownReading(e)
}

var decimal = regexp.MustCompile(`^[-+]?(\d+\.?\d*|\.\d+)([eE][-+]?\d+)?$`)

// readingOf reads a gauge's command's output: its last line, trimmed, as a decimal number.
func readingOf(out string) (float64, bool) {
	out = strings.TrimSpace(out)
	last := strings.TrimSpace(out[strings.LastIndex(out, "\n")+1:])
	if !decimal.MatchString(last) {
		return 0, false
	}

	v, err := strconv.ParseFloat(last, 64)
	return v, err == nil && !math.IsInf(v, 0)
}

// shownReading is a gauge line's reading beside the line it was read against. kiln fire and kiln status show it.
func shownReading(e entry) string {
	value := "unknown"
	if e.Value != nil {
		value = number(*e.Value)
	}

	shown := fmt.Sprintf("%s: %s, line %s", e.Gauge, value, e.text())
	if e.crossed(e.Value) {
		shown += ", crossed"
	}

	return shown
}

// gaugeStatus shows each gauge at main's tip, with its latest reading and the landing it measured.
func (r *Repo) gaugeStatus(ctx context.Context, out io.Writer) error {
	_, gauges, err := r.gauges(ctx)
	if err != nil {
		fmt.Fprintf(out, "kiln could not read its gauges: %v\n", err)
		return nil
	}

	entries, err := r.store.entries()
	if err != nil {
		return err
	}

	latest := map[string]reading{}
	for _, s := range collect(entries) {
		for _, e := range s.readings {
			if was, ok := latest[e.Gauge]; !ok || !e.At.Before(was.At) {
				latest[e.Gauge] = reading{e, s.Spec}
			}
		}
	}

	for _, m := range gauges {
		rd, ok := latest[string(m.Name)]
		if !ok {
			fmt.Fprintf(out, "gauge %s: no reading yet, line %s\n", m.Name, m.line().text())
			continue
		}

		fmt.Fprintf(out, "gauge %s, at %s's landing %s\n", shownReading(rd.entry), rd.spec, short(rd.Landed))
	}

	return nil
}

// reading is a gauge line, and the spec whose landing it read.
type reading struct {
	entry
	spec Spec
}
