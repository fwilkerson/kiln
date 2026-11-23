package gittest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/fwilkerson/kiln/internal/git"
)

// TestCommitDir holds that CommitDir commits what git add -A commits of a directory. That is each file with its
// executable bit, as git reads the owner's, and each link. It is not an empty directory, or the directory .git. A
// .gitattributes that changes no content, and git init's exclude of comments alone, change nothing.
func TestCommitDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, sub := range []string{"empty", "d/e"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	for name, perm := range map[string]os.FileMode{"plain": 0o644, "run": 0o755, "group-run": 0o654,
		"d/e/with space": 0o600, "d/f": 0o700} {
		write(t, filepath.Join(dir, name), name+"\n", perm)
	}

	symlink(t, "plain", filepath.Join(dir, "to-file"))
	symlink(t, "d", filepath.Join(dir, "to-dir"))
	write(t, filepath.Join(dir, ".gitattributes"), "# kiln's own\n* linguist-generated\n*.png -diff binary\n", 0o644)
	cmd := exec.Command("/bin/sh", "-c", "git init --quiet && git add -A && git write-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}

	h := New(t)
	tip := h.CommitDir("main", dir)
	if got, want := treeID(h.mu.commits[tip].files, ""), strings.TrimSpace(string(out)); got != want {
		t.Errorf("CommitDir committed the tree %s; git add -A commits %s", got, want)
	}
}

// TestFailsLoudly holds that a test fails when it asks what the stand-in does not answer, or git reads otherwise. Each
// such call answers an error too.
func TestFailsLoudly(t *testing.T) {
	t.Parallel()
	for name, call := range loud {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := errEnded
			said := failures(t, func(h *History) {
				main := h.CommitFiles("main", Files{"f": "one\n", "dir/g": "in\n"})
				err = call(t.Context(), h, main)
			})
			if len(said) == 0 || err == nil {
				t.Errorf("the test failed %q, and the call answered %v; want it failed, and an error", said, err)
			}
		})
	}
}

