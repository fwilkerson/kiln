package git_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fwilkerson/kiln/internal/git"
)

// TestRemove holds what Remove takes away of each checkout and branch, and what it leaves. A path that fails leaves
// the branch.
func TestRemove(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		given  func(t *testing.T, s scratch, path git.Worktree)
		branch git.Branch
		want   string
	}{
		{"a worktree and its branch", checkedOut, "kiln/x", "no error, path gone, 1 worktree, branches main"},
		{"a clone", cloned, "", "no error, path gone, 1 worktree, branches main"},
		{"a worktree deleted by hand", deleted, "kiln/x", "no error, path gone, 1 worktree, branches main"},
		{"a directory that is no worktree", plainDir, "kiln/x", "an error, path there, 1 worktree, branches kiln/x main"},
		{"a branch whose name starts another's", longerBranch, "kiln/x",
			"no error, path gone, 1 worktree, branches kiln/x/y main"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := makeScratch(t, "", "")
			path := git.Worktree(filepath.Join(t.TempDir(), "tree"))
			tc.given(t, s, path)
			err := remove(t, s.git, path, tc.branch)
			if got := s.left(t, path, err); got != tc.want {
				t.Errorf("Remove left %s (%v); want %s", got, err, tc.want)
			}
		})
	}
}

// remove calls Remove on path and branch, which may be "".
func remove(t *testing.T, g *git.Git, path git.Worktree, branch git.Branch) error {
	return g.Remove(t.Context(), path, branch)
}

// left says what a Remove that answered err left: path, the worktrees git lists, and the branches.
func (s scratch) left(t *testing.T, path git.Worktree, err error) string {
	failed := map[bool]string{false: "no error", true: "an error"}[err != nil]
	_, missing := os.Stat(string(path))
	there := map[bool]string{false: "path there", true: "path gone"}[missing != nil]
	worktrees := strings.Count(sh(t, s.repo, "git worktree list --porcelain"), "worktree ")
	branches := strings.Fields(sh(t, s.repo, "git for-each-ref --format='%(refname:short)' refs/heads"))
	return fmt.Sprintf("%s, %s, %d worktree, branches %s", failed, there, worktrees, strings.Join(branches, " "))
}

func checkedOut(t *testing.T, s scratch, path git.Worktree) {
	if err := s.git.Checkout(t.Context(), path, "kiln/x", "main"); err != nil {
		t.Fatal(err)
	}
}

func cloned(t *testing.T, s scratch, path git.Worktree) {
	if err := s.git.Clone(t.Context(), path, "main", "kiln/x"); err != nil {
		t.Fatal(err)
	}
}

func deleted(t *testing.T, s scratch, path git.Worktree) {
	checkedOut(t, s, path)
	if err := os.RemoveAll(string(path)); err != nil {
		t.Fatal(err)
	}
}

func plainDir(t *testing.T, s scratch, path git.Worktree) {
	if err := os.Mkdir(string(path), 0o755); err != nil {
		t.Fatal(err)
	}

	sh(t, s.repo, "git branch kiln/x")
}

func longerBranch(t *testing.T, s scratch, _ git.Worktree) {
	sh(t, s.repo, "git branch kiln/x/y")
}
