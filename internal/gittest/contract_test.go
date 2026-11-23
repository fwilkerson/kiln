package gittest

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/fwilkerson/kiln/internal/git"
)

// reads are the methods of [git.Git] that a History answers too.
type reads interface {
	Show(ctx context.Context, at git.Rev, path string) ([]byte, bool, error)
	ShowTip(ctx context.Context, branch git.Branch, paths ...string) (git.Rev, map[string][]byte, error)
	Tip(ctx context.Context, branch git.Branch) (git.Rev, error)
	Files(ctx context.Context, at git.Rev, dir string) ([]string, error)
	Tree(ctx context.Context, at git.Rev, dir string) ([]git.Entry, error)
	Changed(ctx context.Context, from, to git.Rev) ([]string, error)
	Branched(ctx context.Context, main, head git.Rev) ([]string, error)
	MergeBase(ctx context.Context, a, b git.Rev) (git.Rev, error)
}

// TestContract holds the stand-in to real git. Each row asks both one question of one history, and wants one answer.
func TestContract(t *testing.T) {
	t.Parallel()
	h, g := history(t)
	for adapter, port := range map[string]reads{"the stand-in": h, "git": replay(t, h)} {
		for _, tc := range contract {
			t.Run(adapter+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				if got := tc.ask(t.Context(), port, g); got != tc.want {
					t.Errorf("%s answered %s; want %s", adapter, got, tc.want)
				}
			})
		}
	}
}

