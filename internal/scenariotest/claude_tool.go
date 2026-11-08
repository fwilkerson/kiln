package scenariotest

import (
	"bytes"
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

func loadClaudeSettings(flags claudeFlags, dir, home string) (claudeSettings, error) {
	sources := flags.settingSources
	if !flags.sourcesGiven {
		sources = []string{"user", "project", "local"}
	}

	var texts [][]byte
	for _, source := range sources {
		path := map[string]string{
			"user":    filepath.Join(home, ".claude", "settings.json"),
			"project": filepath.Join(dir, ".claude", "settings.json"),
			"local":   filepath.Join(dir, ".claude", "settings.local.json"),
		}[source]
		text, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) || path == "" {
			continue
		}

		if err != nil {
			return claudeSettings{}, err
		}

		texts = append(texts, text)
	}

	if given := flags.settings; given != "" {
		text := []byte(given)
		if !strings.HasPrefix(strings.TrimSpace(given), "{") {
			var err error
			if text, err = readFile(given); err != nil {
				return claudeSettings{}, err
			}
		}

		texts = append(texts, text)
	}

	return mergeClaudeSettings(texts)
}

func mergeClaudeSettings(texts [][]byte) (claudeSettings, error) {
	merged := claudeSettings{env: map[string]string{}}
	for _, text := range texts {
		var file struct {
			Env   map[string]string            `json:"env"`
			Hooks map[string][]claudeHookGroup `json:"hooks"`
		}
		if err := json.Unmarshal(text, &file); err != nil {
			return claudeSettings{}, fmt.Errorf("a settings file: %w", err)
		}

		maps.Copy(merged.env, file.Env)
		for _, group := range file.Hooks["PreToolUse"] {
			for _, hook := range group.Hooks {
				if hook.Type != "command" {
					return claudeSettings{}, fmt.Errorf("a PreToolUse hook of type %q: the scripted claude runs only "+
						"command hooks", hook.Type)
				}
			}

			merged.hooks = append(merged.hooks, group)
		}
	}

	return merged, nil
}

var plainMatcher = regexp.MustCompile(`^[A-Za-z0-9_\- ,|]+$`)

func hookMatches(matcher, tool string) bool {
	switch {
	case matcher == "" || matcher == "*":
		return true
	case plainMatcher.MatchString(matcher):
		for name := range strings.FieldsFuncSeq(matcher, func(r rune) bool { return r == '|' || r == ',' }) {
			if strings.TrimSpace(name) == tool {
				return true
			}
		}

		return false
	}

	re, err := regexp.Compile(matcher)
	return err == nil && re.MatchString(tool)
}

type hookDecision struct {
	refusal string
}

func (s *claudeSession) preToolUse(tool string, input jsonObject, id string) (hookDecision, error) {
	stdin, err := marshalJSON(jsonObject{
		{"session_id", s.call.SessionID},
		{"transcript_path", s.transcript},
		{"cwd", s.call.Dir},
		{"prompt_id", s.promptID},
		{"permission_mode", s.flags.permissionMode},
		{"effort", jsonObject{{"level", s.flags.effort}}},
		{"hook_event_name", "PreToolUse"},
		{"tool_name", tool},
		{"tool_input", input},
		{"tool_use_id", id},
	})
	if err != nil {
		return hookDecision{}, err
	}

	var decision hookDecision
	for _, group := range s.hooks {
		if !hookMatches(group.Matcher, tool) {
			continue
		}

		for _, hook := range group.Hooks {
			d, err := s.runHook(hook, tool, stdin)
			if err != nil {
				return hookDecision{}, err
			}

			if decision.refusal == "" {
				decision.refusal = d.refusal
			}
		}
	}

	return decision, nil
}

func (s *claudeSession) runHook(hook claudeHookCommand, tool string, stdin []byte) (hookDecision, error) {
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
		return hookAnswer(stdout.Bytes(), tool)
	case exited && exit.ExitCode() == 2:
		why := "PreToolUse:" + tool + " hook error: [" + hook.Command + "]: " + stderr.String()
		return hookDecision{refusal: why}, nil
	}

	return hookDecision{}, nil
}

