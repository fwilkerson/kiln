package kiln_test

import (
	"testing"

	"github.com/fwilkerson/practices/repotest"
)

// TestStyle fails while the practices module's style command finds a problem. Run go tool style . to fix blank lines
// in place. It only reports wide lines and long comment sentences, so fix those by hand.
func TestStyle(t *testing.T) { repotest.Style(t) }

// TestGuide fails while docs/guide.md differs from the practices module's guide. CLAUDE.md loads that file into each
// session. Run go tool guide > docs/guide.md to bring it up to date.
func TestGuide(t *testing.T) { repotest.Guide(t, "docs/guide.md") }

// TestModern fails while go fix would change the code, such as to use min or range over an int. Run go fix ./... to
// apply its changes.
func TestModern(t *testing.T) { repotest.Modern(t) }

// TestHealth fails when a file's score from go tool health differs from its score in held. A file that held omits
// must score 10.
func TestHealth(t *testing.T) { repotest.Health(t, held) }

// held is the score of each file under 10. The code-health monitor refactors them, and a line goes once its file
// scores 10. A comment above a line keeps that file's finding, by the developer's choice.
var held = map[string]string{
	// Fire stays a pass-through to run, so cmd/kiln never picks run's lock holder or its spec.
	"internal/kiln/build.go": "9",
}

// TestNames fails on a code span in a doc that names nothing declared in kiln's Go code. It skips lower-case words
// and names in capitals. It also skips Markdown file names and names from packages outside kiln.
func TestNames(t *testing.T) {
	repotest.Names(t, ".kiln/agents/*.md", ".kiln/skills/*/*.md", "README.md", "docs/reference.md")
}

// TestLinks fails on a link in a doc whose target is a path that names no file.
func TestLinks(t *testing.T) {
	repotest.Links(t, "README.md", "docs/reference.md", "docs/practices.md")
}
