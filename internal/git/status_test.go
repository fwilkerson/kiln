package git_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/fwilkerson/kiln/internal/git"
)

// TestStatus holds what Status answers for each change a checkout may hold: HEAD's commit, and each changed path.
func TestStatus(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		work string
		want string
	}{
		{"no change", "true", "HEAD []"},
		{"a changed file", "echo two >f", `HEAD ["f"]`},
		{"a staged file", "echo two >f && git add f", `HEAD ["f"]`},
		{"a deleted file", "rm f", `HEAD ["f"]`},
		{"an untracked file in a directory", "mkdir d && echo x >'d/with space'", `HEAD ["d/with space"]`},
		{"a merge in conflict", "git checkout --quiet -b side && echo side >f && git commit --quiet -am side && " +
			"git checkout --quiet main && echo main >f && git commit --quiet -am main && " +
			"! git merge --quiet side >/dev/null", `HEAD ["f"]`},
		{"a new commit", "echo two >f && git commit --quiet -am two", "HEAD []"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			sh(t, repo, "git init --quiet -b main && git config user.name t && git config user.email t@t && "+
				"echo one >f && git add f && git commit --quiet -m first")
			sh(t, repo, tc.work)
			g, err := git.New(repo, os.Environ())
			if err != nil {
				t.Fatal(err)
			}

			head, changed, err := g.Status(t.Context(), git.Worktree(repo))
			at := map[bool]string{true: "HEAD", false: "not HEAD"}[head == git.Rev(sh(t, repo, "git rev-parse HEAD"))]
			if got := fmt.Sprintf("%s %q", at, changed); err != nil || got != tc.want {
				t.Errorf("Status answered %s (%v); want %s", got, err, tc.want)
			}
		})
	}
}
