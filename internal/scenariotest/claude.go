package scenariotest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// The scripted claude follows $WORK/claude.json, and records each call under $WORK/.claude-calls.
const (
	ClaudeScriptFile = "claude.json"
	ClaudeCallsDir   = ".claude-calls"
)

// Each call's record holds these files. They are the call, its arguments, its prompt, and its appended system prompt.
// Then come the settings it was given, and the stream it wrote.
const (
	ClaudeCallFile     = "call.json"
	ClaudeArgsFile     = "args"
	ClaudePromptFile   = "prompt"
	ClaudeSystemFile   = "system"
	ClaudeSettingsFile = "settings.json"
	ClaudeStreamFile   = "stream.jsonl"
)

// ClaudeScript holds each call's steps, by its role, its attempt and its call. The role is the base name of the
// file given to --append-system-prompt-file, such as "build". An attempt is one session, and resuming the session
// starts the attempt's next call.
type ClaudeScript map[string]map[int]map[int][]ClaudeStep

// ClaudeStep is one thing the scripted claude does. It names exactly one of its fields.
type ClaudeStep struct {
	// Write writes files, by their paths in the session's directory, past every tool and hook.
	Write map[string]string `json:"write,omitempty"`
	// Tool calls a tool as the model would, through the session's PreToolUse hooks.
	Tool *ClaudeTool `json:"tool,omitempty"`
	// Limit writes a usage limit's rate_limit_event.
	Limit  *ClaudeLimit  `json:"limit,omitempty"`
	Result *ClaudeResult `json:"result,omitempty"`
	// Exit ends the process with its code.
	Exit *int `json:"exit,omitempty"`
}

// ClaudeTool is a call of Bash, Write, Edit or Read. Raw skips the hooks, as a tool past every layer would.
type ClaudeTool struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
	Raw   bool            `json:"raw,omitempty"`
	// Usage is the usage of the message that makes the call, as Claude Code writes it. Without it, the usage is small.
	Usage json.RawMessage `json:"usage,omitempty"`
}

// ClaudeLimit is a usage limit. Status is "rejected" unless it says otherwise, and it resets ResetsIn from now.
// SevenDay, from 0 to 1, adds the use of the 7-day limit. A status of "allowed" with SevenDay is a reading of it.
type ClaudeLimit struct {
	Status       string         `json:"status,omitempty"`
	ResetsIn     ClaudeDuration `json:"resets_in,omitempty"`
	UsingOverage bool           `json:"using_overage,omitempty"`
	SevenDay     *float64       `json:"seven_day,omitempty"`
}

// ClaudeResult is the session's result line. CostUSD is what the run cost since the session's last result.
type ClaudeResult struct {
	IsError        bool    `json:"is_error,omitempty"`
	Result         *string `json:"result,omitempty"`
	APIErrorStatus *int    `json:"api_error_status,omitempty"`
	CostUSD        float64 `json:"cost_usd,omitempty"`
}

// ClaudeDuration is a duration written as a string, such as "3s".
type ClaudeDuration time.Duration

func (d ClaudeDuration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *ClaudeDuration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("a duration is a string such as \"3s\": %w", err)
	}

	parsed, err := time.ParseDuration(s)
	*d = ClaudeDuration(parsed)
	return err
}

// ClaudeCall is a call's record, in its call.json.
type ClaudeCall struct {
	Role      string   `json:"role"`
	Attempt   int      `json:"attempt"`
	Call      int      `json:"call"`
	SessionID string   `json:"session_id"`
	Resume    bool     `json:"resume"`
	Dir       string   `json:"dir"`
	Args      []string `json:"args"`
}

var claudeTools = []string{"Bash", "Write", "Edit", "Read"}

func (s ClaudeStep) check() error {
	set := 0
	for _, isSet := range []bool{s.Write != nil, s.Tool != nil, s.Limit != nil, s.Result != nil, s.Exit != nil} {
		if isSet {
			set++
		}
	}

	if set != 1 {
		return fmt.Errorf("a step names exactly one of write, tool, limit, result and exit; this one names %d", set)
	}

	if s.Tool != nil {
		if !slices.Contains(claudeTools, s.Tool.Name) {
			return fmt.Errorf("the tool %q is not one the scripted claude carries out: %s", s.Tool.Name,
				strings.Join(claudeTools, ", "))
		}

		if _, err := parseObject(s.Tool.Input); err != nil {
			return fmt.Errorf("the %s call's input: %w", s.Tool.Name, err)
		}
	}

	return nil
}

func loadClaudeScript(work string, c ClaudeCall) ([]ClaudeStep, error) {
	role, attempt, call := c.Role, c.Attempt, c.Call
	raw, err := readFile(filepath.Join(work, ClaudeScriptFile))
	if err != nil {
		return nil, err
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var script ClaudeScript
	if err := dec.Decode(&script); err != nil {
		return nil, fmt.Errorf("%s: %w", ClaudeScriptFile, err)
	}

	steps, ok := script[role][attempt][call]
	if !ok {
		return nil, fmt.Errorf("%s has no steps for role %q, attempt %d, call %d", ClaudeScriptFile, role, attempt,
			call)
	}

	for i, step := range steps {
		if err := step.check(); err != nil {
			return nil, fmt.Errorf("%s: role %q, attempt %d, call %d, step %d: %w", ClaudeScriptFile, role, attempt,
				call, i+1, err)
		}
	}

	return steps, nil
}

var errNoWork = errors.New("scripted claude: WORK is not set, so there is no script to follow")
