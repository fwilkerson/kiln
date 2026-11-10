// Package claude runs Claude Code sessions headless, as claude -p, and reads how each one ended.
//
//	c := claude.New(claude.Config{Program: "claude", Env: os.Environ(), Sessions: dir})
//	ended, err := c.Run(ctx, claude.Launch{Session: id, Role: "build", System: role, Prompt: prompt, Dir: tree})
//	for err == nil && ended.Limit != nil {
//		time.Sleep(time.Until(ended.Limit.Resets))
//		ended, err = c.Run(ctx, claude.Launch{Session: id, Resume: true, Prompt: "Go on.", ...})
//	}
//
// Each session runs Opus at medium effort, with Fable as its advisor. It runs in auto permission mode, with only the
// project's settings and those Run writes. A question it would ask is refused instead, since no one answers it. Its
// commands run in Claude Code's sandbox, which writes only in Dir and Writable, and reaches only Domains. When it
// exits, Run kills every process it left behind. It finds them by their process tree, their group, and their working
// directory.
//
// A session's files are kept in Config.Sessions, under its ID. They are the system prompt, the settings, and the
// stream it wrote, across every resume. Beside them is the end of its standard error, from its last run.
//
// Run totals what each run printed, in Ended.Totals: its turns, cost, context, tool calls, waits and failed calls.
// Recount totals a run again from the stream, from the length Streamed answered before it ran, and lists its calls.
package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/fwilkerson/kiln/internal/proc"
)

// Config is what every session shares.
type Config struct {
	// Program is the claude to run. One with no slash is found on Env's PATH.
	Program string
	// Env is kiln's environment, of which a session inherits what [proc.Session] keeps.
	Env []string
	// Sessions is the absolute directory that holds each session's files.
	Sessions string
}

// Claude starts sessions as its Config says.
type Claude struct {
	c Config
}

// New returns a Claude that starts sessions as c says.
func New(c Config) Claude {
	return Claude{c: c}
}

// Launch is one start or resume of a session.
type Launch struct {
	// Session is the session's ID, a UUID. A resume names the session it resumes.
	Session string
	Resume  bool
	// Role names the kind of session, such as "build". The file that holds System is named for it.
	Role string
	// System is appended to Claude Code's system prompt.
	System string
	Prompt string
	// Dir is the absolute directory the session works in.
	Dir string
	// Tools are the only tools the session has, or nil for all of them.
	Tools []string
	// Hook is a shell command run before each Edit, Write, MultiEdit and NotebookEdit, or "" for none. It refuses the
	// call as a PreToolUse hook does.
	Hook string
	// Deny are permission rules the session's settings deny, such as "Bash(git push *)".
	Deny []string
	// Forbidden are sentences auto mode never allows, such as "Pushing to any remote."
	Forbidden []string
	// Writable are paths beside Dir where a command may write, such as Go's build cache.
	Writable []string
	// Domains are the hosts a command may reach.
	Domains []string
}

// Ended is how a session ended.
type Ended struct {
	// Text is the text of its result.
	Text string
	// Failed is true of a session that wrote no result, or an error as its result.
	Failed bool
	// Limit is set when a usage limit ended it.
	Limit *Limit
	// Totals counts what the run printed.
	Totals Totals
}

// Limit is a usage limit that ended a session. Resets is the latest reset the session reported, and zero if it gave
// none.
type Limit struct {
	Resets time.Time
}

const (
	stderrKept = 64 << 10
	// outputGrace: a child that keeps the session's output open after the session exits would otherwise keep Run
	// waiting with it.
	outputGrace = 10 * time.Second
	streamFile  = "stream.jsonl"
	stderrFile  = "stderr"
)

