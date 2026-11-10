package kiln

import (
	"context"
	_ "embed"
	"fmt"
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

// efforts are the efforts an agent's file may name.
var efforts = []string{"low", "medium", "high", "xhigh", "max"}

// parseAgent reads an agent's file. A file with no frontmatter is all body. A tools key names its tools on its own
// line, separated by commas, as Claude Code's agent files write them. It refuses a list in any other form. Without
// model or effort, the agent runs Opus at medium effort; without advisor, Fable advises it.
func parseAgent(raw string) (agent, error) {
	a := agent{model: "opus", effort: "medium", advisor: "fable"}
	front, body, err := frontmatter(raw)
	if err != nil {
		return a, err
	}

	a.body = strings.TrimSpace(body)
	for line := range strings.Lines(front) {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(strings.TrimSpace(key), "#") {
			continue
		}

		key, value = strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), `"'`)
		// A YAML list would read as no tools, which gives every tool: the opposite of what its writer meant.
		if key == "tools" && (value == "" || strings.HasPrefix(value, "[")) {
			return a, fmt.Errorf("its tools are not tool names, separated by commas, on the line of tools:")
		}

		a.set(key, value)
	}

	if !slices.Contains(efforts, a.effort) {
		return a, fmt.Errorf("its effort, %q, is none of %s", a.effort, strings.Join(efforts, ", "))
	}

	return a, nil
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
	buildRole = "build"
	auditRole = "audit"
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
	//go:embed contract/audit.md
	auditContract string
)

// contracts are kiln's contract with each agent, by role, for a spec's name. No agent's file holds it, so no project
// drops a line of it.
var contracts = map[string]string{buildRole: buildContract, auditRole: auditContract}

// agentLaunch is l, a session of role's agent in the build, as the agent's file at the build's approval says. Its
// system prompt is the file's body, then kiln's contract. It holds to what every session of a build holds to.
func (b *build) agentLaunch(ctx context.Context, role string, l claude.Launch) (claude.Launch, error) {
	// A build queued by a kiln from before .kiln/ held its agents names a commit without them.
	fix := fmt.Sprintf("the build was queued before main held it, so run kiln queue %s again", b.rec.Name)
	a, err := b.r.agent(ctx, b.rec.Approved, role, fix)
	if err != nil {
		return l, err
	}

	l.Role = role
	l.System = strings.TrimSpace(a.body + "\n\n" + fmt.Sprintf(contracts[role], b.rec.Name))
	l.Tools, l.Model, l.Effort, l.Advisor = a.tools, a.model, a.effort, a.advisor
	return b.sandboxed(ctx, l)
}
