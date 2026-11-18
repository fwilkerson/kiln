package git_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fwilkerson/kiln/internal/git"
)

// TestMove holds the rule a landing rests on: main moves only from the commit the landing gated on.
func TestMove(t *testing.T) {
	for name, tc := range map[string]struct {
		expected func(h history) git.Rev
		want     func(h history) outcome
	}{
		"main is where the landing found it": {
			expected: func(h history) git.Rev { return h.first },
			want:     func(h history) outcome { return outcome{main: h.landed, file: true} },
		},
		"another landing moved main first": {
			expected: func(h history) git.Rev { return h.second },
			want:     func(h history) outcome { return outcome{refused: true, refusedAt: h.first, main: h.first} },
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := makeHistory(t)
			way, err := h.git.Move(t.Context(), "main", h.landed, tc.expected(h))
			if err != nil {
				t.Fatal(err)
			}

			if got, want := h.outcome(t, way), tc.want(h); got != want {
				t.Errorf("got %+v; want %+v", got, want)
			}
		})
	}
}

// outcome is what a move left. It holds any refusal and the tip it names, where main is, and whether landed is out.
type outcome struct {
	refused   bool
	refusedAt git.Rev
	main      git.Rev
	file      bool
}

func (h history) outcome(t *testing.T, way *git.InTheWay) outcome {
	o := outcome{refused: way != nil}
	if way != nil {
		o.refusedAt = way.Tip
	}

	o.main, _ = h.git.Tip(t.Context(), "main")
	_, err := os.Stat(filepath.Join(h.repo, "landed"))
	o.file = err == nil
	return o
}

// history is a repository whose main is at first. second is on another branch, and landed descends from first.
type history struct {
	git    *git.Git
	repo   string
	first  git.Rev
	second git.Rev
	landed git.Rev
}

func makeHistory(t *testing.T) history {
	h := history{repo: t.TempDir()}
	sh(t, h.repo, "git init --quiet -b main && git config user.name t && git config user.email t@t")
	sh(t, h.repo, "git commit --quiet --allow-empty -m first")
	h.first = git.Rev(sh(t, h.repo, "git rev-parse HEAD"))
	sh(t, h.repo, "git commit --quiet --allow-empty -m second && git checkout --quiet -b other")
	h.second = git.Rev(sh(t, h.repo, "git rev-parse HEAD"))
	sh(t, h.repo, "git checkout --quiet main && git reset --quiet --hard "+string(h.first))
	sh(t, h.repo, "echo landed >landed && git add landed && git commit --quiet -m landed")
	h.landed = git.Rev(sh(t, h.repo, "git rev-parse HEAD"))
	sh(t, h.repo, "git reset --quiet --hard "+string(h.first))
	g, err := git.New(h.repo, os.Environ())
	if err != nil {
		t.Fatal(err)
	}

	h.git = g
	return h
}

func sh(t *testing.T, dir, script string) string {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", script, err, out)
	}

	return strings.TrimSpace(string(out))
}

// TestMoveInterrupted holds that an interrupt never cuts a fast-forward short. A killed merge would leave the
// checkout half-updated, and index.lock behind.
func TestMoveInterrupted(t *testing.T) {
	h := makeHistory(t)
	blob := sh(t, h.repo, "echo many | git hash-object -w --stdin")
	var tree strings.Builder
	for i := range 2000 {
		fmt.Fprintf(&tree, "100644 blob %s\t%d\n", blob, i)
	}

	listing := filepath.Join(t.TempDir(), "tree")
	if err := os.WriteFile(listing, []byte(tree.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	many := git.Rev(sh(t, h.repo, "git commit-tree -p HEAD -m many $(git mktree <"+listing+")"))
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := h.git.Move(ctx, "main", many, h.first); err != nil {
		t.Logf("Move: %v", err)
	}

	if main, _ := h.git.Tip(t.Context(), "main"); main != many && main != h.first {
		t.Errorf("main is at %s; want it at first, %s, or landed, %s", main, h.first, many)
	}

	status := sh(t, h.repo, "git status --porcelain --untracked-files=all")
	if _, locked := os.Stat(filepath.Join(h.repo, ".git", "index.lock")); locked == nil || status != "" {
		t.Errorf("index.lock left: %t; status:\n%.500s\nwant no lock, and the checkout as main has it",
			locked == nil, status)
	}
}
