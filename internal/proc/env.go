package proc

import (
	"slices"
	"strings"
)

// Child names a kind of process kiln starts, for what of kiln's environment it inherits.
type Child int

const (
	// Shell is a gate's command, run with sh.
	Shell Child = iota
	// Session is a Claude Code session.
	Session
	// Git is kiln's own git.
	Git
)

// withheld is every variable a child does not inherit, and why.
var withheld = []withholding{
	{"ANTHROPIC_API_KEY", []Child{Session}, "would bill an account instead of the developer's login"},
	{"ANTHROPIC_AUTH_TOKEN", []Child{Session}, "would bill an account instead of the developer's login"},
	{"GIT_*", []Child{Shell, Session, Git}, "one such as GIT_DIR or GIT_INDEX_FILE aims git at another repository"},
	{"CLAUDE*", []Child{Session}, "Claude Code's own, set for the session that may have started kiln"},
}

type withholding struct {
	// name is a variable's name, or a prefix of names when it ends in "*".
	name string
	from []Child
	why  string
}

func (w withholding) holds(c Child, name string) bool {
	if !slices.Contains(w.from, c) {
		return false
	}

	if prefix, ok := strings.CutSuffix(w.name, "*"); ok {
		return strings.HasPrefix(name, prefix)
	}

	return name == w.name
}

// Env returns the variables of env that the child inherits, in env's order. It is nil when it keeps none, and
// [os/exec.Cmd] reads a nil Env as kiln's own environment.
func (c Child) Env(env []string) []string {
	var kept []string
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.ContainsFunc(withheld, func(w withholding) bool { return w.holds(c, name) }) {
			kept = append(kept, kv)
		}
	}

	return kept
}
