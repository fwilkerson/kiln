// Command kiln builds the specs a developer approved, with headless Claude Code sessions, and lands them on main.
//
//	kiln init            write a starting kiln.json, and install the pre-commit hook
//	kiln approve <name>  queue a build of specs/<name>.md, as main holds it
//	kiln run             build each queued spec, one at a time, until none is queued
//	kiln status          show where each spec stands
//	kiln stats [<name>]  show what each build cost, one line each, or each build of one spec in full
//	kiln tui [<name>]    show every build, live and past, in a full-screen view; or open it on one spec
//	kiln accept <name>   end UAT: land the spec if it waits on its branch, then retire it
//
// kiln tui draws only on a terminal. With its output elsewhere, it draws nothing, and refuses.
//
// kiln runs two commands of its own. A build's session runs kiln hook before each file it writes. The pre-commit hook
// runs kiln precommit.
//
// kiln exits 0 when the command did what it says, and 1 when it refused or failed. A usage error exits 2.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/fwilkerson/kiln/internal/kiln"
)

const usage = `usage:
  kiln init
  kiln approve <name>
  kiln run
  kiln status
  kiln stats [<name>]
  kiln tui [<name>]
  kiln accept <name>
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr *os.File) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	command, args := args[0], args[1:]
	var err error
	switch command {
	case "hook":
		err = hook(args, stdin, stdout)
	case "precommit":
		err = kiln.Precommit(ctx, ".", os.Environ(), stderr)
	case "init", "run", "status":
		err = onRepo(ctx, func(r *kiln.Repo) error {
			if len(args) != 0 {
				return errUsage
			}

			switch command {
			case "init":
				return r.Init(ctx, stdout)
			case "run":
				return r.Run(ctx, stdout)
			}

			return r.Status(ctx, stdout)
		})
	case "stats", "tui":
		err = onRepo(ctx, func(r *kiln.Repo) error {
			if len(args) > 1 {
				return errUsage
			}

			name := ""
			if len(args) == 1 {
				name = args[0]
			}

			if command == "tui" {
				return r.TUI(ctx, name, os.Stdin, stdout)
			}

			return r.Stats(ctx, name, stdout)
		})
	case "approve", "accept":
		err = onRepo(ctx, func(r *kiln.Repo) error {
			if len(args) != 1 {
				return errUsage
			}

			if command == "approve" {
				return r.Approve(ctx, args[0], stdout)
			}

			return r.Accept(ctx, args[0], stdout)
		})
	default:
		err = errUsage
	}

	switch {
	case err == errUsage:
		fmt.Fprint(stderr, usage)
		return 2
	case err != nil:
		fmt.Fprintf(stderr, "kiln %s: %v\n", command, err)
		return 1
	}

	return 0
}

var errUsage = errors.New("usage")

func onRepo(ctx context.Context, do func(*kiln.Repo) error) error {
	r, err := kiln.Open(ctx, ".", os.Environ())
	if err != nil {
		return err
	}

	return do(r)
}

// hook answers Claude Code's PreToolUse call. A hook that cannot read its call exits 2, which Claude Code reads as a
// refusal.
func hook(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("hook", flag.ContinueOnError)
	spec := flags.String("spec", "", "the spec the session builds")
	place := flags.String("place", "", "the build's worktree")
	if err := flags.Parse(args); err != nil || *spec == "" || *place == "" {
		return errUsage
	}

	if err := kiln.Hook(stdin, stdout, *spec, *place); err != nil {
		fmt.Fprintf(os.Stderr, "kiln hook: %v\n", err)
		os.Exit(2)
	}

	return nil
}
