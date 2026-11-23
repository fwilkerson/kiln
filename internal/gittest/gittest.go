// Package gittest is a stand-in for [git.Git] in memory, for a test of a rule that reads a repository's history:
//
//	h := gittest.New(t)
//	first := h.CommitFiles("main", gittest.Files{"go.mod": "module x\n"}) // main's first commit
//	h.Branch("kiln/x", first)
//	h.CommitDir("main", dir) // what git add -A would commit of dir, on main's tip
//	tip, files, err := h.ShowTip(ctx, "main", "go.mod")
//
// A History answers Show, ShowTip, Tip, Files, Tree, Changed, Branched and MergeBase as [git.Git] does, without
// starting git. Its commits and files have the IDs git would give them. TestContract asks real git and the stand-in
// the same questions of one history, and holds both to one answer.
//
// A History has no Check or CommonDir. Each other method of [git.Git] fails the test. So does a question git may
// answer otherwise. Such a question names a Rev the history holds as no branch or full commit ID, such as heads/main.
// It may name a branch in other letters, or a path that is not clean.
// A test that needs one runs on real git. Each commit has one parent at most, so a history holds no merge.
package gittest

import (
	"cmp"
	"context"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/fwilkerson/kiln/internal/git"
)

// History is a repository's commits and branches, held in memory.
type History struct {
	t testing.TB
	// mu.commits, mu.branches and mu.made are read and written only while holding mu.
	mu struct {
		sync.Mutex
		commits  map[git.Rev]commit
		branches map[git.Branch]git.Rev
		// made lists each commit in the order it was made, which a replay into real git follows.
		made []git.Rev
	}
}

// commit is the parent of a commit, "" for none, and each file it holds by path.
type commit struct {
	parent git.Rev
	files  map[string]blob
}

// blob is a file that a commit holds: its content, and its mode as git writes it. A link's content is its target.
type blob struct {
	content, mode string
}

// The modes git writes for a file, an executable and a symbolic link.
const (
	regular    = "100644"
	executable = "100755"
	link       = "120000"
)

// Files maps the path of each file a commit writes to the file's content.
type Files map[string]string

// New is a history with no commit. A question it cannot answer as git would fails t.
func New(t testing.TB) *History {
	h := &History{t: t}
	h.mu.commits, h.mu.branches = map[git.Rev]commit{}, map[git.Branch]git.Rev{}
	return h
}

// CommitFiles makes a commit on branch's tip that writes each of files, and moves branch to it. A branch that is not
// there gets its first commit. CommitFiles answers the commit.
func (h *History) CommitFiles(branch git.Branch, files Files) git.Rev {
	h.t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	tip := h.mu.branches[branch]
	held := maps.Clone(h.mu.commits[tip].files)
	if held == nil {
		held = map[string]blob{}
	}

	for p, content := range files {
		held[p] = blob{content: content, mode: regular}
	}

	return h.commit(branch, commit{parent: tip, files: held})
}

// CommitRemoved makes a commit on branch's tip without the files at paths, as git rm would. It moves branch to it.
// A path where the tip holds no file fails the test. CommitRemoved answers the commit.
func (h *History) CommitRemoved(branch git.Branch, paths ...string) git.Rev {
	h.t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	tip := h.mu.branches[branch]
	held := maps.Clone(h.mu.commits[tip].files)
	for _, p := range paths {
		if _, found := held[p]; !found {
			h.t.Fatalf("gittest: %s holds no file %s to remove", branch, p)
		}

		delete(held, p)
	}

	return h.commit(branch, commit{parent: tip, files: held})
}

