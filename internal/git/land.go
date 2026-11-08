package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// InTheWay says why a branch was not moved. Tip is where the branch was, when it was not at the commit expected. Busy
// is the worktree where a rebase, a bisect or another git holding the index is busy with it. Files are the developer's
// changed, untracked or ignored files that moving it would have overwritten.
type InTheWay struct {
	Tip   string
	Busy  string
	Files []string
}

// Move moves branch from expected to to, a commit that descends from it. A branch checked out in a worktree moves
// with it, by a fast-forward merge. It refuses, with an [InTheWay], when the branch is not at expected. That is so when
// another landing moved it first.
func (g *Git) Move(ctx context.Context, branch, to, expected string) (*InTheWay, error) {
	way, again, err := g.moveOnce(ctx, branch, to, expected)
	if !again {
		return way, err
	}

	select {
	case <-ctx.Done():
		return nil, errors.Join(err, ctx.Err())
	case <-time.After(moveAgain):
	}

	way, _, err = g.moveOnce(ctx, branch, to, expected)
	return way, err
}

// moveAgain is how long a move waits to be tried once more, when the reason it was refused is gone. Another git in the
// same worktree, the developer's, held its index for a moment.
const moveAgain = 500 * time.Millisecond

func (g *Git) moveOnce(ctx context.Context, branch, to, expected string) (*InTheWay, bool, error) {
	ref := "refs/heads/" + branch
	tip, err := g.tip(ctx, ref)
	if err != nil {
		return nil, false, err
	}

	if tip != expected {
		return &InTheWay{Tip: tip}, false, nil
	}

	if busy, err := g.busy(ctx, branch); err != nil {
		return nil, false, err
	} else if busy != "" {
		return &InTheWay{Busy: busy}, false, nil
	}

	where, err := g.checkedOut(ctx, ref)
	if err != nil {
		return nil, false, err
	}

	if where == "" {
		_, err = g.in(ctx, g.repo, "update-ref", "-m", "kiln: land", ref, to, expected)
	} else {
		_, err = g.in(ctx, where, "merge", "--quiet", "--ff-only", "--no-overwrite-ignore", to)
	}

	if err == nil {
		return nil, false, g.moved(ctx, ref, to)
	}

	if tip, found := g.tip(ctx, ref); found != nil {
		return nil, false, errors.Join(err, found)
	} else if tip != expected {
		return &InTheWay{Tip: tip}, false, nil
	}

	if where == "" {
		return nil, false, err
	}

	// git says another git holds the index only in its message, "Unable to create '…/index.lock'". The developer's
	// own, an editor's or a git add -p, can hold it for as long as it likes.
	if f, ok := errors.AsType[*failed](err); ok && strings.Contains(f.Stderr, "index.lock") {
		return &InTheWay{Busy: where}, false, nil
	}

	files, found := g.inTheWay(ctx, where, expected, to)
	switch {
	case found != nil:
		return nil, false, errors.Join(err, found)
	case len(files) == 0:
		return nil, true, err
	}

	return &InTheWay{Files: files}, false, nil
}

// Tip is the commit branch points to, or "" if there is no such branch.
func (g *Git) Tip(ctx context.Context, branch string) (string, error) {
	return g.tip(ctx, "refs/heads/"+branch)
}

func (g *Git) tip(ctx context.Context, ref string) (string, error) {
	out, err := g.in(ctx, g.repo, "for-each-ref", "--format=%(refname)%00%(objectname)", ref)
	if err != nil {
		return "", err
	}

	for line := range strings.Lines(out) {
		if name, hash, _ := strings.Cut(strings.TrimSuffix(line, "\n"), "\x00"); name == ref {
			return hash, nil
		}
	}

	return "", nil
}

// moved is the check that a move did what it was asked. A fast-forward to a commit the tip already holds exits 0, and
// leaves the branch where it was.
func (g *Git) moved(ctx context.Context, ref, to string) error {
	tip, err := g.tip(ctx, ref)
	if err != nil {
		return err
	}

	if tip != to {
		return fmt.Errorf("move %s to %s: it is at %s", ref, to, tip)
	}

	return nil
}

