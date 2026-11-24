package kiln

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fwilkerson/kiln/internal/git"
)

// guard is what a build of spec, in the worktree at place, may not change. The hook and the gate both ask it.
// hookCommand writes it as the hook's command line, and Hook reads it back.
type guard struct {
	spec  Spec
	place git.Worktree
	// head is the commit kiln made at place, or "" for the gate to read there.
	head git.Rev
	// tested is the commit of a verify-first fix's proved tests, or "". The fix holds each test file as main does,
	// with tested's change applied. The hook's command line leaves it out, since the hook refuses each test file.
	tested git.Rev
	// allowed holds the paths the build may change, though guarded names them. The landing of an accepted spec retires
	// it. The hook's command line leaves them out.
	allowed []string
	// accepted is kiln's commit that folded an accepted spec's amendments, or "". The build then writes nothing under
	// .kiln/. The gate takes the spec's files only as accepted holds them, with main's changes merged.
	accepted git.Rev
	// side is what one session may write, or the zero side where the build has one session.
	side side
}

// side is the part of a build's worktree one session may write. The zero side is a spec's one build session, in the
// workflow "build". It writes anywhere but where every guard refuses.
type side struct {
	// role is buildRole or verifyRole in the workflow build-and-verify, and fixRole in a monitor's fix. A
	// verify-first fix's verify session has verifyRole too, and its non-empty tests field tells it apart. role is ""
	// otherwise.
	role string
	// acceptance holds the acceptance paths of build-and-verify, with the spec's name filled in, such as
	// "acceptance/x/". In a fix it holds each path's fixed part, and guards each path that starts with one. It is empty
	// in a fix whose monitor says it may write the tests.
	acceptance []string
	// helpers holds the paths both sessions of build-and-verify write, as kiln.json's testHelpers names them. A
	// verify-first fix holds them as test files.
	helpers []string
	// tests holds kiln.json's testFiles in a verify-first fix, and nothing elsewhere. A test file matches one, lies
	// under an acceptance path's fixed part, or lies under a helper.
	tests []string
}

// fixRole is the side of a monitor's fix session, which has no spec. In a verify-first fix, a verify session works
// first.
const fixRole = "fix"

// verifiesFirst is whether s is a side of a verify-first fix: its verify session's, or its fix session's.
func (s side) verifiesFirst() bool { return len(s.tests) > 0 }

// fixes is whether s is a side of a monitor's fix, which writes nothing under .kiln/.
func (s side) fixes() bool { return s.role == fixRole || s.verifiesFirst() }

// valid is whether s is a side kiln builds. A side of build-and-verify has an acceptance path, and no test files.
// Helpers belong to a side of build-and-verify, or to a verify-first fix's side. A verify-first fix's verify session
// names its test files.
func (s side) valid() bool {
	switch s.role {
	case "":
		return s.acceptance == nil && s.helpers == nil && s.tests == nil
	case fixRole:
		return s.helpers == nil || s.verifiesFirst()
	case verifyRole:
		return s.verifiesFirst() || len(s.acceptance) > 0
	case buildRole:
		return len(s.acceptance) > 0 && s.tests == nil
	}

	return false
}

// refusal says why the session of s may not write folded, a path folded as APFS reads it, or "". Beside a verify
// session, the build session writes neither the acceptance tests nor the amendments. The verify session writes only
// those, and the paths of testHelpers. A fix writes no acceptance test, unless its monitor says it may. A fix's verify
// session writes only test files, and the fix session beside it writes none.
func (s side) refusal(rel, folded string, spec Spec) string {
	in := func(root string) bool { return within(folded, root) }
	tests := slices.ContainsFunc(s.acceptance, in) || folded == fold(spec.amendPath())
	paths := strings.Join(s.acceptance, ", ")
	switch {
	case s.role == buildRole:
		if tests {
			return fmt.Sprintf("%s is the verify session's: it alone writes %s and %s", rel,
				paths, spec.amendPath())
		}
	case s.role == verifyRole && s.verifiesFirst():
		if !s.testFile(folded) {
			return fmt.Sprintf("%s is no test file: a fix's verify session writes only %s", rel,
				s.testWords())
		}
	case s.role == verifyRole:
		if !tests && !slices.ContainsFunc(s.helpers, in) {
			return fmt.Sprintf("%s is the build session's: the verify session writes only %s, "+
				"%s and the paths of kiln.json's testHelpers", rel, paths, spec.amendPath())
		}
	case s.role == fixRole:
		return s.fixRefusal(rel, folded)
	}

	return ""
}

