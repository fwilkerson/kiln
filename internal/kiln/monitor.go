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
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/proc"
)

// monitor is a job kiln.json names, which kiln monitor run runs once its interval has passed. Its check or its gauge
// finds a problem, and its command, its agent or both fix it.
type monitor struct {
	// Name names the monitor, by a spec's name's rule. Its fix builds on kiln/<name>.
	Name Spec `json:"name"`
	// Every is how often it runs, a Go duration in whole hours, at least 1h.
	Every string `json:"every"`
	// Check runs with sh -c. Its last line of stdout is a JSON object: fix, a boolean, and summary, a string.
	Check string `json:"check,omitempty"`
	// Gauge names a gauge of kiln.json's gauges, read over the whole repository in place of a check.
	Gauge string `json:"gauge,omitempty"`
	// Command is a fix that needs no model, run with sh -c, outside the sandbox.
	Command string `json:"command,omitempty"`
	// Agent is an agent's file under .kiln/agents/, such as triage.md. It names the session's role too.
	Agent string `json:"agent,omitempty"`
	// Prompt follows the check's or the gauge's output, and the gate's, in the agent's first message.
	Prompt string `json:"prompt,omitempty"`
	// Gates says whether the check joins the gate. It is true when unset.
	Gates *bool `json:"gates,omitempty"`
	// Accept says whether a fix waits on its branch for kiln accept.
	Accept bool `json:"accept,omitempty"`
	// Tests says whether a fix may write under the acceptance path's fixed part.
	Tests bool `json:"tests,omitempty"`
	// Drain says whether a fix that lands starts the check again at once, in the same kiln monitor run.
	Drain bool `json:"drain,omitempty"`
}

// interval is how often m runs.
func (m monitor) interval() time.Duration {
	d, _ := time.ParseDuration(m.Every)
	return d
}

// gated is whether m's check or gauge joins its fix's gate. A gauge always does.
func (m monitor) gated() bool {
	if m.Gauge != "" {
		return true
	}

	return m.Check != "" && (m.Gates == nil || *m.Gates)
}

// finds is whether m has a finding to compare: what its check or its gauge printed.
func (m monitor) finds() bool {
	return m.Check != "" || m.Gauge != ""
}

var agentName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// role is the name of m's agent's file under .kiln/agents/, less .md, which names its session's role. It is "" for
// a monitor with no agent, or one whose agent is no file directly under .kiln/agents/.
func (m monitor) role() string {
	if m.Agent == "" {
		return ""
	}

	name := strings.TrimPrefix(strings.TrimPrefix(m.Agent, "./"), kilnDir+"/agents/")
	name = strings.TrimSuffix(name, ".md")
	if !agentName.MatchString(name) {
		return ""
	}

	return name
}

// hash is a hash of m's entry in kiln.json, as the hold compares it.
func (m monitor) hash() string {
	raw, _ := json.Marshal(m)
	return hashOf(string(raw))
}

// hashOf is a short hash of text.
func hashOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

// checkMonitors refuses a monitor kiln could not run, as the package doc lists.
func (c config) checkMonitors() error {
	named := map[Spec]bool{}
	for _, m := range c.Monitors {
		if why := c.monitorRefusal(m, named); why != "" {
			return fmt.Errorf("%s names a monitor %q that %s", configFile, m.Name, why)
		}

		named[m.Name] = true
	}

	return nil
}

// monitorRefusal says why kiln.json may not name m, or "". named holds the monitors named before it.
func (c config) monitorRefusal(m monitor, named map[Spec]bool) string {
	switch {
	case m.Name == "", named[m.Name]:
		return "has no name, or a name another monitor has"
	case !specName.MatchString(string(m.Name)):
		return "has a name no spec could have, such as deps"
	case !wholeHours(m.Every):
		return "runs every interval that is not whole hours, at least 1h, such as \"24h\""
	}

	if why := c.gaugeRefusal(m); why != "" {
		return why
	}

	return c.stepsRefusal(m)
}

// gaugeRefusal says why kiln.json may not name m's gauge, or "".
func (c config) gaugeRefusal(m monitor) string {
	_, found := c.gaugeNamed(m.Gauge)
	switch {
	case m.Gauge == "":
		return ""
	case !found:
		return "names a gauge that gauges does not hold"
	case m.Check != "":
		return "names both a check and a gauge, where it may name one"
	case m.Gates != nil:
		return "names gates beside a gauge, which always joins the gate; gates says whether a check joins it"
	}

	return ""
}

// gaugeNamed is the gauge of c named name.
func (c config) gaugeNamed(name string) (gauge, bool) {
	i := slices.IndexFunc(c.Gauges, func(g gauge) bool { return g.Name == name })
	if i < 0 {
		return gauge{}, false
	}

	return c.Gauges[i], true
}

// wholeHours is whether every is a Go duration in whole hours, at least 1h.
func wholeHours(every string) bool {
	d, err := time.ParseDuration(every)
	return err == nil && d >= time.Hour && d%time.Hour == 0
}

// stepsRefusal says why kiln.json may not name m's steps, or "".
func (c config) stepsRefusal(m monitor) string {
	switch {
	case m.Command == "" && m.Agent == "":
		return "names no command and no agent, so it fixes nothing"
	case m.Agent != "" && m.role() == "":
		return "names an agent that is no file under .kiln/agents/, such as \"triage.md\""
	case m.role() == auditRole:
		return "names the audit as its agent, which changes nothing"
	case m.Drain && !m.finds():
		return "drains with no check and no gauge, so nothing says when its queue is empty"
	case !m.Tests && c.unguarded():
		return "may not write the tests, while acceptance names no path before {spec} for kiln to guard"
	}

	return ""
}

// monitorNamed is the monitor of c named name.
func (c config) monitorNamed(name Spec) (monitor, bool) {
	i := slices.IndexFunc(c.Monitors, func(m monitor) bool { return m.Name == name })
	if i < 0 {
		return monitor{}, false
	}

	return c.Monitors[i], true
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
		c.ran, c.fix, c.summary = true, *v.Fix, v.Summary
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

// firstCheck runs m's check, or reads its gauge, in a worktree of its own at main. c is kiln.json at main. A monitor
// with neither asks for a fix each time.
func (r *Repo) firstCheck(ctx context.Context, c config, m monitor, main git.Rev) (checked, error) {
	if !m.finds() {
		return checked{ran: true, fix: true}, nil
	}

	g, _ := c.gaugeNamed(m.Gauge)
	tree, err := r.scratchTree(ctx, checkTree, main)
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return checked{}, ctx.Err()
	case err != nil:
		return couldNotCheck(g, fmt.Sprintf("kiln could not check out main for the check: %v", err)), nil
	case m.Gauge == "":
		return r.runMonitorCheck(ctx, tree, m.Check)
	}

	empty, err := r.git.EmptyTree(ctx)
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return checked{}, ctx.Err()
	case err != nil:
		return couldNotCheck(g, fmt.Sprintf("kiln could not name git's empty tree: %v", err)), nil
	}

	return r.measured(ctx, tree, g, empty)
}

// couldNotCheck is a check that couldn't run, for why. g is the monitor's gauge, or the zero gauge for a check.
func couldNotCheck(g gauge, why string) checked {
	c := checked{summary: why}
	if g.Name != "" {
		c.reading = entry{Gauge: g.Name, gaugeLine: g.gaugeLine}
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
