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
	// allowed are paths the build may change, though guarded names them. A landing of accepted amendments changes
	// its spec.
	allowed []string
	// side is the part of a split build that one session may write, or the zero Side outside one.
	side Side
}

// Side is the part of a split build one session may write. The zero Side is a build then audit's session, which
// writes anywhere but where every guard refuses.
type Side struct {
	// Role is "build" or "verify", or "" outside a split build.
	Role string
	// Acceptance is the spec's acceptance path, with its name filled in, such as "acceptance/x/".
	Acceptance string
	// Harness holds the paths both sessions may write, such as the code the acceptance tests run through.
	Harness []string
	// Fix is a monitor's fix, which has no spec. It writes nothing under .kiln/. With Role "build", Acceptance is the
	// acceptance path's fixed part, and guards each path that starts with it.
	Fix bool
}

// refusal says why the session of s may not write folded, a path folded as APFS reads it, or "". The build session
// writes neither the acceptance tests nor the amendments. The verify session writes only those, and the harness.
func (s Side) refusal(rel, folded string, spec Spec) string {
	if s.Fix {
		return s.fixRefusal(rel, folded)
	}

	tests := within(folded, s.Acceptance) || folded == fold(spec.amendPath())
	switch {
	case s.Role == buildRole && tests:
		return fmt.Sprintf("%s is the verify session's: in a split build, it alone writes %s and %s", rel,
			s.Acceptance, spec.amendPath())
	case s.Role == verifyRole && !tests && !slices.ContainsFunc(s.Harness, func(h string) bool {
		return within(folded, h)
	}):
		return fmt.Sprintf("%s is the build session's: in a split build, the verify session writes only %s, %s "+
			"and the harness", rel, s.Acceptance, spec.amendPath())
	}

	return ""
}

// fixRefusal says why a monitor's fix may not write folded, or "". It writes the acceptance tests only when its
// monitor says it may, and then Acceptance is "".
func (s Side) fixRefusal(rel, folded string) string {
	if s.Acceptance == "" || !strings.HasPrefix(folded, fold(s.Acceptance)) {
		return ""
	}

	return fmt.Sprintf("%s is under the acceptance tests, %s, which judge a monitor's fix: it writes them only when "+
		"its monitor says \"tests\": true", rel, s.Acceptance)
}

// within is whether folded is side or under it. A side that ends in a slash is a directory, and holds only what is
// under it.
func within(folded, side string) bool {
	if side == "" {
		return false
	}

	dir := strings.HasSuffix(side, "/")
	side = fold(path.Clean(side))
	return (!dir && folded == side) || strings.HasPrefix(folded, side+"/")
}

// guarded says why the build may not change rel, a path within the repository. It says "" if the build may. It reads
// each name as APFS does, folding case, so .KILN/x.md is .kiln/x.md.
func (g guard) guarded(rel string) string {
	folded := fold(path.Clean(strings.TrimPrefix(rel, "/")))
	names := strings.Split(folded, "/")
	settings, _ := path.Match(fold(".claude/settings*.json"), folded)
	switch {
	case slices.Contains(g.allowed, rel):
		return ""
	case slices.Contains(names, fold(".git")):
		return rel + " is in a git directory, which only git and kiln write"
	case names[0] == fold(kilnDir) && g.side.Fix:
		return rel + " is under .kiln/, which holds what the developer approved, the agents and the gate: a " +
			"monitor's fix writes nothing there"
	case names[0] == fold(kilnDir) && folded != fold(g.spec.amendPath()):
		return fmt.Sprintf("%s is under .kiln/, which holds what the developer approved, the agents and the gate: a "+
			"build writes there only %s", rel, g.spec.amendPath())
	case settings:
		return rel + " is a Claude Code settings file, which stays the developer's"
	}

	return g.side.refusal(rel, folded, g.spec)
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
// outside place, and one to a path that guard.guarded names. In a split build, side is the session's: it refuses a
// write outside it too. It writes its refusal to out, as Claude Code reads it. It reads no shell command: the gate's
// own check finds a guarded path a command changed.
func Hook(in io.Reader, out io.Writer, spec Spec, place git.Worktree, side Side) error {
	var c hookCall
	if err := json.NewDecoder(in).Decode(&c); err != nil {
		return fmt.Errorf("the hook's input: %w", err)
	}

	why := guard{spec: spec, place: place, side: side}.denial(c)
	if why == "" {
		return nil
	}

	return json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]string{
		"hookEventName":            "PreToolUse",
		"permissionDecision":       "deny",
		"permissionDecisionReason": why,
	}})
}

// denial says why the hook denies the call c, or "" if it allows it.
func (g guard) denial(c hookCall) string {
	target := cmpOr(c.Input.File, c.Input.Notebook)
	if target == "" {
		return c.Tool + " names no file, so the hook cannot tell where it writes"
	}

	// A relative target joins the cwd as spelled, and only the result is made canonical.
	cwd := string(g.place)
	if c.Cwd != "" {
		cwd = resolve(cwd, c.Cwd)
	}

	rest, ok := g.inside(resolve(cwd, target))
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