func hookAnswer(stdout []byte, tool string) (hookDecision, error) {
	var answer struct {
		HookSpecificOutput struct {
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	trimmed := bytes.TrimSpace(stdout)
	if !bytes.HasPrefix(trimmed, []byte("{")) || !json.Valid(trimmed) {
		return hookDecision{}, nil
	}

	if err := json.Unmarshal(trimmed, &answer); err != nil {
		return hookDecision{}, fmt.Errorf("a hook answered %s: %w", trimmed, err)
	}

	out := answer.HookSpecificOutput
	switch out.PermissionDecision {
	case "", "allow":
		return hookDecision{}, nil
	case "deny":
		return hookDecision{refusal: "PreToolUse:" + tool + " hook error: " + out.PermissionDecisionReason}, nil
	}

	return hookDecision{}, fmt.Errorf("a hook answered permissionDecision %q, which the scripted claude does not "+
		"carry out", out.PermissionDecision)
}

func (s *claudeSession) tool(t *ClaudeTool) error {
	input, _ := parseObject(t.Input)
	if v, ok := input.get("file_path"); ok {
		input = input.set("file_path", s.path(rawString(v)))
	}

	id := newID("toolu_")
	usage := json.RawMessage(`{"input_tokens":2,"cache_read_input_tokens":0,"output_tokens":16}`)
	s.assistant(jsonObject{{"type", "tool_use"}, {"id", id}, {"name", t.Name}, {"input", input}}, usage)
	if !t.Raw && s.flags.toolsGiven && !slices.Contains(s.flags.tools, t.Name) {
		s.toolResult(id, "No such tool available: "+t.Name, true, nil)
		return nil
	}

	if !t.Raw {
		decision, err := s.preToolUse(t.Name, input, id)
		if err != nil {
			return err
		}

		if decision.refusal != "" {
			s.hookRefusal(t.Name, id, input, decision.refusal)
			return nil
		}
	}

	content, isError, useResult := s.execute(t.Name, input)
	s.toolResult(id, content, isError, useResult, jsonObject{{"id", id}, {"permission_decision", jsonObject{
		{"decision", "accept"}, {"source", "config"}, {"reason_type", "classifier"}}}})
	return nil
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

func (s *claudeSession) toolResult(id, content string, isError bool, useResult any, meta ...jsonObject) {
	if useResult == nil {
		useResult = content
		if isError {
			useResult = "Error: " + content
		}
	}

	line := jsonObject{
		{"type", "user"},
		{"message", jsonObject{{"role", "user"}, {"content", []jsonObject{{
			{"tool_use_id", id}, {"type", "tool_result"}, {"content", content}, {"is_error", isError},
		}}}}},
		{"parent_tool_use_id", nil}, {"session_id", s.call.SessionID}, {"uuid", newUUID()}, {"timestamp", timestamp()},
		{"tool_use_result", useResult},
	}
	if len(meta) > 0 {
		line = line.set("tool_result_meta", meta)
	}

	s.emit(line)
}

func rejected(id, source, reason string) jsonObject {
	return jsonObject{{"id", id}, {"non_execution_kind", "permission-rule"}, {"permission_decision", jsonObject{
		{"decision", "reject"}, {"source", source}, {"reason_type", reason}}}}
}

func (s *claudeSession) hookRefusal(tool, id string, input jsonObject, text string) {
	s.toolResult(id, text, true, nil, rejected(id, "hook", "hook"))
	s.deny(tool, id, input)
}

func (s *claudeSession) deny(tool, id string, input jsonObject) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.denials = append(s.denials, jsonObject{{"tool_name", tool}, {"tool_use_id", id}, {"tool_input", input}})
}

func (s *claudeSession) execute(tool string, input jsonObject) (content string, isError bool, useResult any) {
	str := func(key string) string { v, _ := input.get(key); return rawString(v) }
	switch tool {
	case "Bash":
		return s.bash(str("command"))
	case "Write":
		path := str("file_path")
		_, statErr := os.Stat(path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err.Error(), true, nil
		}

		if err := os.WriteFile(path, []byte(str("content")), 0o644); err != nil {
			return err.Error(), true, nil
		}

		if statErr == nil {
			return "The file " + path + " has been updated successfully.", false, nil
		}

		return "File created successfully at: " + path, false, nil
	case "Edit":
		replaceAll, _ := input.get("replace_all")
		return edit(str("file_path"), str("old_string"), str("new_string"), rawBool(replaceAll))
	}

	text, err := os.ReadFile(str("file_path"))
	if err != nil {
		return err.Error(), true, nil
	}

	return string(text), false, nil
}

func edit(path, old, replacement string, all bool) (content string, isError bool, useResult any) {
	text, err := os.ReadFile(path)
	if err != nil {
		return err.Error(), true, nil
	}

	switch n := strings.Count(string(text), old); {
	case old == "" || n == 0:
		return "String to replace not found in file.\nString: " + old, true, nil
	case n > 1 && !all:
		return fmt.Sprintf("Found %d matches of the string to replace, but replace_all is false.", n), true, nil
	}

	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(text), old, replacement)), 0o644); err != nil {
		return err.Error(), true, nil
	}

	return "The file " + path + " has been updated successfully.", false, nil
}

func (s *claudeSession) bash(command string) (content string, isError bool, useResult any) {
	ctx, cancel := context.WithTimeout(context.Background(), claudeCallLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
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

		return fmt.Sprintf("Exit code %d\n%s", code, output), true, nil
	}

	if output == "" {
		output = "(Bash completed with no output)"
	}

	return output, false, jsonObject{
		{"stdout", stdout.String()}, {"stderr", stderr.String()}, {"interrupted", false}, {"isImage", false}, {"noOutputExpected", false},
	}
}

func rawBool(v any) bool {
	var b bool
	raw, ok := v.(json.RawMessage)
	return ok && json.Unmarshal(raw, &b) == nil && b
}
