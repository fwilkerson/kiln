package gittest

import (
	"context"
	"fmt"

	"github.com/fwilkerson/kiln/internal/git"
)

// lacks fails the test for call, a method of [git.Git] that the stand-in does not answer. It answers an error.
func (h *History) lacks(call string) error {
	h.t.Errorf("gittest: the stand-in does not answer %s: run this test on real git", call)
	return fmt.Errorf("gittest: no %s", call)
}

// Checkout fails the test, since the stand-in holds no worktree.
func (h *History) Checkout(context.Context, git.Worktree, git.Branch, git.Rev) error {
	return h.lacks("Checkout")
}

// Clone fails the test, since the stand-in holds no clone.
func (h *History) Clone(context.Context, git.Worktree, git.Branch, git.Branch) error {
	return h.lacks("Clone")
}

// Commit fails the test, since the stand-in holds no worktree to commit. A test
// commits with History.CommitFiles or History.CommitDir.
func (h *History) Commit(context.Context, git.Worktree, string) (git.Rev, error) {
	return "", h.lacks("Commit")
}

// Detached fails the test, since the stand-in holds no worktree.
func (h *History) Detached(context.Context, git.Worktree, git.Rev) error {
	return h.lacks("Detached")
}

// Diff fails the test, since the stand-in writes no patch.
func (h *History) Diff(context.Context, git.Rev, git.Rev, ...string) (string, error) {
	return "", h.lacks("Diff")
}

// EmptyTree fails the test, since the stand-in reads only commits.
func (h *History) EmptyTree(context.Context) (git.Rev, error) {
	return "", h.lacks("EmptyTree")
}

// Fetch fails the test, since the stand-in holds no clone.
func (h *History) Fetch(context.Context, git.Worktree, git.Branch) error {
	return h.lacks("Fetch")
}

// Head fails the test, since the stand-in holds no worktree.
func (h *History) Head(context.Context, git.Worktree) (git.Rev, error) {
	return "", h.lacks("Head")
}

// Merge fails the test, since the stand-in makes no merge.
func (h *History) Merge(context.Context, git.Worktree, git.Branch, string) (*git.Conflict, error) {
	return nil, h.lacks("Merge")
}

// Merged fails the test, since the stand-in makes no merge.
func (h *History) Merged(context.Context, git.Rev, git.Rev, ...string) (map[string][]byte, *git.Conflict, error) {
	return nil, nil, h.lacks("Merged")
}

// Move fails the test, since a move checks the worktree that holds the branch.
func (h *History) Move(context.Context, git.Branch, git.Rev, git.Rev) (*git.InTheWay, error) {
	return nil, h.lacks("Move")
}

// Pull fails the test, since the stand-in holds no clone.
func (h *History) Pull(context.Context, git.Worktree, git.Branch) error {
	return h.lacks("Pull")
}

// Rebase fails the test, since the stand-in makes no merge.
func (h *History) Rebase(context.Context, git.Worktree, git.Rev, string) (git.Rev, *git.Conflict, error) {
	return "", nil, h.lacks("Rebase")
}

// Remove fails the test, since the stand-in holds no worktree.
func (h *History) Remove(context.Context, []git.Worktree, ...git.Branch) error {
	return h.lacks("Remove")
}

// Reset fails the test, since the stand-in holds no worktree.
func (h *History) Reset(context.Context, git.Worktree, git.Rev) error {
	return h.lacks("Reset")
}

// Restore fails the test, since the stand-in holds no worktree.
func (h *History) Restore(context.Context, git.Worktree, git.Rev, ...string) error {
	return h.lacks("Restore")
}

// Shortstat fails the test, since the stand-in counts no lines.
func (h *History) Shortstat(context.Context, git.Rev, git.Rev) (git.Shortstat, error) {
	return git.Shortstat{}, h.lacks("Shortstat")
}

// Status fails the test, since the stand-in holds no worktree.
func (h *History) Status(context.Context, git.Worktree) (git.Rev, []string, error) {
	return "", nil, h.lacks("Status")
}