var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Run starts the session l names and waits for it to end. Its error is one that kept the session from starting, or
// from being read. A session that failed is an Ended, not an error.
func (c Claude) Run(ctx context.Context, l Launch) (Ended, error) {
	if err := c.check(l); err != nil {
		return Ended{}, err
	}

	program, err := proc.Look(c.c.Env, c.c.Program)
	if err != nil {
		return Ended{}, err
	}

	dir := filepath.Join(c.c.Sessions, l.Session)
	if err := write(dir, l); err != nil {
		return Ended{}, err
	}

	stream, err := os.OpenFile(filepath.Join(dir, streamFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Ended{}, err
	}

	defer stream.Close()

	cmd := exec.CommandContext(ctx, program, arguments(l, dir)...)
	cmd.Dir, cmd.Env = l.Dir, sessionEnv(c.c.Env)
	cmd.Stdin = strings.NewReader(l.Prompt)
	ended, stderr, err := run(cmd, stream)
	return ended, errors.Join(err, os.WriteFile(filepath.Join(dir, stderrFile), []byte(stderr), 0o600))
}

// check refuses a Launch that Run could not start.
func (c Claude) check(l Launch) error {
	switch {
	case !uuid.MatchString(l.Session):
		return fmt.Errorf("%q is not a session's ID: claude takes a UUID", l.Session)
	case strings.TrimSpace(l.Prompt) == "":
		return errors.New("a session starts with a prompt")
	case !filepath.IsAbs(l.Dir), !filepath.IsAbs(c.c.Sessions):
		return fmt.Errorf("the directories %q and %q are not both absolute", l.Dir, c.c.Sessions)
	case l.Role == "", strings.ContainsAny(l.Role, `/\`):
		return fmt.Errorf("%q is not a role's name", l.Role)
	}

	return nil
}

func write(dir string, l Launch) error {
	settings, err := settings(l)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	for name, content := range map[string][]byte{l.Role + ".md": []byte(l.System), settingsFile: settings} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			return err
		}
	}

	return nil
}

func arguments(l Launch, dir string) []string {
	args := []string{"-p"}
	if l.Resume {
		args = append(args, "--resume", l.Session)
	} else {
		args = append(args, "--session-id", l.Session)
	}

	args = append(args, "--append-system-prompt-file", filepath.Join(dir, l.Role+".md"))
	if l.Tools != nil {
		args = append(args, "--tools", strings.Join(l.Tools, ","))
	}

	return append(args, "--model", "opus", "--effort", "medium",
		"--permission-mode", "auto",
		"--setting-sources", "project",
		"--settings", filepath.Join(dir, settingsFile),
		"--output-format", "stream-json", "--verbose")
}

func sessionEnv(env []string) []string {
	own := []string{"CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1", "CLAUDE_CODE_RESUME_INTERRUPTED_TURN=1"}
	return append(proc.Session.Env(env), own...)
}

// run runs cmd, a session ready to start, and copies each line it prints to stream.
func run(cmd *exec.Cmd, stream io.Writer) (ended Ended, stderr string, err error) {
	tail := proc.NewTail(stderrKept)
	cmd.Stderr = tail
	proc.OwnGroup(cmd)
	cmd.WaitDelay = outputGrace
	out, err := cmd.StdoutPipe()
	if err != nil {
		return Ended{}, "", err
	}

	if err := cmd.Start(); err != nil {
		return Ended{}, "", err
	}

	tree := watch(cmd.Process.Pid, cmd.Dir)
	var read reading
	lines := bufio.NewScanner(out)
	lines.Buffer(nil, 64<<20)
	for lines.Scan() {
		_, _ = stream.Write(append(lines.Bytes(), '\n'))
		read.line(lines.Bytes())
	}

	scanned := lines.Err()
	err = cmd.Wait()
	tree.end()
	if _, exited := errors.AsType[*exec.ExitError](err); err != nil && !exited {
		return read.ended(), tail.String(), err
	}

	return read.ended(), tail.String(), scanned
}

// reading is what a session's stream has said so far.
type reading struct {
	result  *result
	limited bool
	resets  time.Time
	count   tally
}

type result struct {
	IsError   bool   `json:"is_error"`
	Text      string `json:"result"`
	APIStatus int    `json:"api_error_status"`
}

// apiRateLimited is the HTTP status of an API error that a usage limit caused.
const apiRateLimited = 429

func (r *reading) line(line []byte) {
	r.count.line(line)
	var head struct {
		Type string    `json:"type"`
		Info limitInfo `json:"rate_limit_info"`
	}
	if json.Unmarshal(line, &head) != nil {
		return
	}

	switch head.Type {
	case "rate_limit_event":
		r.limit(head.Info)
	case "result":
		var res result
		if json.Unmarshal(line, &res) == nil {
			r.result = &res
		}
	}
}

// limitInfo is the part of a rate_limit_event that says whether a usage limit ends the session.
type limitInfo struct {
	Status       string `json:"status"`
	Resets       int64  `json:"resetsAt"`
	UsingOverage bool   `json:"isUsingOverage"`
}

func (r *reading) limit(i limitInfo) {
	// Overage bills beyond the plan, which the developer never chose for an unattended build.
	if i.Status != "rejected" && !i.UsingOverage {
		return
	}

	r.limited = true
	if resets := time.Unix(i.Resets, 0); i.Resets != 0 && resets.After(r.resets) {
		r.resets = resets
	}
}

func (r *reading) ended() Ended {
	e := Ended{Totals: r.count.totals()}
	if r.result == nil {
		e.Failed = true
	} else {
		e.Text, e.Failed = r.result.Text, r.result.IsError
		r.limited = r.limited || r.result.IsError && r.result.APIStatus == apiRateLimited
	}

	if r.limited {
		e.Limit = &Limit{Resets: r.resets}
	}

	return e
}
