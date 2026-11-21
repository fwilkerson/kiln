package kiln_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fwilkerson/kiln/internal/kiln"
)

// approve has the developer approve a spec, x, from /shape's template, and commit .kiln/ on main.
const approve = "mkdir .kiln/specs && " +
	"sed 's/^Status: Proposed/Status: Approved/' .kiln/skills/shape/template.md >.kiln/specs/x.md && " +
	"git add -A && git commit --quiet --no-verify -m 'chore: start kiln'"

// Example starts kiln in a new repository, and queues a spec the developer approved.
func Example() {
	ctx := context.Background()
	repo := must(filepath.EvalSymlinks(must(os.MkdirTemp("", "kiln-example"))))
	defer os.RemoveAll(repo)
	sh(repo, "git init --quiet -b main && git config user.name dev && git config user.email dev@example.com && "+
		"echo 'module example.com/x' >go.mod")

	r := must(kiln.Open(ctx, repo, os.Environ()))
	var out strings.Builder
	check(r.Init(ctx, &out, kiln.InitOptions{}))
	fmt.Print(strings.ReplaceAll(out.String(), repo, "$REPO"))

	sh(repo, approve)
	fmt.Println("kiln queue takes", must(r.Takes(ctx, "queue")))
	check(r.Queue(ctx, "x", io.Discard)) // It prints main's tip, which differs from run to run.
	check(r.Status(ctx, os.Stdout))
	// Output:
	// picked the preset go, since the repository holds go.mod
	// wrote .kiln/agents/audit.md
	// wrote .kiln/agents/build.md
	// wrote .kiln/agents/verify.md
	// wrote .kiln/kiln.json
	// wrote .kiln/kiln.schema.json
	// wrote .kiln/monitor.schema.json
	// wrote .kiln/reviewers/comprehension.md
	// wrote .kiln/reviewers/critic.md
	// wrote .kiln/reviewers/zoom-out.md
	// wrote .kiln/skills/shape/SKILL.md
	// wrote .kiln/skills/shape/template.md
	// wrote .kiln/init.json
	// linked .claude/skills/shape to ../../.kiln/skills/shape
	// linked .claude/agents/comprehension.md to ../../.kiln/reviewers/comprehension.md
	// linked .claude/agents/critic.md to ../../.kiln/reviewers/critic.md
	// linked .claude/agents/zoom-out.md to ../../.kiln/reviewers/zoom-out.md
	// commit .kiln/ on main: kiln reads it there
	// kiln builds on the branch main, which this repository lacks: commit to main, or run git branch -m main
	// the pre-commit hook runs the gate on each commit to main, so make its commands pass before you commit .kiln/:
	//   go vet ./...
	//   go test ./...
	// installed $REPO/.git/hooks/pre-commit
	// kiln queue takes [x]
	// x: queued
}

func sh(dir, script string) {
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("%s: %v\n%s", script, err, out))
	}
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
