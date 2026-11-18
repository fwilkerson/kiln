package git_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/fwilkerson/kiln/internal/git"
)

// TestIdentity holds that kiln's commit is by whom git commit in the developer's shell would name, and never a guess.
func TestIdentity(t *testing.T) {
	for name, tc := range map[string]struct {
		env  []string
		want string
	}{
		"an identity set only in the environment": {
			env: []string{
				"GIT_AUTHOR_NAME=Dev", "GIT_AUTHOR_EMAIL=dev@example.com",
				"GIT_COMMITTER_NAME=Dev", "GIT_COMMITTER_EMAIL=dev@example.com",
			},
			want: "Dev <dev@example.com> Dev <dev@example.com>",
		},
		"no identity anywhere refuses, naming user.name": {want: "user.name"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := commitAs(t, tc.env); !strings.Contains(got, tc.want) {
				t.Errorf("the commit got %q; want %q", got, tc.want)
			}
		})
	}
}

// commitAs commits a file through kiln's git, with env added and no identity in any config. It returns the commit's
// author and committer, or the commit's error.
func commitAs(t *testing.T, env []string) string {
	repo, home := t.TempDir(), t.TempDir()
	sh(t, repo, "git init --quiet -b main && "+
		"git -c user.name=t -c user.email=t@t commit --quiet --allow-empty -m first")
	base := slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "GIT_") })
	base = append(base, "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"))
	g, err := git.New(repo, append(base, env...))
	if err != nil {
		t.Fatal(err)
	}

	tree := filepath.Join(t.TempDir(), "tree")
	if err := g.Checkout(t.Context(), git.Worktree(tree), "kiln/x", "main"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(tree, "x"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	rev, err := g.Commit(t.Context(), git.Worktree(tree), "x")
	if err != nil {
		return fmt.Sprint(err)
	}

	return sh(t, repo, "git log -1 --format='%an <%ae> %cn <%ce>' "+string(rev))
}