// CommitDir makes a commit on branch's tip that holds what git add -A would commit of dir, and nothing else. It
// keeps each file's executable bit, and each symbolic link as its target. It skips dir's .git. It fails the test on
// what git add -A reads that it does not, as unread lists. It moves branch to the commit, and answers it. It reads no
// git config, as under hometest, whose HOME sets no core.autocrlf or core.excludesFile.
func (h *History) CommitDir(branch git.Branch, dir string) git.Rev {
	h.t.Helper()
	held, err := snapshot(dir)
	if err != nil {
		h.t.Fatalf("gittest: commit %s: %v", dir, err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	return h.commit(branch, commit{parent: h.mu.branches[branch], files: held})
}

// snapshot reads each file under dir as git add -A would add it, by its path from dir.
func snapshot(dir string) (map[string]blob, error) {
	held := map[string]blob{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(dir, p)
		if err != nil {
			return err
		}

		if why := unread(p, filepath.ToSlash(rel), d); why != "" {
			return fmt.Errorf("%s: %s; run this test on real git", rel, why)
		}

		switch {
		case d.IsDir() && rel == ".git":
			return filepath.SkipDir
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			held[filepath.ToSlash(rel)] = blob{content: target, mode: link}
			return err
		case !d.Type().IsRegular():
			return nil
		}

		return readFile(held, p, filepath.ToSlash(rel))
	})

	return held, err
}

// unread says why git add -A would read the entry d, at p, otherwise than snapshot does. rel is d's path from the top.
// It answers "" for an entry both read alike.
func unread(p, rel string, d fs.DirEntry) string {
	switch {
	case rel == ".git" && d.IsDir():
		return cmp.Or(excluded(filepath.Join(p, "info", "exclude")), converts(filepath.Join(p, "info", "attributes")))
	case d.Name() == ".git":
		return "git add -A reads a .git below the top, or a linked worktree's, as a repository"
	case d.Name() == ".gitignore":
		return "the stand-in reads no .gitignore"
	case d.Name() == ".gitattributes":
		return converts(p)
	}

	return ""
}

// excluded says that the exclude file at p leaves files out of git add -A, where it holds a pattern. It answers ""
// for a file of comments alone, as git init writes, or none.
func excluded(p string) string {
	raw, _ := os.ReadFile(p)
	for line := range strings.Lines(string(raw)) {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			return fmt.Sprintf("its .git/info/exclude leaves %s out, which the stand-in does not read", line)
		}
	}

	return ""
}

// converts names an attribute in the attributes file at p that changes what git add -A stores of a file. It answers
// "" for a file that names none, or no file.
func converts(p string) string {
	raw, _ := os.ReadFile(p)
	for line := range strings.Lines(string(raw)) {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}

		for _, attr := range fields[1:] {
			if name, _, _ := strings.Cut(strings.TrimLeft(attr, "-!"), "="); slices.Contains(converters, name) {
				return fmt.Sprintf("its attribute %s changes what git add -A stores, which the stand-in does not", name)
			}
		}
	}

	return ""
}

// converters are the attributes that change what git add -A stores: line ends, a filter, $Id$, or an encoding.
var converters = []string{"text", "crlf", "eol", "filter", "ident", "working-tree-encoding"}

// readFile adds the file at p to held, at rel, with git's mode for it. git reads the owner's executable bit alone.
func readFile(held map[string]blob, p, rel string) error {
	info, err := os.Stat(p)
	if err != nil {
		return err
	}

	content, err := os.ReadFile(p)
	mode := regular
	if info.Mode()&0o100 != 0 {
		mode = executable
	}

	held[rel] = blob{content: string(content), mode: mode}
	return err
}

// commit records c, and moves branch to it. A file c holds where another holds a directory fails the test.
func (h *History) commit(branch git.Branch, c commit) git.Rev {
	h.t.Helper()
	for p := range c.files {
		for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
			if _, file := c.files[dir]; file {
				h.t.Fatalf("gittest: %s is a file, which cannot hold %s", dir, p)
			}
		}
	}

	id := commitID(treeID(c.files, ""), c.parent)
	if _, made := h.mu.commits[id]; !made {
		h.mu.commits[id] = c
		h.mu.made = append(h.mu.made, id)
	}

	h.mu.branches[branch] = id
	return id
}

// Branch points branch at the commit at, which the history must hold.
func (h *History) Branch(branch git.Branch, at git.Rev) {
	h.t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, found := h.mu.commits[at]; !found {
		h.t.Fatalf("gittest: branch %s: the history holds no commit %s", branch, at)
	}

	h.mu.branches[branch] = at
}

