package git_test

import (
	"path/filepath"
	"testing"

	"github.com/fwilkerson/kiln/internal/git"
)

// TestRebaseAuthor holds that a landing keeps the author of the work it rebases: name, email and date. Main's tip
// has another author.
func TestRebaseAuthor(t *testing.T) {
	t.Parallel()
	s := makeScratch(t, "", "")
	tree := filepath.Join(t.TempDir(), "tree")
	if err := s.git.Checkout(t.Context(), git.Worktree(tree), "kiln/x", "main"); err != nil {
		t.Fatal(err)
	}

	sh(t, tree, "echo x >x && git add x && GIT_AUTHOR_NAME=a GIT_AUTHOR_EMAIL=a@a "+
		"GIT_AUTHOR_DATE='1000000000 +0100' git commit --quiet -m x")
	sh(t, s.repo, "echo m >m && git add m && git commit --quiet -m m")
	onto := git.Rev(sh(t, s.repo, "git rev-parse main"))
	landed, _, err := s.git.Rebase(t.Context(), git.Worktree(tree), onto, "land")
	if err != nil {
		t.Fatal(err)
	}

	const want = "a a@a 1000000000 +0100"
	if by := sh(t, s.repo, "git log -1 --date=raw --format='%an %ae %ad' "+string(landed)); by != want {
		t.Errorf("the landing's author is %q; want the work's, %s", by, want)
	}
}
