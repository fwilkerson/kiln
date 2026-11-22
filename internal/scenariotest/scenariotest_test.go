package scenariotest

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
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

// TestExits holds that exits passes only on the status it names, a signal's as sh reports it.
func TestExits(t *testing.T) {
	for _, row := range []struct {
		script string
		want   int
		passes bool
	}{
		{"exit 3", 3, true},
		{"exit 3", 2, false},
		{"exit 0", 1, false},
		{"kill -9 $$", 137, true},
	} {
		var stderr bytes.Buffer
		got := exits(row.want, exec.Command("sh", "-c", row.script), &stderr)
		if (got == 0) != row.passes || (got == 0) != (stderr.Len() == 0) {
			t.Errorf("exits %d sh -c %q answered %d, and wrote %q", row.want, row.script, got, stderr.String())
		}
	}
}

// TestRefuseUnstated holds that a script may expect kiln to fail only through exits, which names its status.
func TestRefuseUnstated(t *testing.T) {
	refused := []string{
		"! exec kiln bogus",
		"? exec kiln fire",
		"! exec kiln fire &fire&",
		`! exec sh -c 'PATH=x exec kiln fire'`,
		"! exec exits 2 kiln queue",
	}
	allowed := []string{
		"exec kiln status",
		"exec exits 2 kiln queue",
		"! exec git show main:.kiln/specs/x.md",
		"! exec git rev-parse --verify kiln/x",
	}
	script := strings.Join(append(slices.Clone(refused), allowed...), "\n") + "\n-- a.txt --\n! exec kiln x\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txtar"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &recorder{TB: t}
	refuseUnstated(r, testscript.Params{Dir: dir})
	if len(r.errors) != len(refused) {
		t.Fatalf("refused %d lines, want %d:\n%s", len(r.errors), len(refused), strings.Join(r.errors, "\n"))
	}

	for i, line := range refused {
		if !strings.Contains(r.errors[i], line) || !strings.Contains(r.errors[i], "exec exits <status>") {
			t.Errorf("refusal %d is %q, want one naming %q and the fix", i, r.errors[i], line)
		}
	}
}

// recorder keeps the errors a check reports, in place of failing the test.
type recorder struct {
	testing.TB
	errors []string
}

func (r *recorder) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}
