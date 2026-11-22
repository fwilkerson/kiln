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
	{"ANTHROPIC_API_KEY", []Child{Session}, nil, "would bill an account instead of the developer's login"},
	{"ANTHROPIC_AUTH_TOKEN", []Child{Session}, nil, "would bill an account instead of the developer's login"},
	{
		"GIT_*", []Child{Shell, Session, Git}, identity,
		"one such as GIT_DIR or GIT_INDEX_FILE aims git at another repository",
	},
	{"CLAUDE*", []Child{Session}, signIn, "Claude Code's own, set for the session that may have started kiln"},
}

// signIn is each CLAUDE* variable that says how the developer signs in, which a session keeps.
var signIn = []string{
	"CLAUDE_CODE_OAUTH_TOKEN",
	"CLAUDE_CONFIG_DIR",
	"CLAUDE_CODE_USE_BEDROCK",
	"CLAUDE_CODE_USE_VERTEX",
}

// identity is each GIT_* variable that names who commits, which every child keeps, so its commits are the developer's.
var identity = []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"}

type withholding struct {
	// name is a variable's name, or a prefix of names when it ends in "*".
	name string
	from []Child
	// except is each name the prefix matches that the child still inherits.
	except []string
	why    string
}

func (w withholding) holds(c Child, name string) bool {
	if !slices.Contains(w.from, c) || slices.Contains(w.except, name) {
		return false
	}

	if prefix, ok := strings.CutSuffix(w.name, "*"); ok {
		return strings.HasPrefix(name, prefix)
	}

	return name == w.name
}

// Env returns the variables of env that the child inherits, in env's order. It is nil when it keeps none, and
// [Command] reads a nil Env as kiln's own environment.
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