// fixRefusal says why the fix session of s may not write folded, or "". Beside a verify session, it writes no test
// file. Any other fix writes no acceptance test.
func (s side) fixRefusal(rel, folded string) string {
	if s.verifiesFirst() && s.testFile(folded) {
		return fmt.Sprintf("%s is a test file: a verify session writes the tests that judge the fix, and the fix "+
			"changes none of %s", rel, s.testWords())
	}

	i := slices.IndexFunc(s.acceptance, func(fixed string) bool { return strings.HasPrefix(folded, fold(fixed)) })
	if i < 0 {
		return ""
	}

	return fmt.Sprintf("%s is under the acceptance tests, %s, which judge a monitor's fix: it writes them only "+
		"when its monitor says \"writesTests\": true", rel, s.acceptance[i])
}

// verifiers is whether the verify session alone writes folded. The code session, whose side s is, leaves such a file
// as the proved tests hold it. A spec's are its acceptance tests and its amendments. A fix's are its test files.
func (s side) verifiers(folded string, spec Spec) bool {
	switch s.role {
	case buildRole:
		in := func(root string) bool { return within(folded, root) }
		return slices.ContainsFunc(s.acceptance, in) || folded == fold(spec.amendPath())
	case fixRole:
		return s.verifiesFirst() && s.testFile(folded)
	}

	return false
}

// testFile is whether folded is a test file of s, a verify-first fix's side. folded is a path folded as APFS reads it.
func (s side) testFile(folded string) bool {
	matches := func(name string) bool {
		return slices.ContainsFunc(s.tests, func(p string) bool {
			ok, _ := path.Match(fold(p), name)
			return ok
		})
	}

	under := func(fixed string) bool { return strings.HasPrefix(folded, fold(fixed)) }
	in := func(root string) bool { return within(folded, root) }
	return slices.ContainsFunc(strings.Split(folded, "/"), matches) || slices.ContainsFunc(s.acceptance, under) ||
		slices.ContainsFunc(s.helpers, in)
}

// testWords names the test files of s, a verify-first fix's side, for a refusal.
func (s side) testWords() string {
	words := "the files kiln.json's testFiles names, " + strings.Join(s.tests, ", ")
	if under := slices.Concat(s.acceptance, s.helpers); len(under) > 0 {
		words += ", and those under " + strings.Join(under, ", ")
	}

	return words
}

