package kiln

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
)

// agent is an agent's file: one in .kiln/agents/, or a monitor's agent.md. It holds frontmatter between lines of ---,
// then the body, the session's prompt.
// kiln reads the frontmatter's tools, model, effort and advisor, and ignores every other key and each # line.
type agent struct {
	body string
	// tools are the only tools the session has, or nil for every tool.
	tools                  []string
	model, effort, advisor string
}

// conflictMarker is a line that opens or closes a merge conflict. A line of = alone may underline a heading.
var conflictMarker = regexp.MustCompile(`(?m)^(<{7}|>{7})( .*)?$`)

// conflictIn refuses raw, a file's text, where it holds a conflict's marker, as kiln init leaves one.
func conflictIn(raw string) refusals {
	m := conflictMarker.FindString(raw)
	if m == "" {
		return nil
	}

	return refusals{{fmt.Sprintf("holds a conflict's marker, %q", m), "resolve the conflict, then commit the file"}}
}

// efforts are the efforts an agent's file may name.
var efforts = []string{"low", "medium", "high", "xhigh", "max"}

// parseAgent reads an agent's file. A file with no frontmatter is all body. A tools key names its tools on its own
// line, separated by commas, as Claude Code's agent files write them. It refuses a list in any other form. Without
// model, the agent runs Opus. Without effort or advisor, kiln names none, and each is Claude Code's own. The project
// says which a session runs with, never kiln. It refuses a file that holds a conflict's marker, as kiln init or git
// leaves one.
func parseAgent(raw string) (agent, error) {
	a := agent{model: "opus"}
	if refused := conflictIn(raw); refused != nil {
		return a, refused.about("it")
	}

	front, body, err := frontmatter(raw)
	if err != nil {
		return a, err
	}

	a.body = strings.TrimSpace(body)
	var refused refusals
	for line := range strings.Lines(front) {
		refused = append(refused, a.read(line)...)
	}

	return a, append(refused, a.badEffort()...).err()
}

// read reads one line of the frontmatter into a, and refuses the line kiln cannot read. It skips a line whose key
// starts with #, and a line with no colon.
func (a *agent) read(line string) refusals {
	key, value, ok := field(line)
	// A YAML list would read as no tools, which gives every tool: the opposite of what its writer meant.
	noNames := value == "" || strings.HasPrefix(value, "[")
	switch {
	case !ok:
		return nil
	case key == "tools" && noNames:
		return refusals{{"its tools are not tool names on the line of tools, separated by commas",
			"write them as tools: Read, Grep"}}
	}

	a.set(key, value)
	return nil
}

// badEffort refuses an effort Claude Code does not take. An agent that names none runs at Claude Code's own.
func (a *agent) badEffort() refusals {
	if a.effort == "" || slices.Contains(efforts, a.effort) {
		return nil
	}

	return refusals{{fmt.Sprintf("its effort, %q, is none of %s", a.effort, strings.Join(efforts, ", ")),
		"name one, or drop effort"}}
}

// set sets the field key names to value, unless value is empty.
func (a *agent) set(key, value string) {
	if value == "" {
		return
	}

	switch key {
	case "tools":
		a.tools = []string{}
		for tool := range strings.SplitSeq(value, ",") {
			if tool = strings.TrimSpace(tool); tool != "" {
				a.tools = append(a.tools, tool)
			}
		}
	case "model":
		a.model = value
	case "effort":
		a.effort = value
	case "advisor":
		a.advisor = value
	}
}

// field splits a line of frontmatter into its key and its value, unquoted. It answers false for a # line, or a line
// with no colon.
func field(line string) (key, value string, ok bool) {
	key, value, ok = strings.Cut(line, ":")
	key, value = strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), `"'`)
	return key, value, ok && !strings.HasPrefix(key, "#")
}

// frontmatter splits a file into its frontmatter and its body. A file whose first line is not --- has none.
func frontmatter(raw string) (front, body string, err error) {
	first, rest, _ := strings.Cut(raw, "\n")
	if strings.TrimRight(first, " \r") != "---" {
		return "", raw, nil
	}

	for i := 0; i < len(rest); {
		line, _, _ := strings.Cut(rest[i:], "\n")
		if strings.TrimRight(line, " \r") == "---" {
			return rest[:i], rest[min(len(rest), i+len(line)+1):], nil
		}

		i += len(line) + 1
	}

	return "", "", refusals{{"its frontmatter has no closing ---", "end it with a line of ---"}}
}

const (
	buildRole  = "build"
	auditRole  = "audit"
	verifyRole = "verify"
)

// agentFile is the path of a role's agent file.
func agentFile(role string) string { return kilnDir + "/agents/" + role + ".md" }

// files reads a project's files by their paths from the top, as a commit or the working tree holds them.
type files struct {
	// at names where, as a refusal says it: a commit, or the working tree.
	at   string
	read func(ctx context.Context, path string) (raw []byte, found bool, err error)
}

// filesAt reads the files the commit at holds.
func (r *Repo) filesAt(at git.Rev) files {
	return files{at: short(at), read: func(ctx context.Context, path string) ([]byte, bool, error) {
		return r.git.Show(ctx, at, path)
	}}
}

