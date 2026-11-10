package scenariotest

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

type claudeHookGroup struct {
	Matcher string              `json:"matcher"`
	Hooks   []claudeHookCommand `json:"hooks"`
}

type claudeHookCommand struct {
	Type    string  `json:"type"`
	Command string  `json:"command"`
	Timeout float64 `json:"timeout"`
}

type claudeSettings struct {
	env   map[string]string
	hooks []claudeHookGroup
}

// claudeSettingsFile is the part of a settings file the scripted claude reads.
type claudeSettingsFile struct {
	Env   map[string]string            `json:"env"`
	Hooks map[string][]claudeHookGroup `json:"hooks"`
}

// loadSettings reads the settings the call's flags name, and merges them in the order Claude Code does.
func (s *claudeSession) loadSettings() (claudeSettings, error) {
	var texts [][]byte
	for _, path := range s.settingsPaths() {
		text, err := os.ReadFile(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return claudeSettings{}, err
		}

		texts = append(texts, text)
	}

	given, err := s.flags.givenSettings()
	if err != nil {
		return claudeSettings{}, err
	}

	return mergeClaudeSettings(append(texts, given...))
}

// settingsPaths lists the settings file of each source the flags name. Without --setting-sources, it lists all three.
func (s *claudeSession) settingsPaths() []string {
	home := os.Getenv("HOME")
	bySource := map[string]string{
		"user":    filepath.Join(home, ".claude", "settings.json"),
		"project": filepath.Join(s.call.Dir, ".claude", "settings.json"),
		"local":   filepath.Join(s.call.Dir, ".claude", "settings.local.json"),
	}
	sources := s.flags.settingSources.names
	if !s.flags.settingSources.given {
		sources = []string{"user", "project", "local"}
	}

	var paths []string
	for _, source := range sources {
		if path, ok := bySource[source]; ok {
			paths = append(paths, path)
		}
	}

	return paths
}

// givenSettings reads the settings --settings gives, as JSON or as a file's path.
func (f claudeFlags) givenSettings() ([][]byte, error) {
	switch {
	case f.settings == "":
		return nil, nil
	case strings.HasPrefix(strings.TrimSpace(f.settings), "{"):
		return [][]byte{[]byte(f.settings)}, nil
	}

	text, err := readFile(f.settings)
	return [][]byte{text}, err
}

func mergeClaudeSettings(texts [][]byte) (claudeSettings, error) {
	merged := claudeSettings{env: map[string]string{}}
	for _, text := range texts {
		file, err := parseClaudeSettings(text)
		if err != nil {
			return claudeSettings{}, err
		}

		maps.Copy(merged.env, file.Env)
		merged.hooks = append(merged.hooks, file.Hooks["PreToolUse"]...)
	}

	return merged, nil
}

// parseClaudeSettings parses a settings file, and refuses a PreToolUse hook the scripted claude cannot run.
func parseClaudeSettings(text []byte) (claudeSettingsFile, error) {
	var file claudeSettingsFile
	if err := json.Unmarshal(text, &file); err != nil {
		return file, fmt.Errorf("a settings file: %w", err)
	}

	for _, group := range file.Hooks["PreToolUse"] {
		if i := slices.IndexFunc(group.Hooks, notCommand); i >= 0 {
			return file, fmt.Errorf("a PreToolUse hook of type %q: the scripted claude runs only command hooks",
				group.Hooks[i].Type)
		}
	}

	return file, nil
}

func notCommand(hook claudeHookCommand) bool { return hook.Type != "command" }

var plainMatcher = regexp.MustCompile(`^[A-Za-z0-9_\- ,|]+$`)

func (g claudeHookGroup) matches(tool string) bool {
	switch {
	case g.Matcher == "" || g.Matcher == "*":
		return true
	case plainMatcher.MatchString(g.Matcher):
		for name := range strings.FieldsFuncSeq(g.Matcher, func(r rune) bool { return r == '|' || r == ',' }) {
			if strings.TrimSpace(name) == tool {
				return true
			}
		}

		return false
	}

	re, err := regexp.Compile(g.Matcher)
	return err == nil && re.MatchString(tool)
}

// toolUse is one call of a tool, as the model asks for it.
type toolUse struct {
	id, name string
	input    jsonObject
}

// toolOutcome is what a tool answers. A nil useResult makes the tool_use_result the content.
type toolOutcome struct {
	content   string
	isError   bool
	useResult any
}

func failed(err error) toolOutcome { return toolOutcome{content: err.Error(), isError: true} }

// result is the outcome's tool_use_result.
func (o toolOutcome) result() any {
	switch {
	case o.useResult != nil:
		return o.useResult
	case o.isError:
		return "Error: " + o.content
	}

	return o.content
}

