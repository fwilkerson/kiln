package kiln

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/proc"
)

// monitorsDir holds a directory for each monitor, named for the monitor.
const monitorsDir = kilnDir + "/monitors"

// The files of a monitor's directory. kiln runs each script with sh, at the top of a worktree, so none needs to be
// executable.
const (
	// settingsFile is how often the monitor runs, its gauge's line, and what its fix may do.
	settingsFile = "monitor.json"
	// checkScript finds a problem. Its last line of stdout is a verdict.
	checkScript = "check"
	// gaugeScript finds a problem by a number, the monitor's gauge. kiln reads it after each landing too.
	gaugeScript = "gauge"
	// commandScript fixes the problem with no model, outside the sandbox.
	commandScript = "command"
	// monitorAgent is the agent that fixes the problem in a session, written as any agent's file is.
	monitorAgent = "agent.md"
)

// monitor is a directory under .kiln/monitors/, which kiln monitor run runs once its interval has passed. Its check or
// its gauge finds a problem, and its command, its agent or both fix it.
type monitor struct {
	// Name is the directory's name, by a spec's name's rule. Its fix builds on kiln/<name>.
	Name Spec
	settings
	// finder is checkScript or gaugeScript, whichever the directory holds, or "" for neither.
	finder string
	// command and agent say whether the directory holds commandScript and monitorAgent.
	command, agent bool
	// tree is git's ID of the directory, which a change to any file in it changes. The hold compares it.
	tree string
}

// settings are a monitor's monitor.json. Each key may be left out.
type settings struct {
	// Every is how often the monitor runs, a Go duration in whole hours, at least 1h. It is 24h when unset.
	Every string `json:"every"`
	// Gates says whether the check or the gauge joins the fix's gate. It is true when unset.
	Gates *bool `json:"gates"`
	// Accept says whether a fix waits on its branch for kiln accept.
	Accept bool `json:"accept"`
	// Tests says whether a fix may write under the acceptance paths' parts before {spec}.
	Tests bool `json:"tests"`
	// gaugeLine is the gauge's line, which a monitor with a gauge names, and no other.
	gaugeLine
}

// everyDay is how often a monitor runs when its monitor.json names no interval.
const everyDay = 24 * time.Hour

// interval is how often m runs.
func (m monitor) interval() time.Duration {
	if m.Every == "" {
		return everyDay
	}

	d, _ := time.ParseDuration(m.Every)
	return d
}

// gated is whether m's check or gauge joins its fix's gate.
func (m monitor) gated() bool {
	return m.finds() && (m.Gates == nil || *m.Gates)
}

// finds is whether m has a finding to compare: what its check or its gauge printed.
func (m monitor) finds() bool {
	return m.finder != ""
}

// path is the path of file in m's directory, from the repository's top.
func (m monitor) path(file string) string {
	return path.Join(monitorsDir, string(m.Name), file)
}

// script is the command that runs the script file of m's directory.
func (m monitor) script(file string) string {
	return "sh " + m.path(file)
}

// gauge is m's gauge, named for m. It is a gauge only when m's directory holds one.
func (m monitor) gauge() gauge {
	return gauge{Name: string(m.Name), Command: m.script(gaugeScript), gaugeLine: m.gaugeLine}
}

// hashOf is a short hash of text.
func hashOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

// monitors reads kiln.json at the commit at, and each monitor's directory under .kiln/monitors/ there, by name. It
// refuses a monitor kiln could not run, as the package doc lists.
func (r *Repo) monitors(ctx context.Context, at git.Rev) (config, []monitor, error) {
	c, err := r.config(ctx, at)
	if err != nil {
		return c, nil, err
	}

	dirs, err := r.monitorDirs(ctx, at)
	if err != nil {
		return c, nil, err
	}

	monitors := make([]monitor, len(dirs))
	for i, d := range dirs {
		if monitors[i], err = r.readDir(ctx, at, c, d); err != nil {
			return c, nil, err
		}
	}

	return c, monitors, nil
}

// monitorDir is a monitor's directory at a commit: its name, git's ID of it, and the files it holds.
type monitorDir struct {
	name  Spec
	tree  string
	files []string
}

// monitorDirs lists each monitor's directory under .kiln/monitors/ at the commit at, by name. It refuses a file
// directly there, since a monitor is a directory.
func (r *Repo) monitorDirs(ctx context.Context, at git.Rev) ([]monitorDir, error) {
	entries, err := r.git.Tree(ctx, at, monitorsDir)
	var dirs []monitorDir
	files := map[string][]string{}
	for _, e := range entries {
		name, file, inside := strings.Cut(e.Path, "/")
		switch {
		case inside:
			files[name] = append(files[name], file)
		case !e.Dir:
			return nil, fmt.Errorf("%s/%s is a file: a monitor is a directory, such as %s/deps/", monitorsDir, name,
				monitorsDir)
		default:
			dirs = append(dirs, monitorDir{name: Spec(name), tree: e.ID})
		}
	}

	for i, d := range dirs {
		dirs[i].files = files[string(d.name)]
	}

	return dirs, err
}

