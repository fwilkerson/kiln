// Package claude runs Claude Code sessions headless, as claude -p, and reads how each one ended. It starts one
// session for the developer at the keyboard too, as Interact says.
//
//	c := claude.New(claude.Config{Program: "claude", Env: os.Environ(), Sessions: dir})
//	l := claude.Launch{Session: id, Role: "build", System: role, Prompt: prompt, Dir: tree, Silence: time.Hour}
//	ended, err := c.Run(ctx, l)
//	for err == nil && ended.Limit != nil {
//		time.Sleep(time.Until(ended.Limit.Resets))
//		l.Prompt = "Go on."
//		ended, err = c.Run(ctx, l)
//	}
//
// Run starts claude in Launch.Dir, with Launch.Prompt on its standard input:
//
//	claude -p --session-id <Session>                        or --resume <Session>, once Begun
//	       --append-system-prompt-file <dir>/<Role>.md
//	       --tools <Tools> --model <Model> --effort <Effort>  each only when set
//	       --permission-mode auto --setting-sources project --settings <dir>/settings.json
//	       --mcp-config <dir>/mcp.json --strict-mcp-config --output-format stream-json --verbose
//
// <dir> is Config.Sessions/<Session>, which keeps the session's files when the session is resumed:
//
//	<Role>.md      System, which Claude Code appends to its system prompt
//	settings.json  the settings below, which Claude Code lays over the project's own
//	mcp.json       Launch.MCP, the only MCP servers the session calls, which run outside the sandbox
//	stream.jsonl   each line the session printed, across every run
//	stderr         the last 64 KiB of its standard error, from its last run
//
// settings.json holds the session to what its Launch allows. No user setting reaches it:
//
//	permissions.deny        Deny, and denyInsteadOfPrompting: a question is refused, since no one answers it
//	autoMode.hard_deny      Claude Code's defaults, and Forbidden
//	autoMode.environment    no one is at the keyboard, and nothing is pushed to any remote
//	sandbox                 on, with no unsandboxed command, and failing when it cannot start; writes in Dir and
//	                        Writable alone
//	sandbox.network         Domains alone, a listener on a local port, and trustd to check a TLS certificate; each
//	                        Unix socket, when UnixSockets
//	sandbox.filesystem      denyRead from DenyRead, allowRead from AllowRead
//	advisorModel            Advisor, when set
//	hooks.PreToolUse        Hook, before each Edit, Write, MultiEdit and NotebookEdit, when set
//
// The session inherits what [proc.Session] keeps of Config.Env. Run adds CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1 and
// CLAUDE_CODE_RESUME_INTERRUPTED_TURN=1. Run runs the session by [proc.RunSession], which kills every process the
// session left behind.
//
// A session that prints nothing for Launch.Silence is hung. Run kills it, with each process it started, and answers
// a *Silent.
//
// A usage limit ends a session with Ended.Limit. A limit is a rate_limit_event that rejects the session or bills
// overage, or a result with API status 429. Overage bills beyond the plan, which no unattended session should do.
// Run kills the session at the first such event, and reads none of its stream after.
//
// Interact is the one interactive launch, for a session the developer works in, as kiln tend starts it:
//
//	claude --session-id <Session> --append-system-prompt-file <dir>/<Role>.md <Prompt>
//
// It passes no -p, no settings and no permission mode: the session is the developer's own. It runs in Dir, with
// kiln's standard input, output and error, and inherits Config.Env whole. It runs by [proc.RunAttached].
//
// Run totals what each run printed, in Ended.Totals: its turns, cost, context, tool calls, waits and failed calls.
// Totals keeps at most 200 bytes of a call's input, or of its cause. Recount totals a run again from the stream, from
// the length Streamed answered before it ran, and lists its calls.
package claude

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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

// Launch is one run of a session. Run resumes a session that Begun reports, and starts any other. So it never
// resumes a session Claude Code does not hold.
type Launch struct {
	// Session is the session's ID, a UUID.
	Session string
	// Role names the kind of session, such as "build". The file that holds System is named for it.
	Role string
	// System is appended to Claude Code's system prompt.
	System string
	// Prompt is what the run tells a session that began. First takes its place for a session that never began, or
	// is "" to tell that session Prompt too.
	Prompt, First string
	// Dir is the absolute directory the session works in.
	Dir string
	// Tools are the only tools the session has, or nil for all of them.
	Tools []string
	// Model, Effort and Advisor are the session's model, its effort, such as "high", and its advisor's model. Each
	// is Claude Code's own default when "".
	Model, Effort, Advisor string
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
	// UnixSockets lets a command connect to and listen on any Unix socket.
	UnixSockets bool
	// MCP holds the only MCP servers the session calls, by name, each as Claude Code's mcpServers holds one.
	MCP map[string]json.RawMessage
	// DenyRead are absolute paths no command may read, but those under AllowRead, which reopens what it names. The
	// sandbox holds them for a command. Deny's Read rules hold the Read tool, and hold Grep and Glob on a best-effort
	// basis.
	DenyRead, AllowRead []string
	// Silence is how long the session may print nothing before Run kills it as hung. Run refuses a Launch without one.
	Silence time.Duration
}

