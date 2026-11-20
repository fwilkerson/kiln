package hometest_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/fwilkerson/kiln/internal/hometest"
)

func TestMain(m *testing.M) { hometest.Main(m) }

// asChild names the variable that makes TestGitStaysHome run git as a test would, in the test binary it started.
const asChild = "HOMETEST_CHILD"

// TestGitStaysHome holds that the git a test runs writes to no repository that GIT_DIR names. A test binary that git
// rebase --exec or a hook starts inherits GIT_DIR. The test starts its own binary so, and has it run git as the
// examples do, with the whole environment.
func TestGitStaysHome(t *testing.T) {
	if os.Getenv(asChild) != "" {
		cmd := exec.Command("/bin/sh", "-c", "git init --quiet && git config user.name dev")
		cmd.Dir = t.TempDir()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v\n%s", err, out)
		}

		return
	}

	scratch := t.TempDir()
	if out, err := exec.Command("git", "init", "--quiet", scratch).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	config := filepath.Join(scratch, ".git", "config")
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}

	child := exec.Command(os.Args[0], "-test.run=^TestGitStaysHome$")
	child.Env = append(os.Environ(), "GIT_DIR="+filepath.Join(scratch, ".git"), asChild+"=1")
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("the test binary it started failed: %v\n%s", err, out)
	}

	if after, _ := os.ReadFile(config); string(after) != string(before) {
		t.Errorf("a test's git, started with GIT_DIR set, changed that repository's config from\n%s\nto\n%s", before,
			after)
	}
}
