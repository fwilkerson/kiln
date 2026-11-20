package kiln

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/proc"
)

// projectShell runs a project's command with sh -c and env, in its own process group, for at most limit. Only
// config.shell makes one, from kiln.json.
type projectShell struct {
	env   []string
	limit time.Duration
}

// with is s, with vars added to its env, such as "KILN_BASE=<commit>".
func (s projectShell) with(vars ...string) projectShell {
	s.env = append(slices.Clone(s.env), vars...)
	return s
}

// run runs command in dir, and answers what it printed, and the error it ended with. Once command has run for
// s.limit, run kills its group, and answers a *ranPast.
func (s projectShell) run(ctx context.Context, dir git.Worktree, command string) (output, error) {
	bounded, cancel := context.WithTimeout(ctx, s.limit)
	defer cancel()
	var stdout, stderr bytes.Buffer
	both := &lockedBuffer{}
	cmd := exec.CommandContext(bounded, "/bin/sh", "-c", command)
	cmd.Dir, cmd.Env = string(dir), s.env
	cmd.Stdout, cmd.Stderr = io.MultiWriter(&stdout, both), io.MultiWriter(&stderr, both)
	err := proc.Run(cmd)
	out := output{stdout: stdout.String(), stderr: stderr.String(), both: both.String()}
	// Run answers nil for output held past its grace, so the context alone says the limit was met.
	if ctx.Err() == nil && errors.Is(bounded.Err(), context.DeadlineExceeded) {
		return out, &ranPast{limit: s.limit}
	}

	return out, err
}

// ranPast is a command that ran for limit, kiln.json's timeout, before kiln killed its group.
type ranPast struct {
	limit time.Duration
}

func (e *ranPast) Error() string {
	return fmt.Sprintf("ran past %s of %s, so kiln killed it", timeoutKey, shownDuration(e.limit))
}

// shownDuration shows d as kiln.json writes a duration, with no zero minutes or seconds at its end, such as 1h.
func shownDuration(d time.Duration) string {
	shown := d.String()
	if strings.HasSuffix(shown, "m0s") {
		shown = strings.TrimSuffix(shown, "0s")
	}

	if strings.HasSuffix(shown, "h0m") {
		shown = strings.TrimSuffix(shown, "0m")
	}

	return shown
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
