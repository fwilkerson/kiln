package kiln

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// guarded says why a build of spec may not change rel, a path within the repository. It says "" if the build may. The
// hook and the gate both ask it. It reads each name as APFS does, folding case, so SPECS/x.md is specs/x.md.
func guarded(rel, spec string) string {
	names := strings.Split(path.Clean(strings.TrimPrefix(rel, "/")), "/")
	for _, name := range names {
		if fold(name) == fold(".git") {
			return rel + " is in a git directory, which only git and kiln write"
		}
	}

	switch {
	case fold(names[0]) == fold("specs") && (len(names) != 2 || fold(names[1]) != fold(spec+amendSuffix)):
		return fmt.Sprintf("%s is under specs/, which holds what the developer approved: a build writes there only %s",
			rel, amendPath(spec))
	case fold(names[0]) == fold(".claude") && len(names) == 2 && settingsFile(names[1]):
		return rel + " is a Claude Code settings file, which stays the developer's"
	}

	return ""
}

func settingsFile(name string) bool {
	matched, err := path.Match(fold("settings*.json"), fold(name))
	return err == nil && matched
}

// hookCall is what Claude Code gives a PreToolUse hook on its standard input.
type hookCall struct {
	Tool  string `json:"tool_name"`
	Cwd   string `json:"cwd"`
	Input struct {
		File     string `json:"file_path"`
		Notebook string `json:"notebook_path"`
	} `json:"tool_input"`
}

// Hook answers the PreToolUse call on in, for a session building spec in the worktree at place. It refuses a write
// outside place, and one to a path [guarded] names. It writes its refusal to out, as Claude Code reads it. It reads no
// shell command: the gate's own check finds a guarded path a command changed.
func Hook(in io.Reader, out io.Writer, spec, place string) error {
	var c hookCall
	if err := json.NewDecoder(in).Decode(&c); err != nil {
		return fmt.Errorf("the hook's input: %w", err)
	}

	why := refusal(c, spec, place)
	if why == "" {
		return nil
	}

	return json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]string{
		"hookEventName":            "PreToolUse",
		"permissionDecision":       "deny",
		"permissionDecisionReason": why,
	}})
}

func refusal(c hookCall, spec, place string) string {
	target := c.Input.File
	if target == "" {
		target = c.Input.Notebook
	}

	if target == "" {
		return c.Tool + " names no file, so the hook cannot tell where it writes"
	}

	cwd := place
	if c.Cwd != "" {
		cwd = resolve(place, c.Cwd)
	}

	rest, ok := inside(resolve(cwd, target), canonical(place))
	if !ok {
		return fmt.Sprintf("%s is outside this build's worktree, %s: a build writes only there", target, place)
	}

	return guarded(rest, spec)
}

func canonical(p string) string {
	p = path.Clean(p)
	// macOS links /var, /tmp and /etc to /private/...: kiln and Claude Code may spell a directory either way.
	for _, linked := range []string{"/var", "/tmp", "/etc"} {
		if rest, ok := strings.CutPrefix(p, "/private"+linked); ok && (rest == "" || rest[0] == '/') {
			return linked + rest
		}
	}

	return p
}

func resolve(base, target string) string {
	if path.IsAbs(target) {
		return canonical(target)
	}

	return canonical(path.Join(base, target))
}

// fold maps each rune to the least of its case orbit, as APFS, where builds run, compares names.
func fold(s string) string {
	return strings.Map(func(r rune) rune {
		least := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			least = min(least, f)
		}

		return least
	}, s)
}

// inside is whether p is d or under it, as the volume reads names. rest keeps p's own spelling of what follows d.
func inside(p, d string) (rest string, ok bool) {
	if d == "/" {
		return strings.TrimPrefix(p, "/"), true
	}

	i := 0
	for range utf8.RuneCountInString(d) {
		if i == len(p) {
			return "", false
		}

		_, size := utf8.DecodeRuneInString(p[i:])
		i += size
	}

	if fold(p[:i]) != fold(d) || (i < len(p) && p[i] != '/') {
		return "", false
	}

	return strings.TrimPrefix(p[i:], "/"), true
}
