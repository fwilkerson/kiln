// Package proc holds what kiln's adapters share to start a child process and to read it back. It finds the
// program, filters the environment, and groups the process. It keeps the end of its standard error, and reads the
// process table.
//
//	path, err := proc.Look(env, "claude")
//	if err != nil {
//		return err // a *proc.NotOnPath
//	}
//	cmd := exec.CommandContext(ctx, path, args...)
//	cmd.Env = proc.Session.Env(env)
//	stderr := proc.NewTail(64 << 10)
//	cmd.Stderr = stderr
//	proc.OwnGroup(cmd) // when ctx ends, the whole group is killed
//	err = cmd.Run()
//
// Each adapter keeps its own policy: which program it runs, which [Child] that is, and how long it waits. Package
// proc decides only what each kind of child withholds of kiln's environment, marked ✗:
//
//	variable               Shell  Session  Git    why
//	ANTHROPIC_API_KEY             ✗               would bill an account instead of the developer's login
//	ANTHROPIC_AUTH_TOKEN          ✗               the same
//	GIT_*                  ✗      ✗        ✗      GIT_DIR or GIT_INDEX_FILE would aim git at another repository
//	CLAUDE*                       ✗               Claude Code's own, set by the session that may have started kiln
//
// A session still keeps the four CLAUDE* variables that say how the developer signs in: CLAUDE_CODE_OAUTH_TOKEN,
// CLAUDE_CONFIG_DIR, CLAUDE_CODE_USE_BEDROCK and CLAUDE_CODE_USE_VERTEX. Every child still keeps the four GIT_*
// variables that name who commits: GIT_AUTHOR_NAME, GIT_AUTHOR_EMAIL, GIT_COMMITTER_NAME and GIT_COMMITTER_EMAIL.
//
// kiln runs on macOS alone. The process table is read with sysctl(3), not /bin/ps, which is setuid and which a
// session's sandbox refuses to exec. [WorkingDirs] asks lsof, which it gives 5 s. [Run] gives a group 10 s to close a
// command's output, once the command exits within its limit.
package proc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Look finds program on the PATH that env holds, where [exec.LookPath] would read kiln's own. It skips a
// relative entry, which would find a program in whatever directory kiln was started from. A program whose
// name holds a slash is taken as given, unchecked. Its only error is a *[NotOnPath].
func Look(env []string, program string) (string, error) {
	if strings.Contains(program, "/") {
		return program, nil
	}

	path := pathOf(env)
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			continue
		}

		if candidate := filepath.Join(dir, program); Executable(candidate) {
			return candidate, nil
		}
	}

	return "", &NotOnPath{Program: program, Path: path}
}

// pathOf returns the value of the last PATH entry in env, or "" when env holds none.
func pathOf(env []string) string {
	var path string
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			path = v
		}
	}

	return path
}

// NotOnPath is [Look]'s answer for a program that no absolute entry of Path holds.
type NotOnPath struct {
	Program string
	Path    string
}

func (e *NotOnPath) Error() string {
	return fmt.Sprintf("%s is not on the PATH: %s", e.Program, e.Path)
}

// outputGrace is how long [Run] waits for a group to close a command's output once the command exits.
const outputGrace = 10 * time.Second

// Executable says whether path names a regular file that someone may execute.
func Executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// OwnGroup makes cmd's process lead a process group of its own. A signal to the group then reaches what it
// started, and kiln's interrupt does not reach it. When cmd's context ends, the whole group is killed, not
// cmd's process alone. Call it before cmd starts, on a cmd made with [exec.CommandContext], since
// Start refuses a Cancel on any other.
func OwnGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}

// Run runs cmd, made with [exec.CommandContext], in a process group of its own, as [OwnGroup] says. When cmd's own
// process still runs once limit has passed, Run kills its group, and answers a *[RanPast]. Once cmd's process exits,
// what it started has [outputGrace] to close cmd's output. Run then kills cmd's process group, when the output stayed
// open or cmd failed. A process that left the group survives. Run answers the error cmd's process ended with, and nil
// for output held past the grace.
func Run(cmd *exec.Cmd, limit time.Duration) error {
	OwnGroup(cmd)
	cmd.WaitDelay = outputGrace
	if err := cmd.Start(); err != nil {
		return err
	}

	var past atomic.Bool
	pid := cmd.Process.Pid
	started, _ := Of(pid)
	timer := time.AfterFunc(limit, func() {
		// A process that exited ended in time, though a child still holds its output.
		if stillRuns(pid, started.Born) {
			past.Store(true)
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	})

	err := cmd.Wait()
	timer.Stop()
	held := errors.Is(err, exec.ErrWaitDelay)
	// Go answers an ExitError, not ErrWaitDelay, when the output stayed open and the shell failed.
	if _, failed := errors.AsType[*exec.ExitError](err); held || failed {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}

	switch {
	case past.Load():
		return &RanPast{Limit: limit}
	case held:
		return nil
	}

	return err
}

// stillRuns reports whether the process pid, started at born, has yet to exit. Born tells a reused pid apart.
func stillRuns(pid int, born int64) bool {
	p, err := Of(pid)
	switch {
	case err != nil, p.Zombie:
		return false
	}

	return p.Born == born
}

// RanPast is [Run]'s error for a command whose own process still ran once Limit passed. Run killed its group.
type RanPast struct {
	Limit time.Duration
}

func (e *RanPast) Error() string {
	return fmt.Sprintf("ran past %v, so its group was killed", e.Limit)
}

// Tail is an [io.Writer] that keeps only the last bytes written to it. Those may be the end of a child's
// standard error. It is safe for concurrent use.
type Tail struct {
	keep int
	mu   sync.Mutex
	end  []byte
}

// NewTail returns a Tail that keeps the last keep bytes.
func NewTail(keep int) *Tail {
	return &Tail{keep: keep}
}

func (t *Tail) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.end = append(t.end, b...)
	if over := len(t.end) - t.keep; over > 0 {
		t.end = t.end[over:]
	}

	return len(b), nil
}

// String returns the bytes kept so far.
func (t *Tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.end)
}
