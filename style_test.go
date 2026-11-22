package kiln_test

import (
	"bytes"
	"io/fs"
	"os"
	"regexp"
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
var held = map[string]string{}

// TestNames fails on a code span in a doc that names nothing declared in kiln's Go code. It skips lower-case words
// and names in capitals. It also skips Markdown file names and names from packages outside kiln.
func TestNames(t *testing.T) {
	repotest.Names(t, ".kiln/agents/*.md", ".kiln/reviewers/*.md", ".kiln/monitors/*/agent.md", ".kiln/skills/*/*.md",
		"README.md",
		"docs/reference.md")
}

// TestNoConflictMarkers fails on a line that starts as git marks a conflict. A merge's commit runs no hook, so no hook
// finds one. A scenario still passes with one in a file it writes.
func TestNoConflictMarkers(t *testing.T) {
	err := fs.WalkDir(os.DirFS("."), ".", func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && d.Name() == ".git":
			return fs.SkipDir
		case !d.Type().IsRegular():
			return nil
		}

		raw, err := os.ReadFile(p)
		for _, at := range conflictMarker.FindAllIndex(raw, -1) {
			line := bytes.Count(raw[:at[0]], []byte("\n")) + 1
			t.Errorf("%s:%d: a conflict marker: resolve the conflict, and remove it", p, line)
		}

		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// conflictMarker matches a line that starts as git marks a conflict's ours, base or theirs.
var conflictMarker = regexp.MustCompile(`(?m)^(<{7}|\|{7}|>{7}) `)

// TestLinks fails on a link in a doc whose target is a path that names no file.
func TestLinks(t *testing.T) {
	repotest.Links(t, "README.md", "docs/reference.md", "docs/practices.md")
}
