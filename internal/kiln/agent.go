package kiln

import (
	"context"
	_ "embed"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
)

// agent is an agent's file in .kiln/agents/: frontmatter between lines of ---, then the body, the session's prompt.
// kiln reads the frontmatter's tools, model, effort and advisor, and ignores every other key and each # line.
type agent struct {
	body string
	// tools are the only tools the session has, or nil for every tool.
	tools                  []string
	model, effort, advisor string
}

// conflictMarker is a line that opens or closes a merge conflict. A line of = alone may underline a heading.
var conflictMarker = regexp.MustCompile(`(?m)^(<{7}|>{7})( .*)?$`)

// efforts are the efforts an agent's file may name.
var efforts = []string{"low", "medium", "high", "xhigh", "max"}

// parseAgent reads an agent's file. A file with no frontmatter is all body. A tools key names its tools on its own
// line, separated by commas, as Claude Code's agent files write them. It refuses a list in any other form. Without
// model, the agent runs Opus. Without effort or advisor, kiln names none, and each is Claude Code's own. The project
// says which a session runs with, never kiln. It refuses a file that holds a conflict's marker, as kiln init or git
// leaves one.
func parseAgent(raw string) (agent, error) {
	a := agent{model: "opus"}
	if m := conflictMarker.FindString(raw); m != "" {
		return a, fmt.Errorf("it holds a conflict's marker, %q: resolve the conflict, then commit the file", m)
	}

	front, body, err := frontmatter(raw)
	if err != nil {
		return a, err
	}

	a.body = strings.TrimSpace(body)
	for line := range strings.Lines(front) {
		if err := a.read(line); err != nil {
			return a, err
		}
	}

	return a, a.badEffort()
}

// read reads one line of the frontmatter into a. It skips a # line, and a line with no colon.
func (a *agent) read(line string) error {
	key, value, ok := strings.Cut(line, ":")
	key, value = strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), `"'`)
	// A YAML list would read as no tools, which gives every tool: the opposite of what its writer meant.
	noNames := value == "" || strings.HasPrefix(value, "[")
	switch {
	case !ok, strings.HasPrefix(key, "#"):
		return nil
	case key == "tools" && noNames:
		return fmt.Errorf("its tools are not tool names, separated by commas, on the line of tools:")
	}

	a.set(key, value)
	return nil
}

// badEffort refuses an effort Claude Code does not take. An agent that names none runs at Claude Code's own.
func (a *agent) badEffort() error {
	if a.effort == "" || slices.Contains(efforts, a.effort) {
		return nil
	}

	return fmt.Errorf("its effort, %q, is none of %s", a.effort, strings.Join(efforts, ", "))
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

	return "", "", fmt.Errorf("its frontmatter has no closing ---")
}

const (
	buildRole  = "build"
	auditRole  = "audit"
	verifyRole = "verify"
)

// agentFile is the path of a role's agent file.
func agentFile(role string) string { return kilnDir + "/agents/" + role + ".md" }

// agent reads the file of role's agent at the commit at. A commit with no such file is refused, and fix says what
// to do about it.
func (r *Repo) agent(ctx context.Context, at git.Rev, role, fix string) (agent, error) {
	raw, found, err := r.git.Show(ctx, at, agentFile(role))
	switch {
	case err != nil:
		return agent{}, err
	case !found:
		return agent{}, fmt.Errorf("%w: %s holds no %s: %s", errRefused, short(at), agentFile(role), fix)
	}

	a, err := parseAgent(string(raw))
	if err != nil {
		return a, fmt.Errorf("%w: %s at %s: %w", errRefused, agentFile(role), short(at), err)
	}

	return a, nil
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
)

// contract is kiln's contract with role's agent, in the build's workflow. No agent's file holds it, so no project
// drops a line of it. It fills in the spec's name, its acceptance paths and the harness, each path in backticks.
func (s sides) contract(role string, name Spec) string {
	text := map[string]string{buildRole: buildContract, verifyRole: verifyContract, auditRole: auditContract}[role]
	if role == buildRole && s.split {
		text = splitContract
	}

	harness := "which kiln.json names none of"
	if len(s.harness) > 0 {
		harness = quoted(s.harness)
	}

	return fmt.Sprintf(text, name, quoted(s.acceptance), harness)
}

// quoted is each of paths in backticks, joined by commas.
func quoted(paths []string) string {
	return "`" + strings.Join(paths, "`, `") + "`"
}

// agentLaunch is l, a session of role's agent in the build, as the agent's file at the build's approval says. Its
// system prompt is the file's body, then kiln's contract. Outside a split build, the build session writes the
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
	if role == buildRole && !s.split {
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

	l.Role = role
	l.System = strings.TrimSpace(body + "\n\n" + contract)
	l.Tools, l.Model, l.Effort, l.Advisor = a.tools, a.model, a.effort, a.advisor
	return b.sandboxed(ctx, l)
}

// agentAt reads role's agent as it stood at the build's approval.
func (b *build) agentAt(ctx context.Context, role string) (agent, error) {
	// A build queued by a kiln from before .kiln/ held its agents names a commit without them.
	fix := fmt.Sprintf("the build was queued before main held it, so run kiln queue %s again", b.rec.Name)
	return b.r.agent(ctx, b.rec.Approved, role, fix)
}

// sides is what each session of a build may write, as kiln.json at the build's approval says.
type sides struct {
	split bool
	// acceptance holds the spec's acceptance paths, with its name filled in.
	acceptance []string
	harness    []string
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

	s := sides{split: c.Workflow == splitWorkflow, acceptance: c.acceptancePaths(b.rec.Name), harness: c.Harness}
	b.side = &s
	return s, nil
}
