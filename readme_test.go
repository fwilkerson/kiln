package kiln_test

import (
	"os"
	"strings"
	"testing"
)

// TestReadmeCommands holds README's list of commands to kiln's usage in cmd/kiln, line for line.
func TestReadmeCommands(t *testing.T) {
	src, err := os.ReadFile("cmd/kiln/main.go")
	if err != nil {
		t.Fatal(err)
	}

	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}

	_, usage, _ := strings.Cut(string(src), "const usage = `usage:\n")
	usage, _, _ = strings.Cut(usage, "`")
	var want strings.Builder
	for line := range strings.Lines(usage) {
		want.WriteString(strings.TrimPrefix(line, "  "))
	}

	_, list, _ := strings.Cut(string(readme), "## Commands\n\n```text\n")
	list, _, _ = strings.Cut(list, "```")
	if usage == "" || list != want.String() {
		t.Errorf("README.md's Commands differ from the usage in cmd/kiln/main.go. Copy the usage there, "+
			"without its indent:\n%s", want.String())
	}
}
