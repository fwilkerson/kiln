// This command rebuilds kiln from the tip of main, and installs kiln with the zsh completion for kiln. Run it in the
// repository of kiln:
//
//	go run ./tools/install
//
// It builds the tip of main, never the working tree, in a worktree of its own that it removes after. So neither an
// edit in progress nor a build's worktree reaches the binary. go install puts kiln in GOBIN, or else in the first
// GOPATH's bin. It copies cmd/kiln/_kiln, from the same commit, into $XDG_DATA_HOME/zsh/completions. XDG_DATA_HOME
// is ~/.local/share when unset.
//
// It refuses while any kiln on the machine drives a build session, in this repository or another. It holds every kiln
// off from starting one until the new binary is in place.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/kiln"
)

func main() {
	if err := install(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "install:", err)
		os.Exit(1)
	}
}

func install(ctx context.Context) error {
	release, err := kiln.HoldBuilds()
	if err != nil {
		return err
	}

	defer release()
	top, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return fmt.Errorf("run it in kiln's repository: %w", err)
	}

	g, err := git.New(strings.TrimSpace(string(top)), os.Environ())
	if err != nil {
		return err
	}

	tip, err := g.Tip(ctx, "main")
	if err != nil {
		return err
	}

	tmp, err := os.MkdirTemp("", "kiln-install-")
	if err != nil {
		return err
	}

	defer os.RemoveAll(tmp)
	tree := filepath.Join(tmp, "kiln")
	if err := g.Detached(ctx, git.Worktree(tree), tip); err != nil {
		return err
	}

	err = build(ctx, tree, tip)
	return errors.Join(err, g.Remove(ctx, []git.Worktree{git.Worktree(tree)}))
}

// build installs kiln and its completion from the checkout at tree, of the commit tip.
func build(ctx context.Context, tree string, tip git.Rev) error {
	cmd := exec.CommandContext(ctx, "go", "install", "./cmd/kiln")
	cmd.Dir, cmd.Stdout, cmd.Stderr = tree, os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go install ./cmd/kiln: %w", err)
	}

	completion, err := os.ReadFile(filepath.Join(tree, "cmd", "kiln", "_kiln"))
	if err != nil {
		return err
	}

	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}

		data = filepath.Join(home, ".local", "share")
	}

	dir := filepath.Join(data, "zsh", "completions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(dir, "_kiln"), completion, 0o644); err != nil {
		return err
	}

	fmt.Printf("installed kiln, and %s, from main at %.12s\n", filepath.Join(dir, "_kiln"), tip)
	return nil
}
