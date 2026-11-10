package git_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fwilkerson/kiln/internal/git"
)

// TestCheckout holds the wiring a build's links into .claude/ rest on. A build's worktree holds a link as a plain
// file. Claude Code's sandbox then lets a session's shell write where the link points. A commit there keeps it a
// link.
func TestCheckout(t *testing.T) {
	repo := t.TempDir()
	sh(t, repo, "git init --quiet -b main && git config user.name t && git config user.email t@t")
	sh(t, repo, "mkdir -p .kiln/agents .claude && touch .kiln/agents/a.md")
	sh(t, repo, "ln -s ../.kiln/agents .claude/agents")
	sh(t, repo, "git add -A && git commit --quiet -m first")

	g, err := git.New(repo, os.Environ())
	if err != nil {
		t.Fatal(err)
	}

	tree := filepath.Join(t.TempDir(), "tree")
	if err := g.Checkout(t.Context(), git.Worktree(tree), "kiln/x", "main"); err != nil {
		t.Fatal(err)
	}

	sh(t, tree, "echo b >.kiln/agents/b.md")
	if _, err := g.Commit(t.Context(), git.Worktree(tree), "add b\n"); err != nil {
		t.Fatal(err)
	}

	checkLinks(t, repo, tree)
}

// checkLinks checks that tree holds .claude/agents as a plain file. Its commit and repo's checkout hold a link.
func checkLinks(t *testing.T, repo, tree string) {
	t.Helper()
	switch info, err := os.Lstat(filepath.Join(tree, ".claude", "agents")); {
	case err != nil:
		t.Fatal(err)
	case !info.Mode().IsRegular():
		t.Errorf("the worktree's .claude/agents has mode %v; want a plain file", info.Mode())
	}

	if mode := sh(t, tree, "git ls-tree --format='%(objectmode)' HEAD .claude/agents"); mode != "120000" {
		t.Errorf("the commit holds .claude/agents with mode %s; want 120000, a link", mode)
	}

	if link, err := os.Readlink(filepath.Join(repo, ".claude", "agents")); err != nil || link != "../.kiln/agents" {
		t.Errorf("the main checkout's .claude/agents links to %q, %v; want ../.kiln/agents", link, err)
	}
}