// Silent is Run's error for a session that printed nothing for too long. For is how long the session printed nothing.
// Run killed the session, with each process the session started.
type Silent struct {
	For time.Duration
}

func (e *Silent) Error() string {
	return fmt.Sprintf("the session printed nothing for %v, so Run killed it", e.For)
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
	streamFile = "stream.jsonl"
	stderrFile = "stderr"
)

var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Run starts the session l names and waits for it to end. Its error is one that kept the session from starting, or
// from being read. It is a *Silent for a session Run killed as hung. Any other session that failed is an Ended,
// not an error.
func (c Claude) Run(ctx context.Context, l Launch) (Ended, error) {
	begun := c.Begun(l.Session)
	if !begun && l.First != "" {
		l.Prompt = l.First
	}

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

	session := proc.Command{Path: program, Args: arguments(l, dir, begun), Dir: l.Dir, Env: sessionEnv(c.c.Env),
		Stdin: strings.NewReader(l.Prompt)}
	ended, stderr, err := run(ctx, session, stream, l.Silence)
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
	case l.Silence <= 0:
		return errors.New("a session needs a Silence, the longest it may print nothing")
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

	servers, err := json.Marshal(map[string]any{"mcpServers": nonNilMap(l.MCP)})
	if err != nil {
		return err
	}

	files := map[string][]byte{l.Role + ".md": []byte(l.System), settingsFile: settings, mcpFile: servers}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			return err
		}
	}

	return nil
}

func arguments(l Launch, dir string, begun bool) []string {
	args := []string{"-p"}
	if begun {
		args = append(args, "--resume", l.Session)
	} else {
		args = append(args, "--session-id", l.Session)
	}

	args = append(args, "--append-system-prompt-file", filepath.Join(dir, l.Role+".md"))
	if l.Tools != nil {
		args = append(args, "--tools", strings.Join(l.Tools, ","))
	}

	for _, f := range [][2]string{{"--model", l.Model}, {"--effort", l.Effort}} {
		if f[1] != "" {
			args = append(args, f[0], f[1])
		}
	}

	return append(args, "--permission-mode", "auto",
		"--setting-sources", "project",
		"--settings", filepath.Join(dir, settingsFile),
		"--mcp-config", filepath.Join(dir, mcpFile), "--strict-mcp-config",
		"--output-format", "stream-json", "--verbose")
}

func sessionEnv(env []string) []string {
	own := []string{"CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1", "CLAUDE_CODE_RESUME_INTERRUPTED_TURN=1"}
	return append(proc.Session.Env(env), own...)
}

// run runs c, a session ready to start, and copies each line it prints to stream. It kills a session that prints
// nothing for silence, and answers a *Silent.
func run(ctx context.Context, c proc.Command, stream io.Writer, silence time.Duration) (Ended, string, error) {
	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	hush := time.AfterFunc(silence, func() { stop(&Silent{For: silence}) })
	var read reading
	out := &lines{each: func(line []byte) bool {
		hush.Reset(silence)
		_, _ = stream.Write(append(line, '\n'))
		read.line(line)
		// A session that runs on past a limit bills overage, or resumes later from work it has already finished.
		if read.limited {
			stop(errLimited)
		}

		return !read.limited
	}}

	tail := proc.NewTail(stderrKept)
	c.Stdout, c.Stderr = out, tail
	err := proc.RunSession(ctx, c)
	out.end()
	// The timer runs on through the run, so a session that closed its output but never exits is killed too.
	hush.Stop()
	_, silent := errors.AsType[*Silent](err)
	_, failed := errors.AsType[*proc.Exited](err)
	switch {
	case silent:
		return read.ended(), tail.String(), err
	// The stream says how a session that failed, or that kiln killed, ended.
	case failed, ctx.Err() != nil && errors.Is(err, context.Cause(ctx)):
		return read.ended(), tail.String(), nil
	}

	return read.ended(), tail.String(), err
}

// errLimited is the cause run gives the kill of a session that a usage limit ended.
var errLimited = errors.New("a usage limit ended the session")

// lines is an io.Writer that calls each with each line written to it, without its line ending. It stops once each
// answers false. One goroutine writes to it.
type lines struct {
	each    func(line []byte) bool
	partial []byte
	done    bool
}

func (l *lines) Write(b []byte) (int, error) {
	if l.done {
		return len(b), nil
	}

	l.partial = append(l.partial, b...)
	for !l.done {
		line, rest, found := bytes.Cut(l.partial, []byte("\n"))
		if !found {
			break
		}

		l.partial = rest
		l.done = !l.each(bytes.TrimSuffix(line, []byte("\r")))
	}

	return len(b), nil
}

// end calls each with a last line that no newline ended, once the writes are done.
func (l *lines) end() {
	if !l.done && len(l.partial) > 0 {
		l.done = !l.each(l.partial)
	}
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

// NewID returns a random UUID, the form of a session ID and of each event's uuid in a session's stream.
func NewID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