// Show reads the file at path in the commit at, as [git.Git.Show] does.
func (h *History) Show(_ context.Context, at git.Rev, path string) ([]byte, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id, err := h.id(at)
	if err == nil {
		err = h.unclean("Show", path)
	}

	if err != nil {
		return nil, false, err
	}

	b, found := h.mu.commits[id].files[path]
	if !found {
		return nil, false, nil
	}

	return []byte(b.content), true, nil
}

// ShowTip reads the files at paths in the commit branch points to, as [git.Git.ShowTip] does.
func (h *History) ShowTip(_ context.Context, branch git.Branch, paths ...string) (git.Rev, map[string][]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	tip, found := h.mu.branches[branch]
	if !found {
		return "", nil, h.missing(branch)
	}

	files := map[string][]byte{}
	for _, p := range paths {
		if err := h.unclean("ShowTip", p); err != nil {
			return "", nil, err
		}

		if b, found := h.mu.commits[tip].files[p]; found {
			files[p] = []byte(b.content)
		}
	}

	return tip, files, nil
}

// Tip is the commit branch points to, as [git.Git.Tip] answers it.
func (h *History) Tip(_ context.Context, branch git.Branch) (git.Rev, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	tip, found := h.mu.branches[branch]
	if !found {
		return "", h.missing(branch)
	}

	return tip, nil
}

// missing is the error of branch, which the history lacks: an ErrNoBranch, as git answers. A name the history holds
// in other letters fails the test. Where the disk folds case, git may read branch as that one.
func (h *History) missing(branch git.Branch) error {
	for held := range h.mu.branches {
		if strings.EqualFold(string(held), string(branch)) {
			h.t.Errorf("gittest: %s names %s in other letters, which git may read as %s: run this test on real git",
				branch, held, held)
			return fmt.Errorf("gittest: %s is %s in other letters", branch, held)
		}
	}

	return fmt.Errorf("%w %s", git.ErrNoBranch, branch)
}

// Files lists the names of the entries directly in dir, in the commit at, as [git.Git.Files] does.
func (h *History) Files(_ context.Context, at git.Rev, dir string) ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id, err := h.id(at)
	if err == nil {
		err = h.unclean("Files", dir)
	}

	if err != nil {
		return nil, err
	}

	var names []string
	for _, c := range children(h.mu.commits[id].files, dir) {
		names = append(names, c.name)
	}

	return names, nil
}

// Tree lists each file and directory under dir in the commit at, as [git.Git.Tree] does.
func (h *History) Tree(_ context.Context, at git.Rev, dir string) ([]git.Entry, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id, err := h.id(at)
	if err == nil {
		err = h.unclean("Tree", dir)
	}

	if err != nil {
		return nil, err
	}

	return entries(h.mu.commits[id].files, dir, dir), nil
}

// entries lists each file and directory under dir, in files, as [git.Git.Tree] lists them under top.
func entries(files map[string]blob, top, dir string) []git.Entry {
	var list []git.Entry
	for _, c := range children(files, dir) {
		rel := strings.TrimPrefix(c.path, top+"/")
		if !c.dir {
			list = append(list, git.Entry{Path: rel, Exec: c.mode == executable, ID: object("blob", c.content)})
			continue
		}

		list = append(list, git.Entry{Path: rel, Dir: true, ID: treeID(files, c.path)})
		list = append(list, entries(files, top, c.path)...)
	}

	return list
}

// Changed lists the paths that differ between the commits from and to, as [git.Git.Changed] does.
func (h *History) Changed(_ context.Context, from, to git.Rev) ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	a, err := h.id(from)
	if err != nil {
		return nil, err
	}

	b, err := h.id(to)
	if err != nil {
		return nil, err
	}

	return h.changed(a, b), nil
}