// contract is each question TestContract asks of the history that history makes, and the answer it wants.
var contract = []struct {
	name string
	ask  func(ctx context.Context, r reads, g given) string
	want string
}{
	{"Show: a file", func(ctx context.Context, r reads, g given) string {
		return shown(r.Show(ctx, g.second, "dir/g"))
	}, `"in\n" true, no error`},
	{"Show: a file in a directory in a directory", func(ctx context.Context, r reads, g given) string {
		return shown(r.Show(ctx, g.second, "dir/sub/h"))
	}, `"deeper\n" true, no error`},
	{"Show: a name with a space", func(ctx context.Context, r reads, g given) string {
		return shown(r.Show(ctx, g.first, "with space"))
	}, `"a\nb" true, no error`},
	{"Show: an empty file", func(ctx context.Context, r reads, g given) string {
		return shown(r.Show(ctx, g.first, "empty"))
	}, `"" true, no error`},
	{"Show: a directory", func(ctx context.Context, r reads, g given) string {
		return shown(r.Show(ctx, g.second, "dir"))
	}, `"" false, no error`},
	{"Show: a path the commit lacks", func(ctx context.Context, r reads, g given) string {
		return shown(r.Show(ctx, g.first, "new/n"))
	}, `"" false, no error`},
	{"Show: at a branch, by its name", func(ctx context.Context, r reads, g given) string {
		return shown(r.Show(ctx, "old", "f"))
	}, `"one\n" true, no error`},
	{"Show: a link, as its target", func(ctx context.Context, r reads, g given) string {
		return shown(r.Show(ctx, g.exec, "bin/link"))
	}, `"lib/x" true, no error`},
	{"Show: a commit git lacks", func(ctx context.Context, r reads, g given) string {
		return shown(r.Show(ctx, lacked, "f"))
	}, `"" false, an error`},
	{"Show: a branch git lacks", func(ctx context.Context, r reads, g given) string {
		return shown(r.Show(ctx, "none", "f"))
	}, `"" false, an error`},
	{"ShowTip: the tip's files", func(ctx context.Context, r reads, g given) string {
		return g.tipShown(r.ShowTip(ctx, "main", "f", "dir/g"))
	}, `second map["dir/g":"in\n" "f":"two\n"], no error`},
	{"ShowTip: paths the tip lacks", func(ctx context.Context, r reads, g given) string {
		return g.tipShown(r.ShowTip(ctx, "main", "none", "dir", "f"))
	}, `second map["f":"two\n"], no error`},
	{"ShowTip: a branch git lacks", func(ctx context.Context, r reads, g given) string {
		return g.tipShown(r.ShowTip(ctx, "none", "f"))
	}, `no commit map[], no branch`},
	{"Tip: a branch", func(ctx context.Context, r reads, g given) string {
		tip, err := r.Tip(ctx, "old")
		return g.name(tip) + ", " + kind(err)
	}, "first, no error"},
	{"Tip: a branch git lacks", func(ctx context.Context, r reads, g given) string {
		tip, err := r.Tip(ctx, "none")
		return g.name(tip) + ", " + kind(err)
	}, "no commit, no branch"},
	{"Files: a directory's files and directories", func(ctx context.Context, r reads, g given) string {
		return listed(r.Files(ctx, g.second, "dir"))
	}, "[g sub], no error"},
	{"Files: a directory in a directory", func(ctx context.Context, r reads, g given) string {
		return listed(r.Files(ctx, "main", "dir/sub"))
	}, "[h], no error"},
	{"Files: in git's order, a directory's name as if it ended in a slash", func(ctx context.Context, r reads,
		g given) string {
		return listed(r.Files(ctx, g.first, "order"))
	}, "[a-b a.txt a a0], no error"},
	{"Files: a directory the commit lacks", func(ctx context.Context, r reads, g given) string {
		return listed(r.Files(ctx, g.second, "none"))
	}, "[], no error"},
	{"Files: a file's path", func(ctx context.Context, r reads, g given) string {
		return listed(r.Files(ctx, g.second, "f"))
	}, "[], no error"},
	{"Files: a directory whose last file a commit removed", func(ctx context.Context, r reads, g given) string {
		return listed(r.Files(ctx, "pruned", "dir"))
	}, "[g], no error"},
	{"Files: a commit git lacks", func(ctx context.Context, r reads, g given) string {
		return listed(r.Files(ctx, lacked, "dir"))
	}, "[], an error"},
	{"Tree: each directory before what it holds, a link as a file", func(ctx context.Context, r reads,
		g given) string {
		return entered(r.Tree(ctx, g.plain, "bin"))
	}, "[lib/ ab69b4a lib/x 587be6b link e5224d5 run 1a24852], no error"},
	{"Tree: an executable file", func(ctx context.Context, r reads, g given) string {
		return entered(r.Tree(ctx, g.exec, "bin"))
	}, "[lib/ ab69b4a lib/x 587be6b link ae9e223 run* 1a24852], no error"},
	{"Tree: in git's order", func(ctx context.Context, r reads, g given) string {
		return entered(r.Tree(ctx, g.first, "order"))
	}, "[a-b e69de29 a.txt e69de29 a/ 5805b67 a/x e69de29 a0 e69de29], no error"},
	{"Tree: a directory whose last file a commit removed", func(ctx context.Context, r reads, g given) string {
		return entered(r.Tree(ctx, g.pruned, "dir"))
	}, "[g 4935e88], no error"},
	{"Tree: a directory the commit lacks", func(ctx context.Context, r reads, g given) string {
		return entered(r.Tree(ctx, g.second, "none"))
	}, "[], no error"},
	{"Tree: a file's path", func(ctx context.Context, r reads, g given) string {
		return entered(r.Tree(ctx, g.second, "f"))
	}, "[], no error"},
	{"Changed: files changed, added, and changed in a directory in a directory", func(ctx context.Context,
		r reads, g given) string {
		return listed(r.Changed(ctx, g.first, g.second))
	}, "[dir/sub/h f new/n], no error"},
	{"Changed: one commit", func(ctx context.Context, r reads, g given) string {
		return listed(r.Changed(ctx, g.second, g.second))
	}, "[], no error"},
	{"Changed: an executable bit, and a link's target", func(ctx context.Context, r reads, g given) string {
		return listed(r.Changed(ctx, g.plain, g.exec))
	}, "[bin/link bin/run], no error"},
	{"Changed: each file removed", func(ctx context.Context, r reads, g given) string {
		return listed(r.Changed(ctx, g.side, g.orphan))
	}, "[dir/g dir/sub/h empty f o order/a-b order/a.txt order/a/x order/a0 s with space], no error"},
	{"Changed: a file removed", func(ctx context.Context, r reads, g given) string {
		return listed(r.Changed(ctx, g.second, g.pruned))
	}, "[dir/sub/h], no error"},
	{"Changed: a commit git lacks", func(ctx context.Context, r reads, g given) string {
		return listed(r.Changed(ctx, g.first, lacked))
	}, "[], an error"},
	{"Branched: what a branch changed since it left main", func(ctx context.Context, r reads, g given) string {
		return listed(r.Branched(ctx, "main", "side"))
	}, "[s], no error"},
	{"Branched: what main changed since a branch left it", func(ctx context.Context, r reads, g given) string {
		return listed(r.Branched(ctx, "side", "main"))
	}, "[dir/sub/h f new/n], no error"},
	{"Branched: main against itself", func(ctx context.Context, r reads, g given) string {
		return listed(r.Branched(ctx, "main", g.second))
	}, "[], no error"},
	{"Branched: a branch that shares no commit with main", func(ctx context.Context, r reads, g given) string {
		return listed(r.Branched(ctx, "main", "orphan"))
	}, "[], an error"},
	{"MergeBase: where a branch left main", func(ctx context.Context, r reads, g given) string {
		return g.based(r.MergeBase(ctx, "main", "side"))
	}, "first, no error"},
	{"MergeBase: either way round", func(ctx context.Context, r reads, g given) string {
		return g.based(r.MergeBase(ctx, g.side, "main"))
	}, "first, no error"},
	{"MergeBase: a commit that main descends from", func(ctx context.Context, r reads, g given) string {
		return g.based(r.MergeBase(ctx, "main", "old"))
	}, "first, no error"},
	{"MergeBase: one commit", func(ctx context.Context, r reads, g given) string {
		return g.based(r.MergeBase(ctx, g.second, "main"))
	}, "second, no error"},
	{"MergeBase: two commits that share none", func(ctx context.Context, r reads, g given) string {
		return g.based(r.MergeBase(ctx, "main", "orphan"))
	}, "no commit, an error"},
	{"MergeBase: a branch git lacks", func(ctx context.Context, r reads, g given) string {
		return g.based(r.MergeBase(ctx, "none", "main"))
	}, "no commit, an error"},
}

