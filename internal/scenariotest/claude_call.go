package scenariotest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type claudeFlags struct {
	print, verbose bool
	sessionID      string
	resume         string
	roleFile       string
	tools          []string
	toolsGiven     bool
	effort         string
	permissionMode string
	settingSources []string
	sourcesGiven   bool
	settings       string
	outputFormat   string
}

var claudeValueFlags = map[string]bool{
	"--session-id": true, "--resume": true, "-r": true,
	"--append-system-prompt-file": true, "--append-system-prompt": true, "--system-prompt": true,
	"--system-prompt-file": true, "--tools": true, "--allowedTools": true, "--disallowedTools": true, "--add-dir": true,
	"--model": true, "--fallback-model": true, "--effort": true, "--permission-mode": true,
	"--setting-sources": true, "--mcp-config": true, "--settings": true, "--json-schema": true,
	"--output-format": true, "--input-format": true, "--max-turns": true, "--max-budget-usd": true,
	"--agents": true, "--agent": true,
}

func parseClaudeFlags(args []string) (claudeFlags, error) {
	f := claudeFlags{effort: "medium", permissionMode: "default", outputFormat: "text"}
	for i := 0; i < len(args); i++ {
		name, value, inline := strings.Cut(args[i], "=")
		if !strings.HasPrefix(name, "--") {
			name, inline = args[i], false
		}

		if !claudeValueFlags[name] {
			f = f.withSwitch(name)
			continue
		}

		if !inline {
			if i+1 == len(args) {
				return f, fmt.Errorf("error: option '%s' argument missing", name)
			}

			i++
			value = args[i]
		}

		f = f.withValue(name, value)
	}

	return f, nil
}

func (f claudeFlags) withSwitch(name string) claudeFlags {
	switch name {
	case "-p", "--print":
		f.print = true
	case "--verbose":
		f.verbose = true
	}

	return f
}

func (f claudeFlags) withValue(name, value string) claudeFlags {
	switch name {
	case "--session-id":
		f.sessionID = value
	case "--resume", "-r":
		f.resume = value
	case "--append-system-prompt-file":
		f.roleFile = value
	case "--tools":
		f.tools, f.toolsGiven = splitList(value), true
	case "--effort":
		f.effort = value
	case "--permission-mode":
		f.permissionMode = value
	case "--setting-sources":
		f.settingSources, f.sourcesGiven = splitList(value), true
	case "--settings":
		f.settings = value
	case "--output-format":
		f.outputFormat = value
	}

	return f
}

func splitList(s string) []string {
	var out []string
	for item := range strings.FieldsFuncSeq(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		out = append(out, item)
	}

	return out
}

// claudeRole names a call's role by the file its system prompt was appended from. kiln names the file for the role.
func claudeRole(roleFile string) string {
	if roleFile == "" {
		return "none"
	}

	return strings.TrimSuffix(filepath.Base(roleFile), filepath.Ext(roleFile))
}

type claudeCalls struct {
	dir string
}

func (c claudeCalls) locked(do func() error) error {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}

	lock, err := lockFile(filepath.Join(c.dir, ".lock"))
	if err != nil {
		return err
	}

	defer lock.Close()

	return do()
}

type claudeSessionOwner struct {
	Role    string `json:"role"`
	Attempt int    `json:"attempt"`
}

func (c claudeCalls) owner(sessionID string) (claudeSessionOwner, bool) {
	var owner claudeSessionOwner
	if sessionID == "" || sessionID != filepath.Base(sessionID) || strings.HasPrefix(sessionID, ".") {
		return owner, false
	}

	raw, err := os.ReadFile(filepath.Join(c.dir, ".sessions", sessionID))
	return owner, err == nil && json.Unmarshal(raw, &owner) == nil
}

// add numbers a call. A resume of a known session is that session's next call. Any other call is a new attempt.
func (c claudeCalls) add(call *ClaudeCall) (dir, refused string, err error) {
	err = c.locked(func() error {
		owner, known := c.owner(call.SessionID)
		switch {
		case call.Resume && known:
			call.Role, call.Attempt = owner.Role, owner.Attempt
			call.Call = numbered(filepath.Join(c.dir, call.Role, strconv.Itoa(call.Attempt))) + 1
		case call.Resume:
			refused = "No conversation found with session ID: " + call.SessionID
		}

		if call.Attempt == 0 {
			call.Attempt, call.Call = numbered(filepath.Join(c.dir, call.Role))+1, 1
		}

		dir = filepath.Join(c.dir, call.Role, strconv.Itoa(call.Attempt), strconv.Itoa(call.Call))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}

		raw, err := json.MarshalIndent(call, "", "  ")
		if err != nil {
			return err
		}

		return os.WriteFile(filepath.Join(dir, ClaudeCallFile), append(raw, '\n'), 0o644)
	})

	return dir, refused, err
}

// open records a new session as its role's, and refuses an ID already in use.
func (c claudeCalls) open(call ClaudeCall) (refused string, err error) {
	err = c.locked(func() error {
		if _, known := c.owner(call.SessionID); known {
			refused = "Error: Session ID " + call.SessionID + " is already in use."
			return nil
		}

		raw, err := json.Marshal(claudeSessionOwner{call.Role, call.Attempt})
		if err != nil {
			return err
		}

		if err := os.MkdirAll(filepath.Join(c.dir, ".sessions"), 0o755); err != nil {
			return err
		}

		return os.WriteFile(filepath.Join(c.dir, ".sessions", call.SessionID), raw, 0o644)
	})

	return refused, err
}

func recordCall(dir string, call ClaudeCall, flags claudeFlags, prompt []byte) error {
	args := make([]string, len(call.Args))
	for i, a := range call.Args {
		args[i] = strings.ReplaceAll(a, "\n", `\n`)
	}

	files := map[string][]byte{ClaudeArgsFile: []byte(strings.Join(args, "\n") + "\n"), ClaudePromptFile: prompt}
	if flags.settings != "" {
		settings, err := os.ReadFile(flags.settings)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		files[ClaudeSettingsFile] = settings
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			return err
		}
	}

	return nil
}

func lockFile(path string) (*os.File, error) {
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}

	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		lock.Close()
		return nil, err
	}

	return lock, nil
}

func numbered(dir string) int {
	entries, _ := os.ReadDir(dir)
	n := 0
	for _, e := range entries {
		if i, err := strconv.Atoi(e.Name()); err == nil && e.IsDir() {
			n = max(n, i)
		}
	}

	return n
}
