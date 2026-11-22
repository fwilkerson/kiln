package kiln

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/proc"
)

// projectShell runs a project's command with sh -c and env, in its own process group, for at most limit. Only
// config.shell and notifyShell make one.
type projectShell struct {
	env   []string
	limit time.Duration
	// bound names the setting that gave limit, for the message of what kiln killed.
	bound string
}

// kilnVar names the path of the kiln that runs a script. The script runs it in place of a kiln on the PATH.
const kilnVar = "KILN"

// with is s, with vars added to its env, such as "KILN_BASE=<commit>".
func (s projectShell) with(vars ...string) projectShell {
	s.env = append(slices.Clone(s.env), vars...)
	return s
}

// run runs command in dir, with this kiln's path in $KILN, as [proc.Run] runs a program. It answers what the command
// printed, and the error it ended with. When the shell still runs once s.limit has passed, run kills its group, and
// answers a *ranPast.
func (s projectShell) run(ctx context.Context, dir git.Worktree, command string) (output, error) {
	exe, err := os.Executable()
	if err != nil {
		return output{}, err
	}

	var stdout, stderr bytes.Buffer
	both := &lockedBuffer{}
	ctx, stop := context.WithTimeoutCause(ctx, s.limit, &ranPast{limit: s.limit, bound: s.bound})
	defer stop()
	// Of two values of one variable, the last wins. So this kiln's path replaces one a caller passed on.
	err = proc.Run(ctx, proc.Command{Path: "/bin/sh", Args: []string{"-c", command}, Dir: string(dir),
		Env: append(slices.Clone(s.env), kilnVar+"="+exe), Stdout: io.MultiWriter(&stdout, both),
		Stderr: io.MultiWriter(&stderr, both)})
	return output{stdout: stdout.String(), stderr: stderr.String(), both: both.String()}, err
}

// ranPast is a command that ran for limit, which the setting bound names, before kiln killed its group.
type ranPast struct {
	limit time.Duration
	bound string
}

func (e *ranPast) Error() string {
	return fmt.Sprintf("ran past %s of %s, so kiln killed it", e.bound, shownDuration(e.limit))
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

// output is what a check or a gauge printed: each stream, and both as they arrived.
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