// changed lists, sorted, each path whose file differs between the commits a and b, or that one of them lacks.
func (h *History) changed(a, b git.Rev) []string {
	from, to := h.mu.commits[a].files, h.mu.commits[b].files
	var paths []string
	for p, f := range from {
		if g, found := to[p]; !found || g != f {
			paths = append(paths, p)
		}
	}

	for p := range to {
		if _, found := from[p]; !found {
			paths = append(paths, p)
		}
	}

	slices.Sort(paths)
	return paths
}

// Branched lists the paths that head changed since it left main's line, as [git.Git.Branched] does.
func (h *History) Branched(_ context.Context, main, head git.Rev) ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	base, err := h.mergeBase(main, head)
	if err != nil {
		return nil, err
	}

	tip, err := h.id(head)
	return h.changed(base, tip), err
}

// MergeBase is the newest commit that both a and b descend from, as [git.Git.MergeBase] answers it.
func (h *History) MergeBase(_ context.Context, a, b git.Rev) (git.Rev, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.mergeBase(a, b)
}

// mergeBase is the newest commit on both a's line and b's. Each commit has one parent at most, so it is the first of
// b's line that a's holds.
func (h *History) mergeBase(a, b git.Rev) (git.Rev, error) {
	from, err := h.id(a)
	if err != nil {
		return "", err
	}

	to, err := h.id(b)
	if err != nil {
		return "", err
	}

	line := map[git.Rev]bool{}
	for at := from; at != ""; at = h.mu.commits[at].parent {
		line[at] = true
	}

	for at := to; at != ""; at = h.mu.commits[at].parent {
		if line[at] {
			return at, nil
		}
	}

	return "", fmt.Errorf("gittest: %s and %s share no commit", a, b)
}

// fullID matches a commit's full ID, which git reads as one before it reads a branch's name.
var fullID = regexp.MustCompile(`^[0-9a-f]{40}$`)

// id is the commit at names: a held commit's full ID, or a held branch's name. Any other Rev fails the test. git
// may read it as a commit the history holds, as heads/main, or MAIN where the disk folds case.
func (h *History) id(at git.Rev) (git.Rev, error) {
	if _, held := h.mu.commits[at]; held {
		return at, nil
	}

	if tip, held := h.mu.branches[git.Branch(at)]; held && !fullID.MatchString(string(at)) {
		return tip, nil
	}

	h.t.Errorf("gittest: the history holds no commit or branch %q, which git may read otherwise: run this test on "+
		"real git", at)
	return "", fmt.Errorf("gittest: %q names no commit or branch the history holds", at)
}

// unclean fails the test where p is no clean path from the top, which call reads otherwise in git. It then answers
// an error, and nil for a clean path.
func (h *History) unclean(call, p string) error {
	switch {
	case p == "", p == ".", path.Clean(p) != p, path.IsAbs(p), p == "..", strings.HasPrefix(p, "../"):
		h.t.Errorf("gittest: %s of %q: the stand-in reads only a clean path from the top; run this test on real git",
			call, p)
		return fmt.Errorf("gittest: %q is no clean path", p)
	}

	return nil
}

// child is an entry directly in a commit's directory: a file or a directory, by its name and path. A file holds its
// blob.
type child struct {
	name, path string
	dir        bool
	blob
}

// children lists the entries directly in dir, in files, in git's order. That is by name, with a slash after a
// directory's. The top directory is "".
func children(files map[string]blob, dir string) []child {
	prefix := dir + "/"
	if dir == "" {
		prefix = ""
	}

	found := map[string]child{}
	for p, b := range files {
		rest, under := strings.CutPrefix(p, prefix)
		if !under {
			continue
		}

		name, _, nested := strings.Cut(rest, "/")
		c := child{name: name, path: prefix + name, dir: nested}
		if !nested {
			c.blob = b
		}

		found[name] = c
	}

	return slices.SortedFunc(maps.Values(found), func(a, b child) int { return strings.Compare(a.key(), b.key()) })
}

// key is c's name as git sorts it: a directory's ends in /.
func (c child) key() string {
	if c.dir {
		return c.name + "/"
	}

	return c.name
}