// readDir reads the monitor whose directory is d, at the commit at. c is kiln.json there.
func (r *Repo) readDir(ctx context.Context, at git.Rev, c config, d monitorDir) (monitor, error) {
	var raw []byte
	if slices.Contains(d.files, settingsFile) {
		var err error
		if raw, _, err = r.git.Show(ctx, at, path.Join(monitorsDir, string(d.name), settingsFile)); err != nil {
			return monitor{}, err
		}
	}

	m, err := c.readMonitor(d.name, d.files, raw)
	m.tree = d.tree
	return m, err
}

// readMonitor reads the monitor name from the files its directory holds, and from raw, its monitor.json or nil. c is
// kiln.json. It refuses a monitor kiln could not run, as the package doc lists.
func (c config) readMonitor(name Spec, files []string, raw []byte) (monitor, error) {
	m := monitor{Name: name, command: slices.Contains(files, commandScript),
		agent: slices.Contains(files, monitorAgent)}
	if raw != nil {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&m.settings); err != nil {
			return m, fmt.Errorf("%s: %w", m.path(settingsFile), err)
		}
	}

	for _, finder := range []string{checkScript, gaugeScript} {
		if slices.Contains(files, finder) {
			m.finder = finder
		}
	}

	if why := c.monitorRefusal(m, files); why != "" {
		return m, fmt.Errorf("%s/ %s", m.path(""), why)
	}

	if _, found := c.gaugeNamed(string(name)); found && m.finder == gaugeScript {
		return m, fmt.Errorf("%s names a gauge %s, as %s/ does: rename one", configFile, name, m.path(""))
	}

	return m, nil
}

// monitorRefusal says why kiln may not run m, whose directory holds files, or "".
func (c config) monitorRefusal(m monitor, files []string) string {
	switch {
	case !specName.MatchString(string(m.Name)):
		return "has a name no spec could have, such as deps"
	case m.Every != "" && !wholeHours(m.Every):
		return "runs every interval that is not whole hours, at least 1h, such as \"24h\""
	case slices.Contains(files, checkScript) && slices.Contains(files, gaugeScript):
		return "holds both a check and a gauge, where it may hold one"
	case m.finder == gaugeScript && m.sides() != 1:
		return "holds a gauge, so its monitor.json names exactly one of min and max, the gauge's line"
	case m.finder != gaugeScript && m.sides() > 0:
		return "names min or max, a gauge's line, but holds no gauge"
	case !m.command && !m.agent:
		return "holds no command and no agent.md, so it fixes nothing"
	case !m.Tests && c.unguarded():
		return "may not write the tests, while acceptance names no path before {spec} for kiln to guard"
	}

	return ""
}

// wholeHours is whether every is a Go duration in whole hours, at least 1h.
func wholeHours(every string) bool {
	d, err := time.ParseDuration(every)
	return err == nil && d >= time.Hour && d%time.Hour == 0
}

// monitorNamed is the monitor of monitors named name.
func monitorNamed(monitors []monitor, name Spec) (monitor, bool) {
	i := slices.IndexFunc(monitors, func(m monitor) bool { return m.Name == name })
	if i < 0 {
		return monitor{}, false
	}

	return monitors[i], true
}

// nowVar names a time, as RFC 3339, that kiln monitor run takes as now.
const nowVar = "KILN_NOW"

// now is the time a monitor's run starts at: the machine's clock, or the time $KILN_NOW names.
func (r *Repo) now() time.Time {
	if at, err := time.Parse(time.RFC3339, envValue(r.env, nowVar)); err == nil {
		return at
	}

	return time.Now()
}

// envValue is the value of the variable name in env, or "".
func envValue(env []string, name string) string {
	for _, kv := range slices.Backward(env) {
		if value, ok := strings.CutPrefix(kv, name+"="); ok {
			return value
		}
	}

	return ""
}

// checked is what a check found.
type checked struct {
	// ran is false for a check that couldn't run: it exited non-zero, or its last line is no verdict.
	ran bool
	// fix is whether the check asks for a fix.
	fix bool
	// summary is the verdict's summary, or the last line of output of a check that couldn't run.
	summary string
	// drain is whether the verdict asks kiln to check again at once, once the fix it starts lands.
	drain bool
	// printed is the check's stdout, which the hold compares, and found what it printed above its last line.
	printed, found string
	// reading is a gauge's reading, as a monitor line holds it: the gauge's name, its value and its line. Its Gauge
	// is "" for a check.
	reading entry
}

// verdictLine is a check's last line of stdout.
type verdictLine struct {
	Fix     *bool  `json:"fix"`
	Summary string `json:"summary"`
	Drain   bool   `json:"drain"`
}