// heldFiles reads the files the commit at holds from held, what one git read there, by path. A path held lacks reads
// as no file there, so held must hold each path a reader asks for.
func heldFiles(at git.Rev, held map[string][]byte) files {
	return files{at: short(at), read: func(_ context.Context, path string) ([]byte, bool, error) {
		raw, found := held[path]
		return raw, found, nil
	}}
}

// workingTree reads the files the working tree of r's checkout holds, edits and all. That is the checkout kiln was
// opened in, which may be a linked worktree.
func (r *Repo) workingTree() files {
	return files{at: "the working tree", read: func(_ context.Context, path string) ([]byte, bool, error) {
		raw, err := os.ReadFile(filepath.Join(r.top, filepath.FromSlash(path)))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}

		return raw, err == nil, err
	}}
}

// agent reads the agent's file at path from f. It refuses a file f lacks, with fix saying what to do about it. It
// refuses a file kiln cannot read, with each reason.
func (f files) agent(ctx context.Context, path, fix string) (agent, error) {
	raw, found, err := f.read(ctx, path)
	switch {
	case err != nil:
		return agent{}, err
	case !found:
		return agent{}, refusals{{fmt.Sprintf("%s holds no %s", f.at, path), fix}}
	}

	a, err := parseAgent(string(raw))
	if refused, ok := errors.AsType[refusals](err); ok {
		return a, refused.about(fmt.Sprintf("%s, as %s holds it,", path, f.at))
	}

	return a, err
}

var (
	//go:embed contract/build.md
	buildContract string
	//go:embed contract/build-split.md
	splitContract string
	//go:embed contract/verify.md
	verifyContract string
	//go:embed contract/audit.md
	auditContract string
	// auditReply follows the audit's contract, in a spec's build and in a fix's.
	//go:embed contract/audit-reply.md
	auditReply string
)

// contract is kiln's contract with role's agent, in the build's workflow. No agent's file holds it, so no project
// drops a line of it. It fills in the spec's name, its acceptance paths and the harness, each path in backticks.
func (s sides) contract(role string, name Spec) string {
	text := map[string]string{buildRole: buildContract, verifyRole: verifyContract, auditRole: auditContract}[role]
	if role == buildRole && s.order.isolated() {
		text = splitContract
	}

	harness := "of which kiln.json's \"testHelpers\" lists none"
	if len(s.helpers) > 0 {
		harness = quoted(s.helpers)
	}

	return fmt.Sprintf(text, name, quoted(s.acceptance), harness)
}

// quoted is each of paths in backticks, joined by commas.
func quoted(paths []string) string {
	return "`" + strings.Join(paths, "`, `") + "`"
}

// agentLaunch is l, a session of role's agent in the build, as the agent's file at the build's approval says. Its
// system prompt is the file's body, then kiln's contract. In the workflow build, the build session writes the
// acceptance tests too. Its prompt then holds the verify agent's body after the build agent's. Every session of a
// build holds to what sandboxed adds.
func (b *build) agentLaunch(ctx context.Context, role string, l claude.Launch) (claude.Launch, error) {
	s, err := b.sides(ctx)
	if err != nil {
		return l, err
	}

	a, err := b.agentAt(ctx, role)
	if err != nil {
		return l, err
	}

	body := a.body
	if role == buildRole && !s.order.isolated() {
		v, err := b.agentAt(ctx, verifyRole)
		if err != nil {
			return l, err
		}

		body += "\n\n" + v.body
	}

	contract := s.contract(role, b.rec.Name)
	if b.fix != nil {
		contract = fmt.Sprintf(monitorAuditContract, b.rec.Name)
	}

	if role == auditRole {
		contract += "\n" + auditReply
	}

	l.Role = role
	l.System = strings.TrimSpace(body + "\n\n" + contract)
	l.Tools, l.Model, l.Effort, l.Advisor = a.tools, a.model, a.effort, a.advisor
	return b.sandboxed(ctx, l)
}

// agentAt reads role's agent as it stood at the build's approval.
func (b *build) agentAt(ctx context.Context, role string) (agent, error) {
	// A build queued by a kiln from before .kiln/ held its agents names a commit without them.
	fix := fmt.Sprintf("the build was queued before main held it, so run kiln queue %s again", b.rec.Name)
	return b.r.filesAt(b.rec.Approved).agent(ctx, agentFile(role), fix)
}

// sides is what each session of a build may write, as kiln.json at the build's approval says. order is when its
// sessions run.
type sides struct {
	order order
	// acceptance holds the spec's acceptance paths, with its name filled in.
	acceptance []string
	helpers    []string
}

// sides reads the build's sides from kiln.json at the build's approval, once.
func (b *build) sides(ctx context.Context) (sides, error) {
	if b.side != nil {
		return *b.side, nil
	}

	c, err := b.r.config(ctx, b.rec.Approved)
	if err != nil {
		return sides{}, err
	}

	s := sides{order: orderOf(c.Workflow), acceptance: c.acceptancePaths(b.rec.Name), helpers: c.TestHelpers}
	b.side = &s
	return s, nil
}