// lacked is the ID of a commit that no history holds.
var lacked = git.Rev(strings.Repeat("1", 40))

// given names the commits of the history that history makes.
type given struct {
	first, second, side, orphan, pruned, plain, exec git.Rev
}

// history makes the history each row of contract asks about. main has two commits, first and second. old points at
// first, and side has a commit of its own on it. orphan shares no commit with main. pruned removes dir/sub/h from
// second. tools holds two commits of a directory, plain and then exec, which differ in an executable bit and a link.
func history(t *testing.T) (*History, given) {
	h := New(t)
	var g given
	g.first = h.CommitFiles("main", Files{"f": "one\n", "dir/g": "in\n", "dir/sub/h": "deep\n", "with space": "a\nb",
		"empty": "", "order/a.txt": "", "order/a-b": "", "order/a/x": "", "order/a0": ""})
	h.Branch("old", g.first)
	h.Branch("side", g.first)
	g.side = h.CommitFiles("side", Files{"s": "side\n"})
	g.second = h.CommitFiles("main", Files{"f": "two\n", "dir/sub/h": "deeper\n", "new/n": "n\n"})
	g.orphan = h.CommitFiles("orphan", Files{"o": "o\n"})
	h.Branch("pruned", g.second)
	g.pruned = h.CommitRemoved("pruned", "dir/sub/h")
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(filepath.Join(bin, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}

	write(t, filepath.Join(bin, "run"), "#!/bin/sh\n", 0o644)
	write(t, filepath.Join(bin, "lib", "x"), "x\n", 0o644)
	symlink(t, "run", filepath.Join(bin, "link"))
	g.plain = h.CommitDir("tools", dir)
	if err := os.Chmod(filepath.Join(bin, "run"), 0o755); err != nil {
		t.Fatal(err)
	}

	symlink(t, "lib/x", filepath.Join(bin, "link"))
	g.exec = h.CommitDir("tools", dir)
	return h, g
}

// write writes content to the file at p, with perm.
func write(t *testing.T, p, content string, perm os.FileMode) {
	if err := os.WriteFile(p, []byte(content), perm); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(p, perm); err != nil {
		t.Fatal(err)
	}
}

// symlink makes the file at p a symbolic link to target, in place of whatever was there.
func symlink(t *testing.T, target, p string) {
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}

	if err := os.Symlink(target, p); err != nil {
		t.Fatal(err)
	}
}

