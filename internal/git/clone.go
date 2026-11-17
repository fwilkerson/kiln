package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Clone makes a clone of the repository at path. It holds only main's objects and no remote, so nothing in it
// reaches the repository's other branches. It checks the clone out on a new branch, made from main. Its links are
// plain files, as a worktree's are, and it commits as the repository does. It refuses a path that is there already.
// If the clone fails part way, Clone removes it.
func (g *Git) Clone(ctx context.Context, path Worktree, main, branch Branch) error {
	path = g.at(path)
	if _, err := os.Lstat(string(path)); err == nil {
		return fmt.Errorf("clone into %s: it is there already", path)
	}

	err := g.clone(ctx, path, main, branch)
	if err != nil {
		err = errors.Join(err, os.RemoveAll(string(path)))
	}

	return err
}

func (g *Git) clone(ctx context.Context, path Worktree, main, branch Branch) error {
	clone := []string{"clone", "--quiet", "--no-local", "--single-branch", "--no-tags", "--branch", string(main),
		"--config", "core.symlinks=false", string(g.repo), string(path)}
	if _, err := g.in(ctx, g.repo, clone...); err != nil {
		return err
	}

	steps := [][]string{{"remote", "remove", "origin"}, {"checkout", "--quiet", "-b", string(branch)}}
	for _, key := range []string{"user.name", "user.email"} {
		if value, err := g.line(ctx, g.repo, "config", "get", key); err == nil && value != "" {
			steps = append(steps, []string{"config", "set", key, value})
		}
	}

	for _, args := range steps {
		if _, err := g.in(ctx, path, args...); err != nil {
			return err
		}
	}

	return nil
}

// Fetch sets branch in the repository to the commit branch holds in clone, even where that drops commits.
func (g *Git) Fetch(ctx context.Context, clone Worktree, branch Branch) error {
	refspec := "+" + branch.ref() + ":" + branch.ref()
	_, err := g.in(ctx, g.repo, "fetch", "--quiet", "--no-tags", "--no-write-fetch-head", string(g.at(clone)), refspec)
	return err
}

// Pull sets branch in clone to the commit branch holds in the repository. clone must not have branch checked out.
func (g *Git) Pull(ctx context.Context, clone Worktree, branch Branch) error {
	refspec := "+" + branch.ref() + ":" + branch.ref()
	_, err := g.in(ctx, clone, "fetch", "--quiet", "--no-tags", "--no-write-fetch-head", string(g.repo), refspec)
	return err
}

// Merge merges branch into what is checked out at checkout, in a commit with message, when the merge is clean. A
// merge that conflicts is answered as a [Conflict], and changes nothing.
func (g *Git) Merge(ctx context.Context, checkout Worktree, branch Branch, message string) (*Conflict, error) {
	args := []string{"merge-tree", "--write-tree", "-z", "--name-only", "HEAD", string(branch)}
	out, err := g.in(ctx, checkout, args...)
	switch {
	case conflicted(err):
		return &Conflict{Files: conflicts(strings.Split(out, "\x00")[1:])}, nil
	case err != nil:
		return nil, err
	}

	_, err = g.in(ctx, checkout, "merge", "--quiet", "--no-edit", "--no-verify", "-m", message, string(branch))
	return nil, err
}

// Restore puts paths at checkout back as the commit at holds them, staged.
func (g *Git) Restore(ctx context.Context, checkout Worktree, at Rev, paths ...string) error {
	_, err := g.in(ctx, checkout, append([]string{"--literal-pathspecs", "checkout", "--quiet", string(at), "--"},
		paths...)...)
	return err
}