func (s *claudeSession) tool(t *ClaudeTool) error {
	use := toolUse{id: newID("toolu_"), name: t.Name}
	use.input, _ = parseObject(t.Input)
	if v, ok := use.input.get("file_path"); ok {
		use.input = use.input.set("file_path", s.path(rawString(v)))
	}

	usage := json.RawMessage(`{"input_tokens":2,"cache_read_input_tokens":0,"output_tokens":16}`)
	if t.Usage != nil {
		usage = t.Usage
	}

	s.assistant(jsonObject{{"type", "tool_use"}, {"id", use.id}, {"name", use.name}, {"input", use.input}}, usage)
	if !t.Raw {
		if refused, err := s.vet(use); err != nil || refused {
			return err
		}
	}

	s.toolResult(use, s.execute(use), jsonObject{{"id", use.id}, {"permission_decision", jsonObject{
		{"decision", "accept"}, {"source", "config"}, {"reason_type", "classifier"}}}})
	return nil
}

// vet answers whether the session's tools or its PreToolUse hooks refused the call. It writes the refusal.
func (s *claudeSession) vet(use toolUse) (refused bool, err error) {
	if s.flags.tools.given && !slices.Contains(s.flags.tools.names, use.name) {
		s.toolResult(use, toolOutcome{content: "No such tool available: " + use.name, isError: true})
		return true, nil
	}

	refusal, err := s.preToolUse(use)
	if err != nil || refusal == "" {
		return false, err
	}

	s.hookRefusal(use, refusal)
	return true, nil
}

// preToolUse runs each PreToolUse hook that matches the call, and answers the first refusal, or "".
func (s *claudeSession) preToolUse(use toolUse) (string, error) {
	stdin, err := marshalJSON(jsonObject{
		{"session_id", s.call.SessionID},
		{"transcript_path", s.transcript},
		{"cwd", s.call.Dir},
		{"prompt_id", s.promptID},
		{"permission_mode", s.flags.permissionMode},
		{"effort", jsonObject{{"level", s.flags.effort}}},
		{"hook_event_name", "PreToolUse"},
		{"tool_name", use.name},
		{"tool_input", use.input},
		{"tool_use_id", use.id},
	})
	if err != nil {
		return "", err
	}

	refusal := ""
	for _, hook := range s.hooksFor(use.name) {
		said, err := s.runHook(hook, use, stdin)
		if err != nil {
			return "", err
		}

		refusal = cmp.Or(refusal, said)
	}

	return refusal, nil
}

func (s *claudeSession) hooksFor(tool string) []claudeHookCommand {
	var hooks []claudeHookCommand
	for _, group := range s.hooks {
		if group.matches(tool) {
			hooks = append(hooks, group.Hooks...)
		}
	}

	return hooks
}

// runHook runs a hook, and answers its refusal of the call, or "".
func (s *claudeSession) runHook(hook claudeHookCommand, use toolUse, stdin []byte) (string, error) {
	limit := 600 * time.Second
	if hook.Timeout > 0 {
		limit = time.Duration(hook.Timeout * float64(time.Second))
	}

	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", hook.Command)
	cmd.Dir, cmd.Env, cmd.Stdin = s.call.Dir, s.env, bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	exit, exited := errors.AsType[*exec.ExitError](err)
	switch {
	case err == nil:
		return hookAnswer(stdout.Bytes(), use)
	case exited && exit.ExitCode() == 2:
		return "PreToolUse:" + use.name + " hook error: [" + hook.Command + "]: " + stderr.String(), nil
	}

	return "", nil
}

