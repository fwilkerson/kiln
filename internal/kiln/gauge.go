package kiln

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/proc"
)

// gauge is a command kiln.json names, which prints a number. Its line is where that number should stay.
type gauge struct {
	// Name names the gauge. No two gauges share one.
	Name string `json:"name"`
	// Command runs with sh -c, at the top of a worktree at the landed commit. Its last line of stdout is its reading.
	Command string `json:"command"`
	gaugeLine
}

// gaugeLine is a gauge's line. A reading below Min, or above Max, crossed it. kiln.json sets exactly one of them.
type gaugeLine struct {
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
}

// crossed reports whether value is on the wrong side of the line. An unknown value, nil, never is.
func (l gaugeLine) crossed(value *float64) bool {
	switch {
	case value == nil:
		return false
	case l.Min != nil:
		return *value < *l.Min
	case l.Max != nil:
		return *value > *l.Max
	}

	return false
}

// text shows the line and its side, such as ≥ 80.
func (l gaugeLine) text() string {
	switch {
	case l.Min != nil:
		return "≥ " + number(*l.Min)
	case l.Max != nil:
		return "≤ " + number(*l.Max)
	}

	return "unset"
}

// number shows v in as few digits as it takes.
func number(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// gaugeTree is the worktree, under the cache directory, where kiln reads the gauges. A spec's name never starts with
// a dot, so it is no build's tree.
const gaugeTree = ".gauges"

// baseVar names main's tip before the landing, which kiln gives each gauge's command.
const baseVar = "KILN_BASE"

// readGauges reads each gauge kiln.json names at main's tip, once the build has landed. It writes each reading to the
// ledger, and prints it. A gauge never stops the build. It answers only the context's error, once kiln is interrupted,
// and writes no reading for the gauge it cut short.
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

// readEach reads the gauges, one at a time, in a worktree of their own at the landed commit. It takes no main.lock, so
// a kiln accept's gate at main never waits on a slow gauge.
func (b *build) readEach(ctx context.Context) error {
	gauges, err := b.r.gauges(ctx)
	if err != nil || len(gauges) == 0 {
		return err
	}

	tree, treeErr := b.r.gaugeTree(ctx, b.landed)
	if ctx.Err() != nil {
		return ctx.Err()
	}

	for _, g := range gauges {
		// A landing kiln could not check out reads unknown on each gauge, never as missing.
		value, why := (*float64)(nil), fmt.Sprintf(", since kiln could not check out the landing: %v", treeErr)
		if treeErr == nil {
			if value, why, err = b.r.readGauge(ctx, tree, g.Command, b.onto); err != nil {
				return err
			}
		}

		e := entry{Kind: gaugeKind, Gauge: g.Name, Landed: b.landed, Value: value, gaugeLine: g.gaugeLine}
		b.record(e)
		b.say("gauge %s%s", shownReading(e), why)
	}

	return nil
}

// gaugeTree checks out landed in the gauges' worktree. A gauge may leave a file git can't remove, such as one in a
// read-only directory. Then the worktree is made anew, so one gauge can't stop every later reading.
func (r *Repo) gaugeTree(ctx context.Context, landed git.Rev) (git.Worktree, error) {
	tree := git.Worktree(filepath.Join(r.trees, gaugeTree))
	err := r.git.Detached(ctx, tree, landed)
	if err == nil || ctx.Err() != nil {
		return tree, err
	}

	if err := removeTree(string(tree)); err != nil {
		return tree, err
	}

	return tree, r.git.Detached(ctx, tree, landed)
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

// gauges are the gauges kiln.json names at main's tip.
func (r *Repo) gauges(ctx context.Context) ([]gauge, error) {
	main, err := r.git.Tip(ctx, mainBranch)
	if err != nil {
		return nil, err
	}

	c, err := r.config(ctx, main)
	return c.Gauges, err
}

// readGauge runs command in tree, with base in $KILN_BASE. It answers its reading, or nil and why kiln could not take
// it. It answers an error only once kiln is interrupted.
func (r *Repo) readGauge(ctx context.Context, tree git.Worktree, command string, base git.Rev) (*float64, string,
	error) {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Dir = string(tree)
	cmd.Env = append(slices.Clone(proc.Shell.Env(r.env)), baseVar+"="+string(base))
	proc.OwnGroup(cmd)
	out, err := cmd.Output()
	switch {
	case ctx.Err() != nil:
		return nil, "", ctx.Err()
	case err != nil:
		return nil, fmt.Sprintf(", since its command failed: %v", err), nil
	}

	value, ok := readingOf(string(out))
	if !ok {
		return nil, ", since its command's last line is no number", nil
	}

	return &value, "", nil
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

// shownReading is a gauge line's reading beside the line it was read against. kiln run and kiln status show it.
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

// gaugeStatus shows each gauge kiln.json names at main's tip, with its latest reading and the landing it measured.
func (r *Repo) gaugeStatus(ctx context.Context, out io.Writer) error {
	gauges, err := r.gauges(ctx)
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

	for _, g := range gauges {
		rd, ok := latest[g.Name]
		if !ok {
			fmt.Fprintf(out, "gauge %s: no reading yet, line %s\n", g.Name, g.text())
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
