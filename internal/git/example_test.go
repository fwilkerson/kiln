package git_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/fwilkerson/kiln/internal/git"
)

// Example lands a branch on main as kiln lands a build: check out, commit, rebase, then move main.
func Example() {
	ctx := context.Background()
	dir := must(os.MkdirTemp("", "git-example"))
	defer os.RemoveAll(dir)
	repo := filepath.Join(dir, "repo")
	start := `git init --quiet -b main "$1" && cd "$1" && git config user.name dev && ` +
		"git config user.email dev@example.com && git commit --quiet --allow-empty -m first"
	must(exec.Command("/bin/sh", "-c", start, "sh", repo).Output())

	g := must(git.New(repo, os.Environ()))
	tree := git.Worktree(filepath.Join(dir, "x"))
	check(g.Checkout(ctx, tree, "kiln/x", "main"))
	check(os.WriteFile(filepath.Join(string(tree), "x.go"), []byte("package x\n"), 0o600))
	must(g.Commit(ctx, tree, "a session's work"))

	onto := must(g.Tip(ctx, "main"))
	landed, conflict, err := g.Rebase(ctx, tree, onto, "feat(x): land the approved spec")
	check(err)
	fmt.Println("conflict:", conflict)
	fmt.Println("landed:", must(g.Changed(ctx, onto, landed)))

	way := must(g.Move(ctx, "main", landed, onto))
	fmt.Println("moved:", way == nil)
	stale := must(g.Move(ctx, "main", landed, onto))
	fmt.Println("refused, since main is no longer at onto:", stale.Tip == landed)
	check(g.Remove(ctx, []git.Worktree{tree}, "kiln/x"))
	// Output:
	// conflict: <nil>
	// landed: [x.go]
	// moved: true
	// refused, since main is no longer at onto: true
}

func must[T any](v T, err error) T {
	check(err)
	return v
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
