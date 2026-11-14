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
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/proc"
)

// monitor is a job kiln.json names, which kiln monitor run runs once its interval has passed. Its check finds a
// problem, and its command, its agent or both fix it.
type monitor struct {
	// Name names the monitor, by a spec's name's rule. Its fix builds on kiln/<name>.
	Name Spec `json:"name"`
	// Every is how often it runs, a Go duration in whole hours, at least 1h.
	Every string `json:"every"`
	// Check runs with sh -c. Its last line of stdout is a JSON object: fix, a boolean, and summary, a string.
	Check string `json:"check,omitempty"`
	// Command is a fix that needs no model, run with sh -c, outside the sandbox.
	Command string `json:"command,omitempty"`
	// Agent is an agent's file under .kiln/agents/, such as triage.md. It names the session's role too.
	Agent string `json:"agent,omitempty"`
	// Prompt follows the check's and the gate's output in the agent's first message.
	Prompt string `json:"prompt,omitempty"`
	// Gates says whether the check joins the gate. It is true when unset.
	Gates *bool `json:"gates,omitempty"`
	// Accept says whether a fix waits on its branch for kiln accept.
	Accept bool `json:"accept,omitempty"`
	// Tests says whether a fix may write under the acceptance path's fixed part.
	Tests bool `json:"tests,omitempty"`
}

// interval is how often m runs.
func (m monitor) interval() time.Duration {
	d, _ := time.ParseDuration(m.Every)
	return d
}

// gated is whether m's check joins its fix's gate.
func (m monitor) gated() bool {
	return m.Check != "" && (m.Gates == nil || *m.Gates)
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

	return c.stepsRefusal(m)
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
	case !m.Tests && c.fixedPart() == "":
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
}

// verdictLine is a check's last line of stdout.
type verdictLine struct {
	Fix     *bool  `json:"fix"`
	Summary string `json:"summary"`
}

// runMonitorCheck runs command with sh -c in dir, as a gauge's command runs, and reads what it found. It answers an
// error only once kiln is interrupted.
func (r *Repo) runMonitorCheck(ctx context.Context, dir git.Worktree, command string) (checked, error) {
	var stdout bytes.Buffer
	both := &lockedBuffer{}
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Dir = string(dir)
	cmd.Env = proc.Shell.Env(r.env)
	cmd.Stdout, cmd.Stderr = io.MultiWriter(&stdout, both), both
	proc.OwnGroup(cmd)
	err := cmd.Run()
	if ctx.Err() != nil {
		return checked{}, ctx.Err()
	}

	c := checked{printed: stdout.String()}
	last, found := lastOf(c.printed)
	c.found = found
	var v verdictLine
	switch {
	case err != nil:
		c.summary = cmpOr(both.last(), fmt.Sprintf("the check failed: %v", err))
	case !jsonLine(last, &v) || v.Fix == nil:
		c.summary = cmpOr(both.last(), "the check printed nothing")
	default:
		c.ran, c.fix, c.summary = true, *v.Fix, v.Summary
	}

	return c, nil
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

// last is the last line written that holds more than space.
func (l *lockedBuffer) last() string {
	last, _ := lastOf(l.String())
	return last
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// checkTree is the worktree, under the cache directory, where a monitor's check first runs, at main's tip. A spec's
// name never starts with a dot, so it is no build's tree.
const checkTree = ".monitor-check"

// firstCheck runs m's check in a worktree of its own at main. A monitor with no check asks for a fix each time.
func (r *Repo) firstCheck(ctx context.Context, m monitor, main git.Rev) (checked, error) {
	if m.Check == "" {
		return checked{ran: true, fix: true}, nil
	}

	tree := git.Worktree(filepath.Join(r.trees, checkTree))
	if err := r.git.Detached(ctx, tree, main); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return checked{}, ctx.Err()
		}

		return checked{summary: fmt.Sprintf("kiln could not check out main for the check: %v", err)}, nil
	}

	return r.runMonitorCheck(ctx, tree, m.Check)
}
