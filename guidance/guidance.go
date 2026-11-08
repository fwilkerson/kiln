// Package guidance holds the Claude Code files a build stages for .claude/, and the audit kiln runs from them.
//
// Claude Code refuses a headless session's write under .claude/, so a build writes agents/ and skills/ here. Landing
// links .claude/agents to guidance/agents/ and .claude/skills to guidance/skills/, where .claude/ has neither.
package guidance

import (
	_ "embed"
	"strings"
)

//go:embed agents/audit.md
var audit string

// Audit is the body of the audit agent, its frontmatter left out. A session started with it as its system prompt
// audits as the agent would.
func Audit() string {
	if rest, ok := strings.CutPrefix(audit, "---\n"); ok {
		if _, body, ok := strings.Cut(rest, "\n---\n"); ok {
			return strings.TrimSpace(body)
		}
	}

	return strings.TrimSpace(audit)
}
