package kiln

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fwilkerson/kiln/internal/git"
)

// guard is what a build of spec, in the worktree at place, may not change. The hook and the gate both ask it.
type guard struct {
	spec  Spec
	place git.Worktree
}

// guarded says why the build may not change rel, a path within the repository. It says "" if the build may. It reads
// each name as APFS does, folding case, so SPECS/x.md is specs/x.md.
func (g guard) guarded(rel string) string {
	folded := fold(path.Clean(strings.TrimPrefix(rel, "/")))
	names := strings.Split(folded, "/")
	settings, _ := path.Match(fold(".claude/settings*.json"), folded)
	switch {
	case slices.Contains(names, fold(".git")):
		return rel + " is in a git directory, which only git and kiln write"
	case names[0] == fold("specs") && folded != fold(g.spec.amendPath()):
		return fmt.Sprintf("%s is under specs/, which holds what the developer approved: a build writes there only %s",
			rel, g.spec.amendPath())
	case settings:
		return rel + " is a Claude Code settings file, which stays the developer's"
	}

	return ""
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
func Hook(in io.Reader, out io.Writer, spec Spec, place git.Worktree) error {
	var c hookCall
	if err := json.NewDecoder(in).Decode(&c); err != nil {
		return fmt.Errorf("the hook's input: %w", err)
	}

	why := guard{spec: spec, place: place}.refusal(c)
	if why == "" {
		return nil
	}

	return json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]string{
		"hookEventName":            "PreToolUse",
		"permissionDecision":       "deny",
		"permissionDecisionReason": why,
	}})
}

func (g guard) refusal(c hookCall) string {
	target := cmpOr(c.Input.File, c.Input.Notebook)
	if target == "" {
		return c.Tool + " names no file, so the hook cannot tell where it writes"
	}

	rest, ok := g.inside(resolve(resolve(string(g.place), c.Cwd), target))
	if !ok {
		return fmt.Sprintf("%s is outside this build's worktree, %s: a build writes only there", target, g.place)
	}

	return g.guarded(rest)
}

func canonical(p string) string {
	p = path.Clean(p)
	// macOS links /var, /tmp and /etc to /private/...: kiln and Claude Code may spell a directory either way.
	for _, linked := range []string{"/var", "/tmp", "/etc"} {
		if p == "/private"+linked || strings.HasPrefix(p, "/private"+linked+"/") {
			return strings.TrimPrefix(p, "/private")
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

// inside is whether p is the build's worktree or under it, as the volume reads names. rest keeps p's own spelling
// of what follows the worktree.
func (g guard) inside(p string) (rest string, ok bool) {
	d := canonical(string(g.place))
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

	if fold(p[:i]) != fold(d) {
		return "", false
	}

	rest, ok = strings.CutPrefix(p[i:], "/")
	return rest, ok || i == len(p)
}