// within is whether folded is root or under it. A root that ends in a slash is a directory, and holds only what is
// under it.
func within(folded, root string) bool {
	if root == "" {
		return false
	}

	dir := strings.HasSuffix(root, "/")
	root = fold(path.Clean(root))
	return (!dir && folded == root) || strings.HasPrefix(folded, root+"/")
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
	case names[0] == fold(kilnDir) && g.side.fixes():
		return rel + " is under .kiln/, which holds what the developer approved, the agents and the gate: a " +
			"monitor's fix writes nothing there"
	case names[0] == fold(kilnDir) && g.accepted != "":
		return rel + " is under .kiln/, which holds what the developer approved, the agents and the gate: the " +
			"developer accepted the spec as it stands, so a build writes nothing there"
	case names[0] == fold(kilnDir) && folded != fold(g.spec.amendPath()):
		return fmt.Sprintf("%s is under .kiln/, which holds what the developer approved, the agents and the gate: a "+
			"build writes there only %s", rel, g.spec.amendPath())
	case settings:
		return rel + " is a Claude Code settings file, which stays the developer's"
	case slices.Contains(names, fold(".claude")):
		return rel + " is under a .claude/ directory, whose agents, skills and hooks run in the developer's own " +
			"sessions: a build names such a file in its last reply instead of writing it"
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

// hookArgs are the arguments of kiln hook that name g. hookGuard reads them back.
func (g guard) hookArgs() []string {
	args := []string{"--spec", string(g.spec), "--place", string(g.place)}
	if g.side.role != "" {
		args = append(args, "--role", g.side.role)
	}

	for _, a := range g.side.acceptance {
		args = append(args, "--acceptance", a)
	}

	for _, h := range g.side.helpers {
		args = append(args, "--helpers", h)
	}

	for _, p := range g.side.tests {
		args = append(args, "--tests", p)
	}

	if g.accepted != "" {
		args = append(args, "--accepted", string(g.accepted))
	}

	return args
}

// hookCommand is the command line of the hook that asks g, as the kiln at exe runs it.
func (g guard) hookCommand(exe string) string {
	words := []string{shellQuote(exe), "hook"}
	for _, arg := range g.hookArgs() {
		words = append(words, shellQuote(arg))
	}

	return strings.Join(words, " ")
}

// hookGuard reads back the guard that hookArgs wrote as args. Its error is ErrUsage for any that hookArgs never writes.
func hookGuard(args []string) (guard, error) {
	var g guard
	flags := flag.NewFlagSet("hook", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar((*string)(&g.spec), "spec", "", "the spec the session builds")
	flags.StringVar((*string)(&g.place), "place", "", "the build's worktree")
	flags.StringVar(&g.side.role, "role", "", "the session's side: build, verify or fix")
	flags.Func("acceptance", "an acceptance path the side guards; given once for each", appendTo(&g.side.acceptance))
	flags.Func("helpers", "a path both sessions of build-and-verify write; given once for each",
		appendTo(&g.side.helpers))
	flags.Func("tests", "a pattern of a verify-first fix's test files; given once for each", appendTo(&g.side.tests))
	flags.StringVar((*string)(&g.accepted), "accepted", "", "kiln's commit that folded an accepted spec's amendments")

	switch err := flags.Parse(args); {
	case err != nil, flags.NArg() > 0, g.spec == "", g.place == "", !g.side.valid():
		return guard{}, fmt.Errorf("%w: kiln never writes the arguments %q", ErrUsage, args)
	}

	return g, nil
}

// appendTo is a flag's func that appends each value given to list.
func appendTo(list *[]string) func(string) error {
	return func(v string) error {
		*list = append(*list, v)
		return nil
	}
}

// Hook answers the PreToolUse call on in, for the session that args name. args are what kiln wrote after "kiln hook" in
// the session's settings. Hook refuses a write outside the session's worktree, one to a guarded path, and one that
// names no file. The package doc lists those paths. Hook judges the path a write reaches, after each link it holds. It
// writes its refusal to out, as Claude Code reads it. It reads no shell command: the gate's own check finds a guarded
// path a command changed. Its error is [ErrUsage] for args that kiln never writes.
func Hook(args []string, in io.Reader, out io.Writer) error {
	g, err := hookGuard(args)
	if err != nil {
		return err
	}

	var c hookCall
	if err := json.NewDecoder(in).Decode(&c); err != nil {
		return fmt.Errorf("the hook's input: %w", err)
	}

	why := g.denial(c)
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

// resolve is target, joined to base when relative, with each link followed, as canonical spells it.
func resolve(base, target string) string {
	if !path.IsAbs(target) {
		target = path.Join(base, target)
	}

	return canonical(followLinks(path.Clean(target)))
}

// followLinks is p with each link in its existing part followed. A link whose target does not exist yet is followed
// too, since writing through the link creates that target. It gives up after 40 links, as a loop of links would never
// end.
func followLinks(p string) string {
	for range 40 {
		next, done := followLink(p)
		if done {
			return next
		}

		p = next
	}

	return p
}

// followLink is p with its existing part's links followed, and done, or p with one dangling link followed.
func followLink(p string) (next string, done bool) {
	for dir, rest := p, ""; ; dir, rest = filepath.Dir(dir), filepath.Join(filepath.Base(dir), rest) {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(resolved, rest), true
		}

		if to, err := os.Readlink(dir); err == nil {
			if !filepath.IsAbs(to) {
				// The kernel reads a relative target from the directory that really holds the link.
				parent, _ := filepath.EvalSymlinks(filepath.Dir(dir))
				to = filepath.Join(parent, to)
			}

			return filepath.Join(to, rest), false
		}

		if filepath.Dir(dir) == dir {
			return p, true
		}
	}
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
	d := canonical(followLinks(path.Clean(string(g.place))))
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
