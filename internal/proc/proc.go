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
//	proc.OwnGroup(cmd)
//	if err := cmd.Start(); err != nil {
//		return err
//	}
//	_ = proc.SignalGroup(cmd.Process.Pid, syscall.SIGTERM)
//
// Each adapter keeps its own policy: which program it runs, which [Child] that is, and how long it waits. Package
// proc decides none of it.
//
// kiln runs on macOS alone. The process table is read with sysctl(3), not /bin/ps, which is setuid and which a
// session's sandbox refuses to exec. [WorkingDirs] asks lsof, which it gives 5 s.
package proc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// Look finds program on the PATH that env holds, where [exec.LookPath] would read kiln's own. It skips a
// relative entry, which would find a program in whatever directory kiln was started from. A program whose
// name holds a slash is taken as given, unchecked. Its only error is a *[NotOnPath].
func Look(env []string, program string) (string, error) {
	if strings.Contains(program, "/") {
		return program, nil
	}

	var path string
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			path = v
		}
	}

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

// NotOnPath is [Look]'s answer for a program that no absolute entry of Path holds.
type NotOnPath struct {
	Program string
	Path    string
}

func (e *NotOnPath) Error() string {
	return fmt.Sprintf("%s is not on the PATH: %s", e.Program, e.Path)
}

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

// SignalGroup sends s to every process of group. It refuses a group of 0 or 1, which kill(2) reads as the caller's
// own group and as every process. A group with no process left is not an error: it ended while the caller decided
// to end it.
func SignalGroup(group int, s syscall.Signal) error {
	if group <= 1 {
		return errors.New("no process group to signal: " + strconv.Itoa(group))
	}

	if err := syscall.Kill(-group, s); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}

	return nil
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
