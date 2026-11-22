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

// InTheWay says why [Git.Move] did not move a branch, by the one field it sets. Tip is where the branch was, when it
// was not at the commit expected. Busy is the worktree where a rebase, a git bisect or another git holding the index
// works on the branch. Files are the developer's changed, untracked or ignored files that the move would overwrite.
type InTheWay struct {
	Tip   Rev
	Busy  Worktree
	Files []string
}

// Move moves branch from the commit expected to the commit to, which descends from expected. A branch checked out in a
// worktree moves with it, by a fast-forward merge. Move refuses with an [InTheWay], which says why, and moves nothing.
// When git refuses for no reason Move can name, Move tries once more after 500 ms.
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

// blocked says why the branch can't move before it is tried, and learns where it is checked out. A worktree list
// names the commit each worktree holds, so a branch checked out has its tip read there.
func (m *move) blocked(ctx context.Context) (*InTheWay, error) {
	list, err := m.g.worktreeList(ctx)
	if err != nil {
		return nil, err
	}

	where := checkedOut(list, m.branch.ref())
	tip := where.head
	if where.path == "" {
		if tip, err = m.g.tip(ctx, m.branch.ref()); err != nil {
			return nil, err
		}
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

	m.where = where.path
	return nil, nil
}

func (m *move) apply(ctx context.Context) error {
	var err error
	if m.where == "" {
		update := []string{"update-ref", "-m", "kiln: land", m.branch.ref()}
		_, err = m.g.in(ctx, m.g.repo, append(update, string(m.to), string(m.expected))...)
	} else {
		// A merge killed midway leaves the checkout half-updated and index.lock behind. It ends well within a minute.
		ctx := context.WithoutCancel(ctx)
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
	tips, err := g.tips(ctx, ref)
	return tips[ref], err
}

// tips maps each of refs that is there to the commit it points to, in one git. A ref git matches only by its prefix
// is not there.
func (g *Git) tips(ctx context.Context, refs ...string) (map[string]Rev, error) {
	out, err := g.in(ctx, g.repo, append([]string{"for-each-ref", "--format=%(refname)%00%(objectname)"}, refs...)...)
	if err != nil {
		return nil, err
	}

	tips := map[string]Rev{}
	for line := range strings.Lines(out) {
		if name, hash, _ := strings.Cut(strings.TrimSuffix(line, "\n"), "\x00"); slices.Contains(refs, name) {
			tips[name] = Rev(hash)
		}
	}

	return tips, nil
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

// busy names the worktree where a rebase or a git bisect is working on the branch. While it does, git lists that
// worktree as detached, and the branch looks free to move by update-ref. The rebase's abort would then put it back.
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
	head     Rev
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
		case key == "HEAD":
			list[len(list)-1].head = Rev(value)
		case key == "prunable":
			list[len(list)-1].prunable = true
		}
	}

	return list, nil
}

// checkedOut is the worktree in list where ref is checked out, or no worktree. It leaves out a worktree whose directory
// is gone. git still counts its branch as checked out, but there is no tree there for a merge to update.
func checkedOut(list []listing, ref string) listing {
	for _, w := range list {
		if w.branch == ref && !w.prunable {
			return w
		}
	}

	return listing{}
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

// Remove removes each worktree or clone at paths that is there, then each of branches that is there. It refuses a
// path that holds the main checkout, or a directory that is not a worktree. It tries each path, and removes no branch
// once one fails.
func (g *Git) Remove(ctx context.Context, paths []Worktree, branches ...Branch) error {
	unlock, err := g.lockWorktrees(ctx)
	if err != nil {
		return err
	}

	defer unlock()
	return g.remove(ctx, paths, branches...)
}

// remove is Remove, for a caller that holds the worktrees lock.
func (g *Git) remove(ctx context.Context, paths []Worktree, branches ...Branch) error {
	r := &removal{g: g}
	var err error
	for _, path := range paths {
		err = errors.Join(err, r.tree(ctx, g.at(path)))
	}

	if r.stale() {
		err = errors.Join(err, g.prune(ctx))
	}

	if err != nil {
		return err
	}

	return g.deleteBranches(ctx, branches)
}

// removal is one Remove under the worktrees lock. It lists the worktrees once: removing one changes whether git lists
// no other. byHand says kiln deleted a worktree git lists, and gone says a path was gone before the removal.
type removal struct {
	g      *Git
	list   []listing
	byHand bool
	gone   bool
}

// stale says whether git may still list a worktree the removal deleted by hand, or found gone. A listing the removal
// read marks each worktree whose directory is gone, as git worktree prune finds it.
func (r *removal) stale() bool {
	switch {
	case r.byHand:
		return true
	case !r.gone:
		return false
	case r.list == nil:
		return true
	}

	return slices.ContainsFunc(r.list, func(w listing) bool { return w.prunable })
}

func (r *removal) tree(ctx context.Context, path Worktree) error {
	there, err := os.Stat(string(path))
	if errors.Is(err, fs.ErrNotExist) {
		r.gone = true
		return nil
	}

	if err != nil {
		return err
	}

	if err := r.g.holdsMain(path); err != nil {
		return err
	}

	listed, err := r.listed(ctx, there)
	if err != nil {
		return err
	}

	if listed {
		_, err := r.g.in(ctx, r.g.repo, "worktree", "remove", "--force", string(path))
		return err
	}

	if _, err := os.Lstat(filepath.Join(string(path), ".git")); err != nil {
		return fmt.Errorf("remove %s: not a worktree: %w", path, err)
	}

	if err := os.RemoveAll(string(path)); err != nil {
		return err
	}

	r.byHand = true
	return nil
}

// deleteBranches deletes each of branches that is there, with one git to find them and one to delete them.
func (g *Git) deleteBranches(ctx context.Context, branches []Branch) error {
	if len(branches) == 0 {
		return nil
	}

	refs := make([]string, len(branches))
	for i, b := range branches {
		refs[i] = b.ref()
	}

	tips, err := g.tips(ctx, refs...)
	there := []string{"branch", "--quiet", "-D"}
	for _, b := range branches {
		if tips[b.ref()] != "" {
			there = append(there, string(b))
		}
	}

	if err != nil || len(there) == 3 {
		return err
	}

	_, err = g.in(ctx, g.repo, there...)
	return err
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
func (r *removal) listed(ctx context.Context, there os.FileInfo) (bool, error) {
	if r.list == nil {
		list, err := r.g.worktreeList(ctx)
		if err != nil {
			return false, err
		}

		r.list = list
	}

	return slices.ContainsFunc(r.list, func(w listing) bool {
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