// loud is each call that fails the test. Its history's branch main holds f and dir/g, at the commit main.
var loud = map[string]func(ctx context.Context, h *History, main git.Rev) error{
	"Checkout":  func(ctx context.Context, h *History, main git.Rev) error { return h.Checkout(ctx, "w", "b", main) },
	"Clone":     func(ctx context.Context, h *History, main git.Rev) error { return h.Clone(ctx, "w", "main", "b") },
	"Detached":  func(ctx context.Context, h *History, main git.Rev) error { return h.Detached(ctx, "w", main) },
	"Fetch":     func(ctx context.Context, h *History, main git.Rev) error { return h.Fetch(ctx, "w", "b") },
	"Pull":      func(ctx context.Context, h *History, main git.Rev) error { return h.Pull(ctx, "w", "main") },
	"Remove":    func(ctx context.Context, h *History, main git.Rev) error { return h.Remove(ctx, nil, "b") },
	"Reset":     func(ctx context.Context, h *History, main git.Rev) error { return h.Reset(ctx, "w", main) },
	"Restore":   func(ctx context.Context, h *History, main git.Rev) error { return h.Restore(ctx, "w", main, "f") },
	"Commit":    func(ctx context.Context, h *History, main git.Rev) error { return second(h.Commit(ctx, "w", "m")) },
	"Diff":      func(ctx context.Context, h *History, main git.Rev) error { return second(h.Diff(ctx, main, main)) },
	"EmptyTree": func(ctx context.Context, h *History, main git.Rev) error { return second(h.EmptyTree(ctx)) },
	"Head":      func(ctx context.Context, h *History, main git.Rev) error { return second(h.Head(ctx, "w")) },
	"Merge":     func(ctx context.Context, h *History, main git.Rev) error { return second(h.Merge(ctx, "w", "b", "m")) },
	"Move":      func(ctx context.Context, h *History, main git.Rev) error { return second(h.Move(ctx, "b", main, main)) },
	"Shortstat": func(ctx context.Context, h *History, main git.Rev) error { return second(h.Shortstat(ctx, main, main)) },
	"Merged": func(ctx context.Context, h *History, main git.Rev) error {
		_, _, err := h.Merged(ctx, main, main, "f")
		return err
	},
	"Rebase": func(ctx context.Context, h *History, main git.Rev) error {
		_, _, err := h.Rebase(ctx, "w", main, "m")
		return err
	},
	"Status": func(ctx context.Context, h *History, main git.Rev) error {
		_, _, err := h.Status(ctx, "w")
		return err
	},
	"Show at HEAD":           func(ctx context.Context, h *History, main git.Rev) error { return show(ctx, h, "HEAD", "f") },
	"Show at main~1":         func(ctx context.Context, h *History, main git.Rev) error { return show(ctx, h, "main~1", "f") },
	"Show at a full ref":     func(ctx context.Context, h *History, main git.Rev) error { return show(ctx, h, "refs/heads/main", "f") },
	"Show at a shortened ID": func(ctx context.Context, h *History, main git.Rev) error { return show(ctx, h, main[:7], "f") },
	"Show at a range":        func(ctx context.Context, h *History, main git.Rev) error { return show(ctx, h, "main..main", "f") },
	"Show of ./f":            func(ctx context.Context, h *History, main git.Rev) error { return show(ctx, h, main, "./f") },
	"Show of a directory/":   func(ctx context.Context, h *History, main git.Rev) error { return show(ctx, h, main, "dir/") },
	"Show of the top":        func(ctx context.Context, h *History, main git.Rev) error { return show(ctx, h, main, "") },
	"Changed from git's empty tree": func(ctx context.Context, h *History, main git.Rev) error {
		return second(h.Changed(ctx, git.Rev(object("tree", "")), main))
	},
	"ShowTip of a path above the top": func(ctx context.Context, h *History, main git.Rev) error {
		_, _, err := h.ShowTip(ctx, "main", "../f")
		return err
	},
	"Files of the top": func(ctx context.Context, h *History, main git.Rev) error { return second(h.Files(ctx, main, "")) },
	"Tree of /dir": func(ctx context.Context, h *History, main git.Rev) error {
		return second(h.Tree(ctx, main, "/dir"))
	},
	"Show at heads/main, which git reads as main": func(ctx context.Context, h *History, main git.Rev) error {
		return show(ctx, h, "heads/main", "f")
	},
	"Show at MAIN, which git may read as main on a disk that folds case": func(ctx context.Context, h *History,
		main git.Rev) error {
		return show(ctx, h, "MAIN", "f")
	},
	"Show at a branch the history lacks": func(ctx context.Context, h *History, main git.Rev) error {
		return show(ctx, h, "none", "f")
	},
	"Show at a commit the history lacks": func(ctx context.Context, h *History, main git.Rev) error {
		return show(ctx, h, git.Rev(strings.Repeat("1", 40)), "f")
	},
	"ShowTip of MAIN": func(ctx context.Context, h *History, main git.Rev) error {
		_, _, err := h.ShowTip(ctx, "MAIN", "f")
		return err
	},
	"Tip of MAIN": func(ctx context.Context, h *History, main git.Rev) error { return second(h.Tip(ctx, "MAIN")) },
	"CommitDir of a directory that holds a .gitignore": func(ctx context.Context, h *History, main git.Rev) error {
		return commitDir(h, map[string]string{".gitignore": "x\n"})
	},
	"CommitDir of a .gitattributes that names text": func(ctx context.Context, h *History, main git.Rev) error {
		return commitDir(h, map[string]string{".gitattributes": "* text=auto\n", "a.txt": "x\r\n"})
	},
	"CommitDir of a .gitattributes that names eol": func(ctx context.Context, h *History, main git.Rev) error {
		return commitDir(h, map[string]string{"d/.gitattributes": "# ends\n*.sh eol=lf\n", "d/a.sh": "x\r\n"})
	},
	"CommitDir of a .gitattributes that names a filter": func(ctx context.Context, h *History, main git.Rev) error {
		return commitDir(h, map[string]string{".gitattributes": "*.bin filter=lfs -diff\n", "a.bin": "x"})
	},
	"CommitDir of a .gitattributes that names ident": func(ctx context.Context, h *History, main git.Rev) error {
		return commitDir(h, map[string]string{".gitattributes": "*.c ident\n", "a.c": "$Id: x $\n"})
	},
	"CommitDir of a .git directory below the top": func(ctx context.Context, h *History, main git.Rev) error {
		return commitDir(h, map[string]string{"sub/.git/note": "n\n", "sub/f": "f\n"})
	},
	"CommitDir of a repository below the top": func(ctx context.Context, h *History, main git.Rev) error {
		return commitDir(h, map[string]string{"sub/.git/HEAD": "ref: refs/heads/main\n", "sub/f": "f\n"})
	},
	"CommitDir of a worktree below the top": func(ctx context.Context, h *History, main git.Rev) error {
		return commitDir(h, map[string]string{"sub/.git": "gitdir: /elsewhere\n", "sub/f": "f\n"})
	},
	"CommitDir of a pattern in .git/info/exclude": func(ctx context.Context, h *History, main git.Rev) error {
		return commitDir(h, map[string]string{".git/info/exclude": "# git ls-files --others\n*.log\n", "a.log": "x"})
	},
	"CommitFiles of a file under a file": func(ctx context.Context, h *History, main git.Rev) error {
		h.CommitFiles("main", Files{"f/y/x": "x\n"})
		return nil
	},
	"CommitRemoved of a file the tip lacks": func(ctx context.Context, h *History, main git.Rev) error {
		h.CommitRemoved("main", "dir")
		return nil
	},
}

// errEnded is the error of a call that never returned, since its failure ended the goroutine.
var errEnded = errors.New("the call ended its goroutine")

// second is the error that ends what a call answered.
func second[T any](_ T, err error) error {
	return err
}

// commitDir commits a new directory that holds files, by path, with CommitDir. It answers an error from making the
// directory.
func commitDir(h *History, files map[string]string) error {
	dir, err := os.MkdirTemp("", "gittest")
	if err != nil {
		return err
	}

	defer os.RemoveAll(dir)
	for p, content := range files {
		target := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			return err
		}
	}

	h.CommitDir("main", dir)
	return nil
}

// show is the error Show answers at at, of p.
func show(ctx context.Context, h *History, at git.Rev, p string) error {
	_, _, err := h.Show(ctx, at, p)
	return err
}

// recorder is a test that keeps what each failure says, rather than fail. Its Fatalf ends the goroutine, as a test's
// does.
type recorder struct {
	testing.TB
	mu   sync.Mutex
	said []string
}

func (r *recorder) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.said = append(r.said, fmt.Sprintf(format, args...))
}

func (r *recorder) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	runtime.Goexit()
}

// failures runs call on a new history, on its own goroutine, and answers what each failure of the test said.
func failures(t *testing.T, call func(h *History)) []string {
	r := &recorder{TB: t}
	var wg sync.WaitGroup
	wg.Go(func() { call(New(r)) })
	wg.Wait()
	return r.said
}
