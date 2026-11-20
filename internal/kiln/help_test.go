package kiln

import (
	"errors"
	"go/parser"
	"go/token"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/fwilkerson/practices/claim"
)

// TestHelp holds help.txt, which kiln help prints, equal to this package's doc in kiln.go. -write writes it.
func TestHelp(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "kiln.go", nil, parser.PackageClauseOnly|parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}

	doc := f.Doc.Text()
	if *writeFiles {
		if err := os.WriteFile("help.txt", []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}

		return
	}

	if doc != packageDoc {
		t.Error("internal/kiln/help.txt differs from the package doc in kiln.go: run go test ./internal/kiln -run " +
			"TestHelp -write")
	}
}

// TestHelpTopics holds what kiln help prints for a topic: the whole doc, or one section from its heading. It refuses
// a topic the doc has no section of.
func TestHelpTopics(t *testing.T) {
	for topic, want := range map[string]string{"all": "Package kiln builds", "gate": "# The gate\n",
		"kiln-ledger": "# kiln ledger\n", "presets": "# Presets\n"} {
		var out strings.Builder
		err := Help(&out, topic)
		whole := strings.Contains(out.String(), "\n# ") == (topic == allTopic)
		if !claim.Holds(err == nil, strings.HasPrefix(out.String(), want), whole) {
			t.Errorf("kiln help %s: %v, and printed:\n%s\nwant it to start %q, and hold one section", topic, err,
				out.String(), want)
		}
	}

	err := Help(io.Discard, "gates")
	if !errors.Is(err, ErrUsage) || !strings.Contains(err.Error(), "only all, specs") {
		t.Errorf("kiln help gates: %v; want a usage error that lists the topics", err)
	}
}