// replay makes a repository of h's commits and branches, through git fast-import, and answers its git. It fails t
// where git names a branch's commit otherwise than h does.
func replay(t *testing.T, h *History) *git.Git {
	repo := t.TempDir()
	h.mu.Lock()
	defer h.mu.Unlock()
	cmd := exec.Command("/bin/sh", "-c", `git init --quiet -b main && git fast-import --quiet`)
	cmd.Dir, cmd.Stdin = repo, strings.NewReader(h.stream())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git fast-import: %v\n%s", err, out)
	}

	g, err := git.New(repo, os.Environ())
	if err != nil {
		t.Fatal(err)
	}

	for branch, tip := range h.mu.branches {
		if got, err := g.Tip(t.Context(), branch); err != nil || got != tip {
			t.Fatalf("git names %s's commit %s (%v); the stand-in names it %s", branch, got, err, tip)
		}
	}

	return g
}

// stream is what git fast-import reads to make h's commits and branches. Each commit holds its files, and nothing
// that its parent holds besides.
func (h *History) stream() string {
	var s strings.Builder
	mark := map[git.Rev]int{}
	for i, id := range h.mu.made {
		c := h.mu.commits[id]
		mark[id] = i + 1
		fmt.Fprintf(&s, "reset refs/gittest/replay\ncommit refs/gittest/replay\nmark :%d\ncommitter %s\n", i+1, author)
		fmt.Fprintf(&s, "data %d\n%s", len(message), message)
		if c.parent != "" {
			fmt.Fprintf(&s, "from :%d\n", mark[c.parent])
		}

		s.WriteString("deleteall\n")
		for _, p := range slices.Sorted(maps.Keys(c.files)) {
			fmt.Fprintf(&s, "M %s inline %s\ndata %d\n%s\n", c.files[p].mode, p, len(c.files[p].content),
				c.files[p].content)
		}
	}

	for branch, tip := range h.mu.branches {
		fmt.Fprintf(&s, "reset refs/heads/%s\nfrom :%d\n\n", branch, mark[tip])
	}

	return s.String()
}

// name is the field of g that holds rev, or "no commit" for "".
func (g given) name(rev git.Rev) string {
	names := map[git.Rev]string{"": "no commit", g.first: "first", g.second: "second", g.side: "side",
		g.orphan: "orphan", g.pruned: "pruned", g.plain: "plain", g.exec: "exec"}
	if name, found := names[rev]; found {
		return name
	}

	return string(rev)
}

// kind names the kind of err, as a row wants it.
func kind(err error) string {
	switch {
	case errors.Is(err, git.ErrNoBranch):
		return "no branch"
	case err != nil:
		return "an error"
	}

	return "no error"
}

// shown is what Show answered, as a row wants it.
func shown(content []byte, found bool, err error) string {
	return fmt.Sprintf("%q %v, %s", content, found, kind(err))
}

// tipShown is what ShowTip answered, as a row wants it.
func (g given) tipShown(tip git.Rev, files map[string][]byte, err error) string {
	return fmt.Sprintf("%s %q, %s", g.name(tip), files, kind(err))
}

// listed is a list of names or paths a call answered, as a row wants it.
func listed(names []string, err error) string {
	return fmt.Sprintf("%v, %s", names, kind(err))
}

// based is what MergeBase answered, as a row wants it.
func (g given) based(base git.Rev, err error) string {
	return g.name(base) + ", " + kind(err)
}

// entered is what Tree answered, as a row wants it: each entry's path and the start of its ID. A directory's path
// ends in a slash, and an executable's in an asterisk.
func entered(entries []git.Entry, err error) string {
	var list []string
	for _, e := range entries {
		mark := map[bool]string{true: "/"}[e.Dir] + map[bool]string{true: "*"}[e.Exec]
		list = append(list, e.Path+mark, e.ID[:min(7, len(e.ID))])
	}

	return listed(list, err)
}
