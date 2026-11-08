package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fwilkerson/kiln/internal/git"
)

// TestMove holds the rule a landing rests on: main moves only from the commit the landing gated on.
func TestMove(t *testing.T) {
	for name, tc := range map[string]struct {
		expected func(first, second string) string
		moved    bool
	}{
		"main is where the landing found it": {expected: func(first, _ string) string { return first }, moved: true},
		"another landing moved main first":   {expected: func(_, second string) string { return second }},
	} {
		t.Run(name, func(t *testing.T) {
			repo := t.TempDir()
			sh(t, repo, "git init --quiet -b main && git config user.name t && git config user.email t@t")
			sh(t, repo, "git commit --quiet --allow-empty -m first")
			first := sh(t, repo, "git rev-parse HEAD")
			sh(t, repo, "git commit --quiet --allow-empty -m second && git checkout --quiet -b other")
			second := sh(t, repo, "git rev-parse HEAD")
			sh(t, repo, "git checkout --quiet main && git reset --quiet --hard "+first)
			sh(t, repo, "echo landed >landed && git add landed && git commit --quiet -m landed")
			landed := sh(t, repo, "git rev-parse HEAD")
			sh(t, repo, "git reset --quiet --hard "+first)

			g, err := git.New(repo, os.Environ())
			if err != nil {
				t.Fatal(err)
			}

			way, err := g.Move(t.Context(), "main", landed, tc.expected(first, second))
			if err != nil {
				t.Fatal(err)
			}

			tip, _ := g.Tip(t.Context(), "main")
			_, statErr := os.Stat(filepath.Join(repo, "landed"))
			switch {
			case tc.moved && (way != nil || tip != landed || statErr != nil):
				t.Errorf("got %+v, main at %s, its file %v; want both at %s", way, tip, statErr, landed)
			case !tc.moved && (way == nil || way.Tip != first || tip != first):
				t.Errorf("got %+v, main at %s; want it refused, with main left at %s", way, tip, first)
			}
		})
	}
}

func sh(t *testing.T, dir, script string) string {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", script, err, out)
	}

	return strings.TrimSpace(string(out))
}
