package scenariotest

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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

// ClaudeResult is the session's result line. CostUSD is what the run cost since the session's last result. An audit's
// result that is no error names Audit in place of Result.
type ClaudeResult struct {
	IsError        bool         `json:"is_error,omitempty"`
	Result         *string      `json:"result,omitempty"`
	Audit          *ClaudeAudit `json:"audit,omitempty"`
	APIErrorStatus *int         `json:"api_error_status,omitempty"`
	CostUSD        float64      `json:"cost_usd,omitempty"`
}

// ClaudeAudit is an audit's reply, which the scripted claude writes in the form of kiln's contract with the audit. It
// writes Prose, then each finding, then the line "read <Read> directories, found <Found>, reported <K>". K counts the
// findings. A Found of 0 counts them too, and a Found above K is a cap that cut the list.
type ClaudeAudit struct {
	Prose    string          `json:"prose,omitempty"`
	Findings []ClaudeFinding `json:"findings,omitempty"`
	Read     int             `json:"read"`
	Found    int             `json:"found,omitempty"`
}

// ClaudeFinding is one finding an audit lists. A Line of 0 names the whole file. Kind is a kind kiln's contract
// names: bug, impossible by design, weak test, false comment or test-only export. A field's text may hold a newline,
// and the reply indents each line it runs on to.
type ClaudeFinding struct {
	Path    string `json:"path"`
	Line    int    `json:"line,omitempty"`
	Kind    string `json:"kind"`
	Reaches string `json:"reaches,omitempty"`
	Chance  string `json:"chance,omitempty"`
	Harm    string `json:"harm,omitempty"`
	Design  string `json:"design,omitempty"`
	Patch   string `json:"patch,omitempty"`
	Proof   string `json:"proof,omitempty"`
}

// auditRole is the role of kiln's audit, whose reply follows kiln's contract.
const auditRole = "audit"

// auditKinds are the kinds of finding kiln's contract with the audit names.
var auditKinds = []string{"bug", "impossible by design", "weak test", "false comment", "test-only export"}

func (r ClaudeResult) check(role string) error {
	switch {
	case contracted(role, r) != (r.Audit != nil):
		return errors.New("an audit's result that is no error names audit, which the scripted claude writes in " +
			"the form of kiln's contract; no other result names audit")
	case r.Audit == nil:
		return nil
	case r.Result != nil:
		return errors.New("an audit's result names audit, never a result's text")
	}

	return r.Audit.check()
}

// contracted is whether r, a result of role, is an audit's reply, whose form kiln's contract sets.
func contracted(role string, r ClaudeResult) bool {
	return role == auditRole && !r.IsError
}

func (a ClaudeAudit) check() error {
	if a.Found != 0 && a.Found < len(a.Findings) {
		return fmt.Errorf("the audit found %d, fewer than the %d it lists", a.Found, len(a.Findings))
	}

	for i, f := range a.Findings {
		switch {
		case !slices.Contains(auditKinds, f.Kind):
			return fmt.Errorf("the audit's finding %d is a %q; kiln's contract names %s", i+1, f.Kind,
				strings.Join(auditKinds, ", "))
		case f.Path == "":
			return fmt.Errorf("the audit's finding %d names no path", i+1)
		case f.fields() == "":
			return fmt.Errorf("the audit's finding %d holds no field", i+1)
		}
	}

	return nil
}

// text is the reply a, in the form of kiln's contract.
func (a ClaudeAudit) text() string {
	var b strings.Builder
	if a.Prose != "" {
		b.WriteString(a.Prose + "\n\n")
	}

	for i, f := range a.Findings {
		place := f.Path
		if f.Line > 0 {
			place += ":" + strconv.Itoa(f.Line)
		}

		fmt.Fprintf(&b, "%d. %s  %s\n%s\n", i+1, place, f.Kind, f.fields())
	}

	directories := "directories"
	if a.Read == 1 {
		directories = "directory"
	}

	fmt.Fprintf(&b, "read %d %s, found %d, reported %d", a.Read, directories, cmp.Or(a.Found, len(a.Findings)),
		len(a.Findings))
	return b.String()
}

// fields are f's fields, a line each, in the contract's order, with each line a field runs on to indented.
func (f ClaudeFinding) fields() string {
	var b strings.Builder
	for _, field := range [][2]string{{"reaches", f.Reaches}, {"chance", f.Chance}, {"harm", f.Harm},
		{"design", f.Design}, {"patch", f.Patch}, {"proof", f.Proof}} {
		if field[1] != "" {
			fmt.Fprintf(&b, "%-10s %s\n", field[0], strings.ReplaceAll(field[1], "\n", "\n           "))
		}
	}

	return b.String()
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

func (s ClaudeStep) check(role string) error {
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

	if s.Result != nil {
		return s.Result.check(role)
	}

	return nil
}

// readClaudeScript decodes the script in work, and refuses a field the scripted claude does not know.
func readClaudeScript(work string) (ClaudeScript, error) {
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

	return script, nil
}

// Check answers the error of a step of s that the scripted claude would refuse, or nil for none. A test that calls
// it first fails on that step's error, not on a session that ended with no result.
func (s ClaudeScript) Check() error {
	for role, attempts := range s {
		for attempt, calls := range attempts {
			for call, steps := range calls {
				for i, step := range steps {
					if err := step.check(role); err != nil {
						return fmt.Errorf("%s: role %q, attempt %d, call %d, step %d: %w", ClaudeScriptFile, role,
							attempt, call, i+1, err)
					}
				}
			}
		}
	}

	return nil
}

// checkScript refuses the script in work, when work holds one, before a scenario runs a bad step.
func checkScript(work string) error {
	if _, err := os.Stat(filepath.Join(work, ClaudeScriptFile)); errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	script, err := readClaudeScript(work)
	if err != nil {
		return err
	}

	return script.Check()
}

func loadClaudeScript(work string, c ClaudeCall) ([]ClaudeStep, error) {
	role, attempt, call := c.Role, c.Attempt, c.Call
	script, err := readClaudeScript(work)
	if err != nil {
		return nil, err
	}

	steps, ok := script[role][attempt][call]
	if !ok {
		return nil, fmt.Errorf("%s has no steps for role %q, attempt %d, call %d", ClaudeScriptFile, role, attempt,
			call)
	}

	for i, step := range steps {
		if err := step.check(role); err != nil {
			return nil, fmt.Errorf("%s: role %q, attempt %d, call %d, step %d: %w", ClaudeScriptFile, role, attempt,
				call, i+1, err)
		}
	}

	return steps, nil
}

var errNoWork = errors.New("scripted claude: WORK is not set, so there is no script to follow")
