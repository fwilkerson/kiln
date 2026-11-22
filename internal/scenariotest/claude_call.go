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
	tools          claudeList
	effort         string
	permissionMode string
	settingSources claudeList
	settings       string
	outputFormat   string
	// refusal is what Claude Code says to the arguments it could not parse, or "".
	refusal string
}

// claudeList is a flag's list of names. Given tells an empty list from a flag the call left out.
type claudeList struct {
	names []string
	given bool
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

func parseClaudeFlags(args []string) claudeFlags {
	f := claudeFlags{effort: "medium", permissionMode: "default", outputFormat: "text"}
	for i := 0; i < len(args); i++ {
		name, value, inline := cutFlag(args[i])
		if claudeValueFlags[name] && !inline {
			if i++; i == len(args) {
				f.refusal = fmt.Sprintf("error: option '%s' argument missing", name)
				return f
			}

			value = args[i]
		}

		f.set(name, value)
	}

	return f
}

// cutFlag splits an argument such as --name=value. Only a flag with two hyphens takes its value inline.
func cutFlag(arg string) (name, value string, inline bool) {
	name, value, inline = strings.Cut(arg, "=")
	if !inline || !strings.HasPrefix(name, "--") {
		return arg, "", false
	}

	return name, value, true
}

// set keeps the flag name, if the scripted claude reads it. A switch ignores value.
func (f *claudeFlags) set(name, value string) {
	switches := map[string]*bool{"-p": &f.print, "--print": &f.print, "--verbose": &f.verbose}
	values := map[string]*string{"--session-id": &f.sessionID, "--resume": &f.resume, "-r": &f.resume,
		"--append-system-prompt-file": &f.roleFile, "--effort": &f.effort, "--permission-mode": &f.permissionMode,
		"--settings": &f.settings, "--output-format": &f.outputFormat}
	lists := map[string]*claudeList{"--tools": &f.tools, "--setting-sources": &f.settingSources}
	if p, ok := switches[name]; ok {
		*p = true
	}

	if p, ok := values[name]; ok {
		*p = value
	}

	if p, ok := lists[name]; ok {
		*p = claudeList{names: strings.FieldsFunc(value, listSeparator), given: true}
	}
}

func listSeparator(r rune) bool { return r == ',' || r == ' ' }

// role names a call's role by the file its system prompt was appended from. kiln names the file for the role.
func (f *claudeFlags) role() string {
	if f.roleFile == "" {
		return "none"
	}

	return strings.TrimSuffix(filepath.Base(f.roleFile), filepath.Ext(f.roleFile))
}

type claudeCalls struct {
	dir string
}

func (c claudeCalls) locked(do func() error) error {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}

	lock, err := os.OpenFile(filepath.Join(c.dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}

	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}

	return do()
}

type claudeSessionOwner struct {
	Role    string `json:"role"`
	Attempt int    `json:"attempt"`
}

// owner answers the role and attempt that opened call's session, if one did.
func (c claudeCalls) owner(call ClaudeCall) (claudeSessionOwner, bool) {
	var owner claudeSessionOwner
	id := call.SessionID
	switch {
	case id == "", id != filepath.Base(id), strings.HasPrefix(id, "."):
		return owner, false
	}

	raw, err := os.ReadFile(filepath.Join(c.dir, ".sessions", id))
	return owner, err == nil && json.Unmarshal(raw, &owner) == nil
}

// add numbers a call, and answers the directory of its record. It also answers what Claude Code says to the call's
// session ID, when it refuses it.
func (c claudeCalls) add(call *ClaudeCall) (dir, refused string, err error) {
	err = c.locked(func() error {
		refused = c.number(call)
		dir = c.path(*call)
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

// number sets call's attempt and call. Resuming a known session makes that session's next call. Any other call is a
// new attempt. It answers what Claude Code says when asked to resume an unknown session, or to a session ID in use.
func (c claudeCalls) number(call *ClaudeCall) (refused string) {
	owner, known := c.owner(*call)
	switch {
	case call.Resume && known:
		call.Role, call.Attempt = owner.Role, owner.Attempt
		call.Call = c.next(*call)
		return ""
	case call.Resume:
		refused = "No conversation found with session ID: " + call.SessionID
	case known:
		refused = "Error: Session ID " + call.SessionID + " is already in use."
	}

	call.Attempt, call.Call = c.next(*call), 1
	return refused
}

// path is the directory of call's record. While call has no attempt, or no call, it is the directory above.
func (c claudeCalls) path(call ClaudeCall) string {
	parts := []string{c.dir, call.Role}
	for _, n := range []int{call.Attempt, call.Call} {
		if n == 0 {
			break
		}

		parts = append(parts, strconv.Itoa(n))
	}

	return filepath.Join(parts...)
}

// next answers the number after the highest one numbering a directory in c.path(call).
func (c claudeCalls) next(call ClaudeCall) int {
	entries, _ := os.ReadDir(c.path(call))
	n := 0
	for _, e := range entries {
		if i, err := strconv.Atoi(e.Name()); err == nil && e.IsDir() {
			n = max(n, i)
		}
	}

	return n + 1
}

// open records a new session as its role's, and refuses an ID already in use.
func (c claudeCalls) open(call ClaudeCall) (refused string, err error) {
	err = c.locked(func() error {
		if _, known := c.owner(call); known {
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

// keepRecord numbers the call and writes its record. It keeps in s.taken what Claude Code says to a session ID in use.
func (s *claudeSession) keepRecord() error {
	var err error
	s.record, s.taken, err = s.calls.add(&s.call)
	if err != nil {
		return err
	}

	files, err := s.recordFiles()
	if err != nil {
		return err
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(s.record, name), content, 0o644); err != nil {
			return err
		}
	}

	return nil
}

// recordFiles holds the call's arguments, one a line, its prompt, and the files --append-system-prompt-file and
// --settings name. A missing file is recorded empty.
func (s *claudeSession) recordFiles() (map[string][]byte, error) {
	args := make([]string, len(s.call.Args))
	for i, a := range s.call.Args {
		args[i] = strings.ReplaceAll(a, "\n", `\n`)
	}

	files := map[string][]byte{ClaudeArgsFile: []byte(strings.Join(args, "\n") + "\n"), ClaudePromptFile: s.prompt}
	named := map[string]string{ClaudeSystemFile: s.flags.roleFile, ClaudeSettingsFile: s.flags.settings}
	for name, path := range named {
		if path == "" {
			continue
		}

		content, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}

		files[name] = content
	}

	return files, nil
}
