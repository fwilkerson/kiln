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
	Tip   Rev
	Busy  Worktree
	Files []string
}

// Move moves branch from expected to to, a commit that descends from it. A branch checked out in a worktree moves
// with it, by a fast-forward merge. It refuses, with an [InTheWay], when the branch is not at expected. That is so when
// another landing moved it first.
func (g *Git) Move(ctx context.Context, branch Branch, to, expected Rev) (*InTheWay, error) {
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

// move is one try at moving a branch. It holds where the branch should be, where it goes, and where it is checked out.
type move struct {
	g        *Git
	branch   Branch
	to       Rev
	expected Rev
	where    Worktree
}

func (g *Git) moveOnce(ctx context.Context, branch Branch, to, expected Rev) (*InTheWay, bool, error) {
	m := &move{g: g, branch: branch, to: to, expected: expected}
	if way, err := m.blocked(ctx); way != nil || err != nil {
		return way, false, err
	}

	if err := m.apply(ctx); err != nil {
		return m.refused(ctx, err)
	}

	return nil, false, g.moved(ctx, m.branch.ref(), to)
}

// blocked says why the branch can't move before it is tried, and learns where it is checked out.
func (m *move) blocked(ctx context.Context) (*InTheWay, error) {
	tip, err := m.g.tip(ctx, m.branch.ref())
	if err != nil {
		return nil, err
	}

	if tip != m.expected {
		return &InTheWay{Tip: tip}, nil
	}

	busy, err := m.g.busy(ctx, m.branch)
	if err != nil {
		return nil, err
	}

	if busy != "" {
		return &InTheWay{Busy: busy}, nil
	}

	m.where, err = m.g.checkedOut(ctx, m.branch.ref())
	return nil, err
}

func (m *move) apply(ctx context.Context) error {
	var err error
	if m.where == "" {
		update := []string{"update-ref", "-m", "kiln: land", m.branch.ref()}
		_, err = m.g.in(ctx, m.g.repo, append(update, string(m.to), string(m.expected))...)
	} else {
		_, err = m.g.in(ctx, m.where, "merge", "--quiet", "--ff-only", "--no-overwrite-ignore", string(m.to))
	}

	return err
}

// refused learns why git refused the move, and whether it is worth one more try.
func (m *move) refused(ctx context.Context, err error) (*InTheWay, bool, error) {
	if tip, found := m.g.tip(ctx, m.branch.ref()); found != nil {
		return nil, false, errors.Join(err, found)
	} else if tip != m.expected {
		return &InTheWay{Tip: tip}, false, nil
	}

	if m.where == "" {
		return nil, false, err
	}

	if indexLocked(err) {
		return &InTheWay{Busy: m.where}, false, nil
	}

	files, found := m.g.inTheWay(ctx, m.where, m.expected, m.to)
	switch {
	case found != nil:
		return nil, false, errors.Join(err, found)
	case len(files) == 0:
		return nil, true, err
	}

	return &InTheWay{Files: files}, false, nil
}

// indexLocked says whether another git held the index. git says so only in its message, "Unable to create
// '…/index.lock'". The developer's own, an editor's or a git add -p, can hold it for as long as it likes.
func indexLocked(err error) bool {
	f, ok := errors.AsType[*failed](err)
	return ok && strings.Contains(f.Stderr, "index.lock")
}

// Tip is the commit branch points to, or "" if there is no such branch.
func (g *Git) Tip(ctx context.Context, branch Branch) (Rev, error) {
	return g.tip(ctx, branch.ref())
}

func (g *Git) tip(ctx context.Context, ref string) (Rev, error) {
	out, err := g.in(ctx, g.repo, "for-each-ref", "--format=%(refname)%00%(objectname)", ref)
	if err != nil {
		return "", err
	}

	for line := range strings.Lines(out) {
		if name, hash, _ := strings.Cut(strings.TrimSuffix(line, "\n"), "\x00"); name == ref {
			return Rev(hash), nil
		}
	}

	return "", nil
}

// moved is the check that a move did what it was asked. A fast-forward to a commit the tip already holds exits 0, and
// leaves the branch where it was.
func (g *Git) moved(ctx context.Context, ref string, to Rev) error {
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
func (g *Git) busy(ctx context.Context, branch Branch) (Worktree, error) {
	gitDirs, err := g.gitDirs(ctx)
	if err != nil {
		return "", err
	}

	for _, gitDir := range slices.Sorted(maps.Keys(gitDirs)) {
		names, err := working(gitDir)
		if err != nil {
			return "", err
		}

		if slices.Contains(names, string(branch)) || slices.Contains(names, branch.ref()) {
			return gitDirs[gitDir], nil
		}
	}

	return "", nil
}

// gitDirs maps the git directory of each worktree to the worktree's own directory.
func (g *Git) gitDirs(ctx context.Context) (map[string]Worktree, error) {
	common, err := g.CommonDir(ctx)
	if err != nil {
		return nil, err
	}

	gitDirs := map[string]Worktree{common: g.repo}
	linked, err := os.ReadDir(filepath.Join(common, "worktrees"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	for _, entry := range linked {
		dir := filepath.Join(common, "worktrees", entry.Name())
		if dotGit, err := os.ReadFile(filepath.Join(dir, "gitdir")); err == nil {
			gitDirs[dir] = Worktree(filepath.Dir(strings.TrimSpace(string(dotGit))))
		}
	}

	return gitDirs, nil
}

// working names the branches a rebase or a bisect in gitDir is working on.
func working(gitDir string) ([]string, error) {
	var names []string
	for _, state := range []string{"rebase-merge/head-name", "rebase-apply/head-name", "BISECT_START"} {
		data, err := os.ReadFile(filepath.Join(gitDir, state))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}

		names = append(names, strings.TrimSpace(string(data)))
	}

	return names, nil
}

type listing struct {
	path     Worktree
	branch   string
	prunable bool
}

func (g *Git) worktreeList(ctx context.Context) ([]listing, error) {
	out, err := g.in(ctx, g.repo, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}

	var list []listing
	for field := range strings.SplitSeq(out, "\x00") {
		key, value, _ := strings.Cut(field, " ")
		switch {
		case key == "worktree":
			list = append(list, listing{path: Worktree(value)})
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
func (g *Git) checkedOut(ctx context.Context, ref string) (Worktree, error) {
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

func (g *Git) inTheWay(ctx context.Context, where Worktree, from, to Rev) ([]string, error) {
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
func (g *Git) Remove(ctx context.Context, path Worktree, branch Branch) error {
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

	_, err = g.in(ctx, g.repo, "branch", "--quiet", "-D", string(branch))
	return err
}

func (g *Git) removeTree(ctx context.Context, path Worktree) error {
	there, err := os.Stat(string(path))
	if errors.Is(err, fs.ErrNotExist) {
		return g.prune(ctx)
	}

	if err != nil {
		return err
	}

	if err := g.holdsMain(path); err != nil {
		return err
	}

	listed, err := g.listed(ctx, there)
	if err != nil {
		return err
	}

	if listed {
		_, err := g.in(ctx, g.repo, "worktree", "remove", "--force", string(path))
		return err
	}

	if _, err := os.Lstat(filepath.Join(string(path), ".git")); err != nil {
		return fmt.Errorf("remove %s: not a worktree: %w", path, err)
	}

	if err := os.RemoveAll(string(path)); err != nil {
		return err
	}

	return g.prune(ctx)
}

func (g *Git) prune(ctx context.Context) error {
	_, err := g.in(ctx, g.repo, "worktree", "prune")
	return err
}

func (g *Git) holdsMain(path Worktree) error {
	holds, err := contains(string(path), string(g.repo))
	if err == nil && holds {
		err = fmt.Errorf("remove %s: it holds the main checkout", path)
	}

	return err
}

// listed says whether git lists the worktree whose directory is there.
func (g *Git) listed(ctx context.Context, there os.FileInfo) (bool, error) {
	list, err := g.worktreeList(ctx)
	if err != nil {
		return false, err
	}

	return slices.ContainsFunc(list, func(w listing) bool {
		info, err := os.Stat(string(w.path))
		return err == nil && os.SameFile(there, info)
	}), nil
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