// runMonitorCheck runs command with sh -c in dir, as a gauge's command runs, and reads what it found. It answers an
// error only once kiln is interrupted.
func (r *Repo) runMonitorCheck(ctx context.Context, dir git.Worktree, command string) (checked, error) {
	out, err := runShell(ctx, dir, command, proc.Shell.Env(r.env))
	if ctx.Err() != nil {
		return checked{}, ctx.Err()
	}

	c := checked{printed: out.stdout}
	last, found := lastOf(c.printed)
	c.found = found
	var v verdictLine
	switch {
	case err != nil:
		c.summary = cmpOr(out.last(), fmt.Sprintf("the check failed: %v", err))
	case !jsonLine(last, &v) || v.Fix == nil:
		c.summary = cmpOr(out.last(), "the check printed nothing")
	default:
		c.ran, c.fix, c.summary, c.drain = true, *v.Fix, v.Summary, v.Drain
	}

	return c, nil
}

// output is what a check's or a gauge's command printed: each stream, and both as they arrived.
type output struct {
	stdout, stderr, both string
}

// last is the last line the command wrote, to stdout or stderr, that holds more than space.
func (o output) last() string {
	last, _ := lastOf(o.both)
	return last
}

// why is the last line of stderr, or of stdout where stderr has none. Two pipes keep no order between them, so a
// failed command's last line of stderr says why it failed.
func (o output) why() string {
	last, _ := lastOf(o.stderr)
	if last == "" {
		last, _ = lastOf(o.stdout)
	}

	return last
}

// runShell runs command with sh -c in dir, with env, in a process group of its own. It answers what the command
// printed, and the error it ended with.
func runShell(ctx context.Context, dir git.Worktree, command string, env []string) (output, error) {
	var stdout, stderr bytes.Buffer
	both := &lockedBuffer{}
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Dir, cmd.Env = string(dir), env
	cmd.Stdout, cmd.Stderr = io.MultiWriter(&stdout, both), io.MultiWriter(&stderr, both)
	err := proc.Run(cmd)
	return output{stdout: stdout.String(), stderr: stderr.String(), both: both.String()}, err
}

// lastOf splits out, trimmed, into its last line and what comes above it.
func lastOf(out string) (last, above string) {
	out = strings.TrimRight(out, "\r\n\t ")
	i := strings.LastIndex(out, "\n")
	return strings.TrimSpace(out[i+1:]), strings.TrimRight(out[:max(i, 0)], "\r\n")
}

// lockedBuffer is a buffer two writers share, a command's stdout and its stderr.
type lockedBuffer struct {
	// mu guards b. Write and String hold it.
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// checkTree is the worktree, under the cache directory, where a monitor's check or gauge runs. Its run reads main's
// tip, and its fix's gate reads the fix's commit there. A spec's name never starts with a dot, so it is no build's
// tree.
const checkTree = ".monitor-check"

// firstCheck runs m's check, or reads its gauge, in a worktree of its own at main. A monitor with neither asks for a
// fix each time.
func (held commands) firstCheck(ctx context.Context, m monitor, main git.Rev) (checked, error) {
	r := held.r
	if !m.finds() {
		return checked{ran: true, fix: true}, nil
	}

	tree, err := held.scratchTree(ctx, checkTree, main)
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return checked{}, ctx.Err()
	case err != nil:
		return couldNotCheck(m, fmt.Sprintf("kiln could not check out main for the check: %v", err)), nil
	case m.finder == checkScript:
		return r.runMonitorCheck(ctx, tree, m.script(checkScript))
	}

	empty, err := r.git.EmptyTree(ctx)
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return checked{}, ctx.Err()
	case err != nil:
		return couldNotCheck(m, fmt.Sprintf("kiln could not name git's empty tree: %v", err)), nil
	}

	return r.measured(ctx, tree, m.gauge(), empty)
}

// couldNotCheck is a check of m that couldn't run, for why. Its reading names m's gauge, where m has one.
func couldNotCheck(m monitor, why string) checked {
	c := checked{summary: why}
	if m.finder == gaugeScript {
		c.reading = entry{Gauge: string(m.Name), gaugeLine: m.gaugeLine}
	}

	return c
}

// measured reads g in tree, from base, as a check's verdict. On its line is healthy, and crossed asks for a fix. A
// reading kiln could not take couldn't run. It answers an error only once kiln is interrupted.
func (r *Repo) measured(ctx context.Context, tree git.Worktree, g gauge, base git.Rev) (checked, error) {
	rd, err := r.readGauge(ctx, tree, g.Command, base)
	if err != nil {
		return checked{}, err
	}

	e := entry{Gauge: g.Name, Value: rd.value, gaugeLine: g.gaugeLine}
	return checked{ran: rd.value != nil, fix: g.crossed(rd.value), summary: rd.summary(e), printed: rd.printed,
		found: rd.found, reading: e}, nil
}
