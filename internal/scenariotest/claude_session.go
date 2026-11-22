package scenariotest

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fwilkerson/kiln/internal/claude"
)

const (
	claudeVersion = "2.1.301"
	claudeModel   = "claude-opus-5-5"
)

type claudeSession struct {
	work   string
	flags  claudeFlags
	call   ClaudeCall
	calls  claudeCalls
	prompt []byte
	record string
	// taken is what Claude Code says to a session ID another call holds, or "".
	taken      string
	transcript string
	promptID   string
	hooks      []claudeHookGroup
	env        []string

	mu      sync.Mutex
	stream  *os.File
	denials []jsonObject
	turns   int
	results int
}

func claudeMain() int {
	code, err := runClaude(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}

	return code
}

func runClaude(args []string) (int, error) {
	go endWithTest()
	if slices.Contains(args, "--version") {
		fmt.Println(claudeVersion + " (Claude Code)")
		return 0, nil
	}

	s, err := admitClaude(args)
	if err != nil {
		return 1, err
	}

	steps, err := loadClaudeScript(s.work, s.call)
	if err != nil {
		return 1, fmt.Errorf("scripted claude: %w", err)
	}

	if err := s.prepare(); err != nil {
		return 1, fmt.Errorf("scripted claude: %w", err)
	}

	return s.run(steps)
}

// endWithTest ends the scripted claude once the test that ran it has ended. One a test left behind then dies with
// the run. It sets no limit of its own, so a step may wait on an event while the test runs. Where the scripted
// claude leads its process group, as kiln starts it, the group's tools die with it.
func endWithTest() {
	<-testEnded()
	fmt.Fprintln(os.Stderr, "scripted claude: the test that ran it has ended")
	if syscall.Getpgrp() == os.Getpid() {
		_ = syscall.Kill(0, syscall.SIGKILL)
	}

	os.Exit(1)
}

// admitClaude records the call args make, and answers its session. It refuses the call as Claude Code would.
func admitClaude(args []string) (*claudeSession, error) {
	work := os.Getenv("WORK")
	if work == "" {
		return nil, errNoWork
	}

	flags := parseClaudeFlags(args)
	call, err := flags.newCall(args)
	if err != nil {
		return nil, fmt.Errorf("scripted claude: %w", err)
	}

	prompt, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("scripted claude: reading the prompt: %w", err)
	}

	s := &claudeSession{work: work, flags: flags, call: call, calls: claudeCalls{filepath.Join(work, ClaudeCallsDir)},
		prompt: prompt, promptID: claude.NewID()}
	if err := s.keepRecord(); err != nil {
		return nil, fmt.Errorf("scripted claude: recording the call: %w", err)
	}

	if msg := s.refusal(); msg != "" {
		return nil, errors.New(msg)
	}

	return s, s.openTranscript()
}

// newCall is the call that flags and args make, from the working directory, before the call is numbered.
func (f *claudeFlags) newCall(args []string) (ClaudeCall, error) {
	dir, err := os.Getwd()
	call := ClaudeCall{SessionID: cmp.Or(f.sessionID, claude.NewID()), Dir: dir, Args: args, Role: f.role()}
	if f.resume != "" {
		call.SessionID, call.Role, call.Resume = f.resume, "unknown", true
	}

	return call, err
}

