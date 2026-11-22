package git_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/fwilkerson/kiln/internal/git"
)

// TestCheckout holds the wiring a build's links into .claude/ rest on. A build's worktree holds a link as a plain
// file. Claude Code's sandbox then lets a session's shell commands write where the link points. A commit made there
// still records the link.
func TestCheckout(t *testing.T) {
	t.Parallel()
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

// TestCheckoutAgain holds that Checkout takes a path whose worktree was deleted by hand. git lists that worktree, with
// its branch checked out, until a prune.
func TestCheckoutAgain(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, left string }{
		{"with its branch", "true"},
		{"without its branch", "git update-ref -d refs/heads/kiln/x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			sh(t, repo, "git init --quiet -b main && git -c user.name=t -c user.email=t@t commit --quiet "+
				"--allow-empty -m first")
			g, err := git.New(repo, os.Environ())
			if err != nil {
				t.Fatal(err)
			}

			tree := filepath.Join(t.TempDir(), "tree")
			if err := g.Checkout(t.Context(), git.Worktree(tree), "kiln/x", "main"); err != nil {
				t.Fatal(err)
			}

			if err := os.RemoveAll(tree); err != nil {
				t.Fatal(err)
			}

			sh(t, repo, tc.left)
			err = g.Checkout(t.Context(), git.Worktree(tree), "kiln/x", "main")
			if got := sh(t, tree, "git branch --show-current"); err != nil || got != "kiln/x" {
				t.Errorf("Checkout answered %v, and left %q checked out; want kiln/x", err, got)
			}
		})
	}
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

// TestCheckoutSharedCore holds that kiln's first worktree leaves the developer's checkout usable. With
// extensions.worktreeConfig, git reads a shared core.bare or core.worktree in each linked worktree. A config that
// turns that extension off, or that sets core.bare twice, still gives kiln a worktree of its own.
func TestCheckoutSharedCore(t *testing.T) {
	t.Parallel()
	commit := "git config user.name t && git config user.email t@t && echo a >a && git add a && git commit -qm first"
	for _, c := range []struct {
		name, setup string
	}{
		{"bare", "mkdir src && cd src && git init -q -b main && " + commit + " && cd .. && " +
			"git clone -q --bare src repo.git && git -C repo.git worktree add -q ../work main"},
		{"worktree", "git --git-dir=gd --work-tree=work init -q -b main && mkdir work && cd work && " +
			"echo 'gitdir: ../gd' >.git && " + commit},
		{"extension off", "mkdir work && cd work && git init -q -b main && " + commit + " && " +
			"git config extensions.worktreeConfig false"},
		{"core.bare twice, false last", "mkdir work && cd work && git init -q -b main && " + commit + " && " +
			"git config core.bare true && git config --add core.bare false"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			sh(t, dir, c.setup)
			work := filepath.Join(dir, "work")
			sh(t, work, "echo dirty >a")
			g, err := git.New(work, os.Environ())
			if err != nil {
				t.Fatal(err)
			}

			tree := filepath.Join(dir, "tree")
			if err := g.Checkout(t.Context(), git.Worktree(tree), "kiln/x", "main"); err != nil {
				t.Fatal(err)
			}

			if status := sh(t, work, "git status --short"); status != "M a" {
				t.Errorf("git status in the developer's checkout says %q; want M a", status)
			}

			if top, want := sh(t, tree, "git rev-parse --show-toplevel"), sh(t, tree, "pwd -P"); top != want {
				t.Errorf("kiln's worktree has its top at %s; want %s", top, want)
			}
		})
	}
}

// TestDetached holds that the gate's worktree holds only the commit. A nested repository lands as a pointer, without
// its files. A file git ignores, left by an earlier use, is gone. A worktree deleted by hand is made again.
func TestDetached(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	sh(t, repo, "git init --quiet -b main && git config user.name t && git config user.email t@t")
	sh(t, repo, "echo '*.golden' >.gitignore && mkdir data && cd data && git init --quiet && echo 42 >want.golden && "+
		"git add --force want.golden && git -c user.name=t -c user.email=t@t commit --quiet -m data")
	sh(t, repo, "git add .gitignore data && git commit --quiet -m first")
	g, err := git.New(repo, os.Environ())
	if err != nil {
		t.Fatal(err)
	}

	tree := filepath.Join(t.TempDir(), "tree")
	for _, left := range []string{"data/want.golden", "x.golden"} {
		if err := g.Detached(t.Context(), git.Worktree(tree), "main"); err != nil {
			t.Fatal(err)
		}

		if _, err := os.Stat(filepath.Join(tree, left)); err == nil {
			t.Errorf("the worktree holds %s, which the commit lacks", left)
		}

		sh(t, tree, "echo 42 >x.golden")
	}

	if err := os.RemoveAll(tree); err != nil {
		t.Fatal(err)
	}

	if err := g.Detached(t.Context(), git.Worktree(tree), "main"); err != nil {
		t.Errorf("kiln could not make again a worktree deleted by hand: %v", err)
	}
}

// TestCheckoutWaits holds that a checkout waits while another kiln adds or removes a worktree. Each such call writes
// the shared config, which git refuses to write while another call holds it. The test looks once the checkout blocks
// on the lock, so a slow machine never looks too soon.
func TestCheckoutWaits(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	sh(t, repo, "git init --quiet -b main")
	sh(t, repo, "git -c user.name=t -c user.email=t@t commit --quiet --allow-empty -m first")
	g, err := git.New(repo, os.Environ())
	if err != nil {
		t.Fatal(err)
	}

	lock := filepath.Join(repo, ".git", "kiln", "worktrees.lock")
	if err := os.MkdirAll(filepath.Dir(lock), 0o700); err != nil {
		t.Fatal(err)
	}

	other, err := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	if err := syscall.Flock(int(other.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}

	tree := filepath.Join(t.TempDir(), "tree")
	done := make(chan error)
	go func() { done <- g.Checkout(t.Context(), git.Worktree(tree), "kiln/x", "main") }()
	for !waitsInFlock("TestCheckoutWaits") {
		select {
		case err := <-done:
			t.Fatalf("the checkout ended, with %v, while another kiln held the lock", err)
		case <-time.After(10 * time.Millisecond):
		}
	}

	if _, err := os.Stat(tree); err == nil {
		t.Errorf("the checkout made %s while another kiln held the lock", tree)
	}

	other.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(tree, ".git")); err != nil {
		t.Errorf("once the lock was free, the checkout made no worktree: %v", err)
	}
}

// waitsInFlock reports whether a goroutine that test started blocks in flock(2), as on a lock another process holds.
func waitsInFlock(test string) bool {
	stacks := make([]byte, 1<<20)
	stacks = stacks[:runtime.Stack(stacks, true)]
	for g := range strings.SplitSeq(string(stacks), "\n\n") {
		if strings.Contains(g, "syscall.Flock(") && strings.Contains(g, "created by "+pkgPath+"."+test) {
			return true
		}
	}

	return false
}

// pkgPath is the import path of these tests, as a goroutine's stack names it.
const pkgPath = "github.com/fwilkerson/kiln/internal/git_test"