// hookAnswer reads a hook's JSON answer, and answers its refusal of the call, or "".
func hookAnswer(stdout []byte, use toolUse) (string, error) {
	var answer struct {
		HookSpecificOutput struct {
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	trimmed := bytes.TrimSpace(stdout)
	if !bytes.HasPrefix(trimmed, []byte("{")) || !json.Valid(trimmed) {
		return "", nil
	}

	if err := json.Unmarshal(trimmed, &answer); err != nil {
		return "", fmt.Errorf("a hook answered %s: %w", trimmed, err)
	}

	out := answer.HookSpecificOutput
	switch out.PermissionDecision {
	case "", "allow":
		return "", nil
	case "deny":
		return "PreToolUse:" + use.name + " hook error: " + out.PermissionDecisionReason, nil
	}

	return "", fmt.Errorf("a hook answered permissionDecision %q, which the scripted claude does not carry out",
		out.PermissionDecision)
}

func (s *claudeSession) assistant(content jsonObject, usage json.RawMessage) {
	s.emit(jsonObject{
		{"type", "assistant"},
		{"message", jsonObject{
			{"model", claudeModel}, {"id", newID("msg_")}, {"type", "message"}, {"role", "assistant"},
			{"content", []jsonObject{content}}, {"stop_reason", nil}, {"stop_sequence", nil}, {"usage", usage},
		}},
		{"parent_tool_use_id", nil}, {"session_id", s.call.SessionID}, {"uuid", newUUID()}, {"timestamp", timestamp()},
	})
}

func timestamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func (s *claudeSession) toolResult(use toolUse, out toolOutcome, meta ...jsonObject) {
	line := jsonObject{
		{"type", "user"},
		{"message", jsonObject{{"role", "user"}, {"content", []jsonObject{{
			{"tool_use_id", use.id}, {"type", "tool_result"}, {"content", out.content}, {"is_error", out.isError},
		}}}}},
		{"parent_tool_use_id", nil}, {"session_id", s.call.SessionID}, {"uuid", newUUID()}, {"timestamp", timestamp()},
		{"tool_use_result", out.result()},
	}
	if len(meta) > 0 {
		line = line.set("tool_result_meta", meta)
	}

	s.emit(line)
}

func (s *claudeSession) hookRefusal(use toolUse, text string) {
	s.emit(jsonObject{{"type", "system"}, {"subtype", "permission_denied"}, {"tool_name", use.name},
		{"tool_use_id", use.id}, {"decision_reason_type", "hook"}, {"decision_reason", text}, {"message", text},
		{"uuid", newUUID()}, {"session_id", s.call.SessionID}})
	s.toolResult(use, toolOutcome{content: text, isError: true}, jsonObject{{"id", use.id},
		{"non_execution_kind", "permission-rule"}, {"permission_decision", jsonObject{
			{"decision", "reject"}, {"source", "hook"}, {"reason_type", "hook"}}}})
	s.deny(use)
}

func (s *claudeSession) deny(use toolUse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.denials = append(s.denials, jsonObject{{"tool_name", use.name}, {"tool_use_id", use.id},
		{"tool_input", use.input}})
}

func (s *claudeSession) execute(use toolUse) toolOutcome {
	switch use.name {
	case "Bash":
		return s.bash(use.input)
	case "Write":
		return writeTool(use.input)
	case "Edit":
		return editTool(use.input)
	}

	return readTool(use.input)
}

func writeTool(input jsonObject) toolOutcome {
	path := input.text("file_path")
	_, statErr := os.Stat(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return failed(err)
	}

	if err := os.WriteFile(path, []byte(input.text("content")), 0o644); err != nil {
		return failed(err)
	}

	if statErr == nil {
		return toolOutcome{content: "The file " + path + " has been updated successfully."}
	}

	return toolOutcome{content: "File created successfully at: " + path}
}

func editTool(input jsonObject) toolOutcome {
	path, old := input.text("file_path"), input.text("old_string")
	text, err := os.ReadFile(path)
	if err != nil {
		return failed(err)
	}

	replaceAll, _ := input.get("replace_all")
	switch n := strings.Count(string(text), old); {
	case old == "", n == 0:
		return toolOutcome{content: "String to replace not found in file.\nString: " + old, isError: true}
	case n > 1 && !rawBool(replaceAll):
		return toolOutcome{content: fmt.Sprintf("Found %d matches of the string to replace, but replace_all is false.",
			n), isError: true}
	}

	edited := strings.ReplaceAll(string(text), old, input.text("new_string"))
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		return failed(err)
	}

	return toolOutcome{content: "The file " + path + " has been updated successfully."}
}

func readTool(input jsonObject) toolOutcome {
	text, err := os.ReadFile(input.text("file_path"))
	if err != nil {
		return failed(err)
	}

	return toolOutcome{content: string(text)}
}

func (s *claudeSession) bash(input jsonObject) toolOutcome {
	ctx, cancel := context.WithTimeout(context.Background(), claudeCallLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", input.text("command"))
	cmd.Dir, cmd.Env = s.call.Dir, s.env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	said := slices.DeleteFunc([]string{stdout.String(), stderr.String()}, func(s string) bool { return s == "" })
	output := strings.TrimRight(strings.Join(said, "\n"), "\n")
	if err != nil {
		code := -1
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			code = exit.ExitCode()
		}

		return toolOutcome{content: fmt.Sprintf("Exit code %d\n%s", code, output), isError: true}
	}

	return toolOutcome{content: cmp.Or(output, "(Bash completed with no output)"), useResult: jsonObject{
		{"stdout", stdout.String()}, {"stderr", stderr.String()}, {"interrupted", false}, {"isImage", false},
		{"noOutputExpected", false},
	}}
}

func rawBool(v any) bool {
	var b bool
	raw, ok := v.(json.RawMessage)
	return ok && json.Unmarshal(raw, &b) == nil && b
}