var claudeUUID = regexp.MustCompile(`^(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// refusal is what Claude Code says to a call it refuses, or "" for one it takes.
func (s *claudeSession) refusal() string {
	prompt := ""
	if len(strings.TrimSpace(string(s.prompt))) == 0 {
		prompt = "Error: Input must be provided either through stdin or as a prompt argument when using --print"
	}

	return cmp.Or(s.flags.refusal, s.flags.sessionRefusal(), s.taken, s.flags.formatRefusal(), prompt)
}

// sessionRefusal is what Claude Code says to a session ID it refuses, or "".
func (f *claudeFlags) sessionRefusal() string {
	switch {
	case f.sessionID == "":
		return ""
	case f.resume != "":
		return "Error: --session-id can only be used with --continue or --resume if --fork-session is also specified."
	case !claudeUUID.MatchString(f.sessionID):
		return "Error: Invalid session ID. Must be a valid UUID."
	}

	return ""
}

// formatRefusal is what the scripted claude says to an output format it does not write, or "".
func (f *claudeFlags) formatRefusal() string {
	switch {
	case f.outputFormat == "stream-json" && !f.verbose:
		return "Error: When using --print, --output-format=stream-json requires --verbose"
	case !f.print || f.outputFormat != "stream-json":
		return "scripted claude: it speaks only as claude -p --output-format stream-json --verbose"
	}

	return ""
}

func (s *claudeSession) prepare() error {
	settings, err := s.loadSettings()
	if err != nil {
		return err
	}

	s.hooks = settings.hooks
	s.env = s.toolEnv(settings.env)
	s.stream, err = os.OpenFile(filepath.Join(s.record, ClaudeStreamFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}

	if err := s.keepTranscript(); err != nil {
		return fmt.Errorf("keeping the transcript: %w", err)
	}

	return nil
}

// toolEnv is the environment a tool or hook runs in: claude's own, the settings' env, then Claude Code's variables.
func (s *claudeSession) toolEnv(settingsEnv map[string]string) []string {
	set := map[string]string{}
	maps.Copy(set, settingsEnv)
	maps.Copy(set, map[string]string{
		"CLAUDECODE":             "1",
		"CLAUDE_CODE_ENTRYPOINT": "sdk-cli",
		"CLAUDE_CODE_SESSION_ID": s.call.SessionID,
		"CLAUDE_PROJECT_DIR":     s.call.Dir,
	})
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		_, overridden := set[name]
		return overridden
	})

	for name, value := range set {
		env = append(env, name+"="+value)
	}

	return env
}

func (s *claudeSession) run(steps []ClaudeStep) (int, error) {
	// The init line lists no tools as [], never as null.
	tools := append([]string{}, s.flags.tools.names...)
	slices.Sort(tools)
	s.emit(jsonObject{{"type", "system"}, {"subtype", "init"}, {"cwd", s.call.Dir}, {"session_id", s.call.SessionID},
		{"tools", tools}, {"model", claudeModel}, {"permissionMode", s.flags.permissionMode},
		{"claude_code_version", claudeVersion}, {"uuid", claude.NewID()}})
	for i, step := range steps {
		code, done, err := s.do(step)
		if err != nil {
			return 1, fmt.Errorf("scripted claude: step %d: %w", i+1, err)
		}

		if done {
			return code, nil
		}
	}

	return 0, nil
}

func (s *claudeSession) do(step ClaudeStep) (code int, done bool, err error) {
	switch {
	case step.Write != nil:
		return 0, false, s.write(step.Write)
	case step.Tool != nil:
		return 0, false, s.tool(step.Tool)
	case step.Limit != nil:
		s.emit(s.limitLine(step.Limit))
	case step.Result != nil:
		s.emit(s.resultLine(step.Result))
	case step.Exit != nil:
		return *step.Exit, true, nil
	}

	return 0, false, nil
}

func (s *claudeSession) emit(line jsonObject) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if kind, _ := line.get("type"); rawString(kind) == "assistant" {
		s.turns++
	}

	b, err := marshalJSON(line)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scripted claude: %v\n", err)
		return
	}

	b = append(b, '\n')
	_, _ = os.Stdout.Write(b)
	_, _ = s.stream.Write(b)
}

func (s *claudeSession) write(files map[string]string) error {
	for _, path := range slices.Sorted(maps.Keys(files)) {
		full := s.path(path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}

		if err := os.WriteFile(full, []byte(files[path]), 0o644); err != nil {
			return err
		}
	}

	return nil
}

func (s *claudeSession) path(p string) string {
	if filepath.IsAbs(p) {
		return p
	}

	return filepath.Join(s.call.Dir, p)
}

func (s *claudeSession) limitLine(l *ClaudeLimit) jsonObject {
	status := l.Status
	if status == "" {
		status = "rejected"
	}

	resets := time.Now().Add(time.Duration(l.ResetsIn) + time.Second - 1).Unix()
	windows := jsonObject{{"five_hour", jsonObject{{"utilization", 1.0}, {"resetsAt", resets}}}}
	if l.SevenDay != nil {
		week := jsonObject{{"utilization", *l.SevenDay}, {"resetsAt", resets}}
		windows = append(windows, jsonField{"seven_day", week})
	}

	info := jsonObject{
		{"status", status}, {"resetsAt", resets}, {"rateLimitType", "five_hour"},
		{"isUsingOverage", l.UsingOverage}, {"unifiedWindows", windows},
	}
	return jsonObject{{"type", "rate_limit_event"}, {"rate_limit_info", info}, {"uuid", claude.NewID()},
		{"session_id", s.call.SessionID}}
}

func (s *claudeSession) resultLine(r *ClaudeResult) jsonObject {
	subtype := "success"
	if r.IsError {
		subtype = "error_during_execution"
	}

	text := ""
	if r.Result != nil {
		text = *r.Result
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	line := jsonObject{
		{"type", "result"}, {"subtype", subtype}, {"is_error", r.IsError}, {"duration_ms", 1000},
		{"num_turns", s.turns}, {"result", text}, {"session_id", s.call.SessionID},
		{"permission_denials", append([]jsonObject{}, s.denials...)}, {"terminal_reason", "completed"},
		{"uuid", claude.NewID()}, {"result_index", s.results},
		{"model_usage_since_last_result", jsonObject{{claudeModel, jsonObject{{"costUSD", r.CostUSD}}}}},
	}
	if r.APIErrorStatus != nil {
		line = line.set("api_error_status", *r.APIErrorStatus)
	}

	s.results++
	return line
}