// busy names the worktree where a rebase or a bisect is working on the branch. While it does, git lists that worktree
// as detached, and the branch looks free to move by update-ref. The rebase's abort would then put it back.
func (g *Git) busy(ctx context.Context, branch string) (string, error) {
	common, err := g.CommonDir(ctx)
	if err != nil {
		return "", err
	}

	gitDirs := map[string]string{common: g.repo}
	linked, err := os.ReadDir(filepath.Join(common, "worktrees"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}

	for _, entry := range linked {
		dotGit, err := os.ReadFile(filepath.Join(common, "worktrees", entry.Name(), "gitdir"))
		if err == nil {
			gitDirs[filepath.Join(common, "worktrees", entry.Name())] = filepath.Dir(strings.TrimSpace(string(dotGit)))
		}
	}

	for _, gitDir := range slices.Sorted(maps.Keys(gitDirs)) {
		for _, state := range []string{"rebase-merge/head-name", "rebase-apply/head-name", "BISECT_START"} {
			data, err := os.ReadFile(filepath.Join(gitDir, state))
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return "", err
			}

			if name := strings.TrimSpace(string(data)); name == branch || name == "refs/heads/"+branch {
				return gitDirs[gitDir], nil
			}
		}
	}

	return "", nil
}

type worktree struct {
	path     string
	branch   string
	prunable bool
}

func (g *Git) worktreeList(ctx context.Context) ([]worktree, error) {
	out, err := g.in(ctx, g.repo, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}

	var list []worktree
	for field := range strings.SplitSeq(out, "\x00") {
		key, value, _ := strings.Cut(field, " ")
		switch {
		case key == "worktree":
			list = append(list, worktree{path: value})
		case len(list) == 0:
		case key == "branch":
			list[len(list)-1].branch = value
		case key == "prunable":
			list[len(list)-1].prunable = true
		}
	}

	return list, nil
}

// checkedOut leaves out a worktree whose directory is gone. git still counts its branch as checked out, but there is
// no tree there for a merge to update.
func (g *Git) checkedOut(ctx context.Context, ref string) (string, error) {
	list, err := g.worktreeList(ctx)
	if err != nil {
		return "", err
	}

	for _, w := range list {
		if w.branch == ref && !w.prunable {
			return w.path, nil
		}
	}

	return "", nil
}

func (g *Git) inTheWay(ctx context.Context, where, from, to string) ([]string, error) {
	changed, err := g.Changed(ctx, from, to)
	if err != nil {
		return nil, err
	}

	out, err := g.in(ctx, where, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored", "--no-renames")
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range paths(out) {
		if len(entry) < 4 {
			continue
		}

		file := strings.TrimSuffix(entry[3:], "/")
		if slices.ContainsFunc(changed, func(c string) bool { return overlap(c, file) }) {
			files = append(files, file)
		}
	}

	slices.Sort(files)
	return slices.Compact(files), nil
}

func overlap(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// Remove removes the worktree at path, if it is there, and then branch, if it is not "". It refuses a path that holds
// the main checkout.
func (g *Git) Remove(ctx context.Context, path, branch string) error {
	g.worktrees.Lock()
	defer g.worktrees.Unlock()
	path = g.at(path)
	if err := g.removeTree(ctx, path); err != nil {
		return err
	}

	if branch == "" {
		return nil
	}

	tip, err := g.Tip(ctx, branch)
	if err != nil || tip == "" {
		return err
	}

	_, err = g.in(ctx, g.repo, "branch", "--quiet", "-D", branch)
	return err
}

func (g *Git) removeTree(ctx context.Context, path string) error {
	there, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		_, err = g.in(ctx, g.repo, "worktree", "prune")
		return err
	}

	if err != nil {
		return err
	}

	if holds, err := contains(path, g.repo); err != nil {
		return err
	} else if holds {
		return fmt.Errorf("remove %s: it holds the main checkout", path)
	}

	list, err := g.worktreeList(ctx)
	if err != nil {
		return err
	}

	for _, w := range list {
		if listed, err := os.Stat(w.path); err == nil && os.SameFile(there, listed) {
			_, err := g.in(ctx, g.repo, "worktree", "remove", "--force", path)
			return err
		}
	}

	if _, err := os.Lstat(filepath.Join(path, ".git")); err != nil {
		return fmt.Errorf("remove %s: not a worktree: %w", path, err)
	}

	if err := os.RemoveAll(path); err != nil {
		return err
	}

	_, err = g.in(ctx, g.repo, "worktree", "prune")
	return err
}

func contains(dir, path string) (bool, error) {
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, err
	}

	if path, err = filepath.EvalSymlinks(path); err != nil {
		return false, err
	}

	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../"), err
}
