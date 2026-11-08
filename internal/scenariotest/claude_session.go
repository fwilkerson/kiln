package scenariotest

import (
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	claudeVersion = "2.1.301"
	claudeModel   = "claude-opus-5-5"
)

type claudeSession struct {
	flags      claudeFlags
	call       ClaudeCall
	record     string
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
	time.AfterFunc(claudeCallLimit, func() {
		fmt.Fprintf(os.Stderr, "scripted claude: it ran past its limit of %v\n", claudeCallLimit)
		os.Exit(1)
	})

	if slices.Contains(args, "--version") {
		fmt.Println(claudeVersion + " (Claude Code)")
		return 0, nil
	}

	work := os.Getenv("WORK")
	if work == "" {
		return 1, errNoWork
	}

	s := &claudeSession{promptID: newUUID()}
	flags, flagErr := parseClaudeFlags(args)
	dir, err := os.Getwd()
	if err != nil {
		return 1, fmt.Errorf("scripted claude: %w", err)
	}

	s.flags = flags
	s.call = ClaudeCall{SessionID: flags.sessionID, Resume: flags.resume != "", Dir: dir, Args: args,
		Role: claudeRole(flags.roleFile)}
	if s.call.Resume {
		s.call.SessionID, s.call.Role = flags.resume, "unknown"
	}

	if s.call.SessionID == "" {
		s.call.SessionID = newUUID()
	}

	prompt, err := io.ReadAll(os.Stdin)
	if err != nil {
		return 1, fmt.Errorf("scripted claude: reading the prompt: %w", err)
	}

	calls := claudeCalls{filepath.Join(work, ClaudeCallsDir)}
	_, taken := calls.owner(s.call.SessionID)
	record, refused, err := calls.add(&s.call)
	if err != nil {
		return 1, fmt.Errorf("scripted claude: recording the call: %w", err)
	}

	s.record = record
	if err := recordCall(record, s.call, flags, prompt); err != nil {
		return 1, fmt.Errorf("scripted claude: recording the call: %w", err)
	}

	if msg := refusal(flags, flagErr, refused, taken && !s.call.Resume, prompt); msg != "" {
		return 1, fmt.Errorf("%s", msg)
	}

	if s.call.Resume {
		s.transcript = findTranscript(s.call.SessionID)
		if s.transcript == "" {
			return 1, fmt.Errorf("%s", "No conversation found with session ID: "+s.call.SessionID)
		}
	} else {
		if refused, err := calls.open(s.call); err != nil || refused != "" {
			return 1, fmt.Errorf("scripted claude: %s%v", refused, err)
		}

		s.transcript = newTranscript(s.call.Dir, s.call.SessionID)
	}

	steps, err := loadClaudeScript(work, s.call.Role, s.call.Attempt, s.call.Call)
	if err != nil {
		return 1, fmt.Errorf("scripted claude: %w", err)
	}

	if err := s.prepare(); err != nil {
		return 1, fmt.Errorf("scripted claude: %w", err)
	}

	if err := s.keepTranscript(prompt); err != nil {
		return 1, fmt.Errorf("scripted claude: keeping the transcript: %w", err)
	}

	return s.run(steps)
}

var claudeUUID = regexp.MustCompile(`^(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// refusal is what Claude Code says to a call it refuses, or "" for one it takes.
func refusal(flags claudeFlags, flagErr error, refused string, taken bool, prompt []byte) string {
	switch {
	case flagErr != nil:
		return flagErr.Error()
	case flags.sessionID != "" && flags.resume != "":
		return "Error: --session-id can only be used with --continue or --resume if --fork-session is also specified."
	case flags.sessionID != "" && !claudeUUID.MatchString(flags.sessionID):
		return "Error: Invalid session ID. Must be a valid UUID."
	case refused != "":
		return refused
	case taken:
		return "Error: Session ID " + flags.sessionID + " is already in use."
	case flags.outputFormat == "stream-json" && !flags.verbose:
		return "Error: When using --print, --output-format=stream-json requires --verbose"
	case !flags.print || flags.outputFormat != "stream-json":
		return "scripted claude: it speaks only as claude -p --output-format stream-json --verbose"
	case len(strings.TrimSpace(string(prompt))) == 0:
		return "Error: Input must be provided either through stdin or as a prompt argument when using --print"
	}

	return ""
}

func (s *claudeSession) prepare() error {
	settings, err := loadClaudeSettings(s.flags, s.call.Dir, os.Getenv("HOME"))
	if err != nil {
		return err
	}

	s.hooks = settings.hooks
	s.env = s.toolEnv(settings.env)
	s.stream, err = os.OpenFile(filepath.Join(s.record, ClaudeStreamFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	return err
}

func (s *claudeSession) toolEnv(settingsEnv map[string]string) []string {
	env := os.Environ()
	for name, value := range settingsEnv {
		env = setEnv(env, name, value)
	}

	for _, kv := range [][2]string{
		{"CLAUDECODE", "1"},
		{"CLAUDE_CODE_ENTRYPOINT", "sdk-cli"},
		{"CLAUDE_CODE_SESSION_ID", s.call.SessionID},
		{"CLAUDE_PROJECT_DIR", s.call.Dir},
	} {
		env = setEnv(env, kv[0], kv[1])
	}

	return env
}

func setEnv(env []string, name, value string) []string {
	env = slices.DeleteFunc(env, func(kv string) bool { return strings.HasPrefix(kv, name+"=") })
	return append(env, name+"="+value)
}

func (s *claudeSession) run(steps []ClaudeStep) (int, error) {
	tools := slices.Clone(s.flags.tools)
	slices.Sort(tools)
	s.emit(jsonObject{{"type", "system"}, {"subtype", "init"}, {"cwd", s.call.Dir}, {"session_id", s.call.SessionID},
		{"tools", nonNil(tools)}, {"model", claudeModel}, {"permissionMode", s.flags.permissionMode},
		{"claude_code_version", claudeVersion}, {"uuid", newUUID()}})
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

func nonNil(items []string) []string {
	if items == nil {
		return []string{}
	}

	return items
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
	return jsonObject{{"type", "rate_limit_event"}, {"rate_limit_info", info}, {"uuid", newUUID()},
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
		{"uuid", newUUID()}, {"result_index", s.results},
		{"model_usage_since_last_result", jsonObject{{claudeModel, jsonObject{{"costUSD", r.CostUSD}}}}},
	}
	if r.APIErrorStatus != nil {
		line = line.set("api_error_status", *r.APIErrorStatus)
	}

	s.results++
	return line
}
