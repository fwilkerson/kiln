package kiln_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestReadme keeps README.md short enough to reach a first command fast, as mkcert's README does.
// kiln's README failed each limit while it restated docs/reference.md. A cut goes there.
func TestReadme(t *testing.T) {
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}

	readme := string(raw)
	intro, _, _ := strings.Cut(readme, "\n```")
	_, quick, found := strings.Cut(readme, "\n## Quick start\n")
	quick, _, _ = strings.Cut(quick, "\n## ")
	if !found {
		t.Fatal("README.md holds no \"## Quick start\" section: add one")
	}

	shBlocks := strings.Count(quick, "\n```sh\n")

	for _, limit := range []struct {
		what      string
		got, most int
		fix       string
	}{
		{"words of prose", words(prose(readme)), 1200, "move what a reader looks up to docs/reference.md"},
		{"words before its first command", words(intro), 150, "say what kiln does in fewer words"},
		{"lines in its quick start", strings.Count(quick, "\n"), 50, "keep the commands of a first build, and no more"},
		{"words of prose in its quick start", words(prose(quick)), 150, "say what each command does in a comment"},
		{"code blocks in its quick start that are not sh", len(fence.FindAllString(quick, -1))/2 - shBlocks, 0,
			"show what the reader types in a sh block, not what kiln prints"},
	} {
		if limit.got > limit.most {
			t.Errorf("README.md holds %d %s, and at most %d: %s", limit.got, limit.what, limit.most, limit.fix)
		}
	}
}

// fence matches a line that opens or closes a fenced code block.
var fence = regexp.MustCompile("(?m)^```.*$")

// block matches a fenced code block, with both of its fences.
var block = regexp.MustCompile("(?s)\n```.*?\n```")

// prose is markdown without its fenced code blocks.
func prose(markdown string) string { return block.ReplaceAllString(markdown, "") }

// words is how many words text holds, where spaces part the words.
func words(text string) int { return len(strings.Fields(text)) }
