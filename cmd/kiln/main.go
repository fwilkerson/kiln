// Command kiln builds the specs a developer approved, with headless Claude Code sessions, and lands them on main.
//
//	kiln init                write a starting kiln.json, and install the pre-commit hook
//	kiln queue <name>        queue a build of specs/<name>.md, as main holds it
//	kiln run                 build each queued spec, one at a time, until none is queued
//	kiln status              show where each spec stands
//	kiln tui [<name>]        show every build, live and past, in a full-screen view; or open it on one spec
//	kiln accept <name>       take the work: retire the spec, or land its amendments first
//	kiln reject <name>       turn the amendments away: drop the build, and its branch
//
// Each refuses a spec in a state it does not take, as go doc ./internal/kiln shows.
//
// A build that ends stopped, waiting for UAT, or waiting on its branch prints a terminal notification. kiln run or
// kiln accept prints it, as OSC 9. Then it sums the build up, and says what to run next.
//
// kiln tui draws only on a terminal. With its output elsewhere, it draws nothing, and refuses.
//
// kiln runs three commands of its own. A build's session runs kiln hook before each file it writes. The pre-commit
// hook runs kiln precommit. The zsh completion in _kiln runs kiln complete, which lists kiln's commands, one per line.
// kiln complete <command> lists the spec names that command would not refuse.
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
  kiln init                write a starting kiln.json, and install the pre-commit hook
  kiln queue <name>        queue a build of specs/<name>.md, as main holds it
  kiln run                 build each queued spec, one at a time, until none is queued
  kiln status              show where each spec stands
  kiln tui [<name>]        show every build, live and past, in a full-screen view; or open it on one spec
  kiln accept <name>       take the work: retire the spec, or land its amendments first
  kiln reject <name>       turn the amendments away: drop the build, and its branch
`

// commands are the developer's commands, as kiln complete lists them.
var commands = []string{"init", "queue", "run", "status", "tui", "accept", "reject"}

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
	case "tui":
		err = onRepo(ctx, func(r *kiln.Repo) error {
			if len(args) > 1 {
				return errUsage
			}

			name := ""
			if len(args) == 1 {
				name = args[0]
			}

			return r.TUI(ctx, name, os.Stdin, stdout)
		})
	case "queue", "accept", "reject":
		err = onRepo(ctx, func(r *kiln.Repo) error {
			if len(args) != 1 {
				return errUsage
			}

			switch command {
			case "queue":
				return r.Queue(ctx, args[0], stdout)
			case "accept":
				return r.Accept(ctx, args[0], stdout)
			}

			return r.Reject(ctx, args[0], stdout)
		})
	case "complete":
		err = complete(ctx, args, stdout)
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

// complete lists kiln's commands, or with a command, the spec names it takes.
func complete(ctx context.Context, args []string, out io.Writer) error {
	names := commands
	switch {
	case len(args) > 1:
		return errUsage
	case len(args) == 1:
		err := onRepo(ctx, func(r *kiln.Repo) (err error) {
			names, err = r.Takes(ctx, args[0])
			return err
		})
		if err != nil {
			return err
		}
	}

	for _, name := range names {
		fmt.Fprintln(out, name)
	}

	return nil
}

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
