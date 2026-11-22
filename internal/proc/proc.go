// Package proc is how kiln runs a program, and what it reads of the processes it ran. It finds the program, filters
// the environment, and runs the program by one of three policies. It keeps the end of a standard error.
//
//	path, err := proc.Look(env, "claude")
//	if err != nil {
//		return err // a *proc.NotOnPath
//	}
//	stderr := proc.NewTail(64 << 10)
//	c := proc.Command{Path: path, Args: args, Dir: tree, Env: proc.Session.Env(env), Stderr: stderr}
//	ctx, stop := context.WithTimeoutCause(ctx, time.Hour, errHung)
//	defer stop()
//	err = proc.RunSession(ctx, c) // errHung, once the session ran an hour: kiln killed what it started
//
// Outside tests, packages that serve tests and tools/, no other package of kiln imports os/exec. A caller picks how its
// program runs by the func it calls:
//
//	func         group     when ctx ends while it runs    once it exits
//	Run          its own   kills its group                kills its group, if it failed or a child holds its output
//	RunSession   its own   kills its tree                 kills its tree
//	RunAttached  kiln's    takes no ctx                   kills nothing
//
// A program in a group of its own is out of reach of the terminal's Ctrl-C, which reaches only RunAttached's. A
// process that leaves the group, as setsid does, survives Run. A session's tree is each process the session started,
// found by parent and by group. It adds each orphan whose working directory is under Command.Dir. Each kill is
// SIGKILL, at once, since kiln waits on no program to clean up.
//
// kiln ends a program by ending its ctx, with a cause such as context.WithTimeoutCause gives. Run and RunSession then
// answer that cause. Once a program exits, its children have 10 s to close its output, the output grace. A run then
// closes the output, and output held past the grace is no error.
//
// Each kind of [Child] withholds part of kiln's environment, marked ✗:
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
// session's sandbox refuses to exec. An orphan's working directory comes from lsof, which proc gives 5 s.
package proc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fwilkerson/kiln/internal/crash"
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

// Executable says whether path names a regular file that someone may execute.
func Executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// outputGrace is how long a run waits for a program's children to close its output, once the program exits.
const outputGrace = 10 * time.Second

// Command is a program for kiln to run, and what it reads and writes. A nil Stdin reads nothing, and a nil Stdout or
// Stderr discards what the program writes there.
type Command struct {
	// Path is the program: a path, or a name that [Look] finds on Env's PATH.
	Path string
	Args []string
	// Dir is the directory the program runs in, or "" for kiln's own.
	Dir string
	// Env is the program's whole environment, or nil for kiln's own.
	Env            []string
	Stdin          io.Reader
	Stdout, Stderr io.Writer
}

// Run runs c in a process group of its own, and waits for it to end. When ctx ends while c's program runs, Run kills
// its group, and answers context.Cause(ctx). Once the program exits, Run kills its group if it failed. Run does so
// too when a child held the output past the output grace. Run answers an *[Exited] for a program that failed, and nil
// for output held past the grace. Any other error is one that kept the program from starting.
func Run(ctx context.Context, c Command) error {
	return run(ctx, c, &group{})
}

// RunSession runs c as [Run] does, but each kill reaches c's whole tree, and its group. Once c's program exits,
// RunSession kills what its tree left behind.
func RunSession(ctx context.Context, c Command) error {
	crash.Step("session start")
	defer crash.Step("session end")
	return run(ctx, c, &session{place: c.Dir})
}

// RunAttached runs c in kiln's own process group, and waits for it to end. The terminal's Ctrl-C reaches c's program
// as it reaches kiln. No context ends it, and it kills nothing. It answers an *[Exited] for a program that failed, and
// nil for output held past the output grace.
func RunAttached(c Command) error {
	cmd, err := c.command(context.Background())
	if err != nil {
		return err
	}

	crash.Step("attached start")
	defer crash.Step("attached end")

	cmd.WaitDelay = outputGrace
	return exited(cmd.Run())
}

// command is the exec.Cmd that runs c, with ctx.
func (c Command) command(ctx context.Context) (*exec.Cmd, error) {
	env := c.Env
	if env == nil {
		env = os.Environ()
	}

	path, err := Look(env, c.Path)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, path, c.Args...)
	cmd.Dir, cmd.Env = c.Dir, c.Env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = c.Stdin, c.Stdout, c.Stderr
	return cmd, nil
}

// ender kills what a run started: when its ctx ends, and once its program exits.
type ender interface {
	// begin learns the program's process, before any kill.
	begin(pid int)
	kill()
	// exited ends what is left once the program exits. failed is true of a program that failed, or whose child held
	// its output.
	exited(failed bool)
}

func run(ctx context.Context, c Command, e ender) error {
	cmd, err := c.command(ctx)
	if err != nil {
		return err
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = outputGrace
	// exec calls Cancel only while the program runs, and may call it before Start returns.
	begun := make(chan struct{})
	var killed atomic.Bool
	cmd.Cancel = func() error {
		<-begun
		killed.Store(true)
		e.kill()
		return nil
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	e.begin(cmd.Process.Pid)
	close(begun)
	err = cmd.Wait()
	held := errors.Is(err, exec.ErrWaitDelay)
	// Go answers an ExitError, not ErrWaitDelay, when the output stayed open and the program failed.
	_, failed := errors.AsType[*exec.ExitError](err)
	e.exited(held || failed)
	if killed.Load() {
		return context.Cause(ctx)
	}

	return exited(err)
}

// exited is err, from exec's Wait, as a run answers it.
func exited(err error) error {
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return &Exited{Code: exit.ExitCode(), State: exit.ProcessState.String()}
	}

	if errors.Is(err, exec.ErrWaitDelay) {
		return nil
	}

	return err
}

// Exited is a run's error for a program that exited with a nonzero status, or that a signal ended.
type Exited struct {
	// Code is the program's exit status, or -1 when a signal ended it.
	Code int
	// State says how the program ended, such as "exit status 1" or "signal: killed".
	State string
}

func (e *Exited) Error() string { return e.State }

// group kills a program's process group.
type group struct{ pid int }

func (g *group) begin(pid int) { g.pid = pid }

func (g *group) kill() { _ = syscall.Kill(-g.pid, syscall.SIGKILL) }

// exited leaves a quiet child of a program that succeeded, such as a build daemon, running for the next program.
func (g *group) exited(failed bool) {
	if failed {
		g.kill()
	}
}

// session kills a program's tree, which it watches from begin, then its group.
type session struct {
	place string
	pid   int
	tree  *tree
}

func (s *session) begin(pid int) {
	s.pid = pid
	s.tree = watch(pid, s.place)
}

// kill ends the tree before the group: a group kill misses a child forked as it lands.
func (s *session) kill() {
	s.tree.end()
	_ = syscall.Kill(-s.pid, syscall.SIGKILL)
}

func (s *session) exited(bool) { s.kill() }

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
