package kiln

import (
	"context"

	"github.com/fwilkerson/kiln/internal/git"
)

// gitPort is each call that kiln makes through package git, as [git.Git] answers it. plainGit runs git outside it, for
// the checkout's files and hooks, and kiln init's merges. Open gives a Repo a *git.Git. A test of a rule that only
// reads history may give it a gittest.History, which answers without starting git.
type gitPort interface {
	Branched(ctx context.Context, main, head git.Rev) ([]string, error)
	Changed(ctx context.Context, from, to git.Rev) ([]string, error)
	Checkout(ctx context.Context, path git.Worktree, branch git.Branch, at git.Rev) error
	Clone(ctx context.Context, path git.Worktree, main, branch git.Branch) error
	Commit(ctx context.Context, checkout git.Worktree, message string) (git.Rev, error)
	Detached(ctx context.Context, path git.Worktree, commit git.Rev) error
	Diff(ctx context.Context, from, to git.Rev, paths ...string) (string, error)
	EmptyTree(ctx context.Context) (git.Rev, error)
	Fetch(ctx context.Context, clone git.Worktree, branch git.Branch) error
	Files(ctx context.Context, at git.Rev, dir string) ([]string, error)
	Head(ctx context.Context, checkout git.Worktree) (git.Rev, error)
	Merge(ctx context.Context, checkout git.Worktree, branch git.Branch, message string) (*git.Conflict, error)
	MergeBase(ctx context.Context, a, b git.Rev) (git.Rev, error)
	Merged(ctx context.Context, a, b git.Rev, paths ...string) (map[string][]byte, *git.Conflict, error)
	Move(ctx context.Context, branch git.Branch, to, expected git.Rev) (*git.InTheWay, error)
	Pull(ctx context.Context, clone git.Worktree, branch git.Branch) error
	Rebase(ctx context.Context, checkout git.Worktree, onto git.Rev, message string) (git.Rev, *git.Conflict, error)
	Remove(ctx context.Context, paths []git.Worktree, branches ...git.Branch) error
	Reset(ctx context.Context, checkout git.Worktree, commit git.Rev) error
	Restore(ctx context.Context, checkout git.Worktree, at git.Rev, paths ...string) error
	Shortstat(ctx context.Context, from, to git.Rev) (git.Shortstat, error)
	Show(ctx context.Context, at git.Rev, path string) ([]byte, bool, error)
	ShowTip(ctx context.Context, branch git.Branch, paths ...string) (git.Rev, map[string][]byte, error)
	Status(ctx context.Context, checkout git.Worktree) (git.Rev, []string, error)
	Tip(ctx context.Context, branch git.Branch) (git.Rev, error)
	Tree(ctx context.Context, at git.Rev, dir string) ([]git.Entry, error)
}
