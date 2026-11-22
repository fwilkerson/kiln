package scenariotest

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRunAllSkipsHidden holds that RunAll runs no feature for a folder Go would ignore. Claude Code leaves
// .claude/.cc-writes in each folder a session's shell enters, so a feature named .claude failed TestUAT.
func TestRunAllSkipsHidden(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{".claude/.cc-writes", "_drafts"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	RunAll(t, dir)
}
