// Command kiln builds the specs a developer approved, with headless Claude Code sessions, and lands them on main.
//
//	kiln init [--preset go|python]
//	                         write a preset of kiln's defaults into .kiln/, Go's by default, and install the
//	                         pre-commit hook
//	kiln queue <name>        queue a build of .kiln/specs/<name>.md, as main holds it
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

	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/kiln"
)

const usage = `usage:
  kiln init [--preset go|python]
                           write a preset of kiln's defaults into .kiln/, Go's by default, and install the
                           pre-commit hook
  kiln queue <name>        queue a build of .kiln/specs/<name>.md, as main holds it
  kiln run                 build each queued spec, one at a time, until none is queued
  kiln status              show where each spec stands
  kiln tui [<name>]        show every build, live and past, in a full-screen view; or open it on one spec
  kiln accept <name>       take the work: retire the spec, or land its amendments first
  kiln reject <name>       turn the amendments away: drop the build, and its branch
`

// commands are the developer's commands, as kiln complete lists them.
var commands = []string{"init", "queue", "run", "status", "tui", "accept", "reject"}

// onRepo holds each of the developer's commands, which act on the repository in the working directory.
var onRepo = map[string]repoCommand{
	"init":   initRepo,
	"run":    noArgs((*kiln.Repo).Run),
	"status": noArgs((*kiln.Repo).Status),
	"tui":    tui,
	"queue":  oneName((*kiln.Repo).Queue),
	"accept": oneName((*kiln.Repo).Accept),
	"reject": oneName((*kiln.Repo).Reject),
}

// repoCommand runs one command on r, with the arguments that follow the command's name.
type repoCommand func(ctx context.Context, r *kiln.Repo, args []string, out *os.File) error

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
	case "complete":
		err = complete(ctx, args, stdout)
	default:
		err = repo(ctx, command, args, stdout)
	}

	return exit(command, err, stderr)
}

// exit reports err on stderr and returns the status kiln exits with.
func exit(command string, err error, stderr io.Writer) int {
	switch {
	case errors.Is(err, errUsage):
		if err != errUsage {
			fmt.Fprintf(stderr, "kiln %s: %v\n", command, err)
		}

		fmt.Fprint(stderr, usage)
		return 2
	case err != nil:
		fmt.Fprintf(stderr, "kiln %s: %v\n", command, err)
		return 1
	}

	return 0
}

// errUsage is a usage error. kiln's own refusal of an argument has the same kind.
var errUsage = kiln.ErrUsage

// repo opens the repository in the working directory and runs command on it.
func repo(ctx context.Context, command string, args []string, out *os.File) error {
	do, ok := onRepo[command]
	if !ok {
		return errUsage
	}

	r, err := kiln.Open(ctx, ".", os.Environ())
	if err != nil {
		return err
	}

	return do(ctx, r, args, out)
}

// noArgs is a command that takes no arguments.
func noArgs(do func(*kiln.Repo, context.Context, io.Writer) error) repoCommand {
	return func(ctx context.Context, r *kiln.Repo, args []string, out *os.File) error {
		if len(args) != 0 {
			return errUsage
		}

		return do(r, ctx, out)
	}
}

// initRepo is kiln init, which takes the preset's name in --preset, and no other argument.
func initRepo(ctx context.Context, r *kiln.Repo, args []string, out *os.File) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	preset := flags.String("preset", "", "the preset to write: go or python")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errUsage
	}

	return r.Init(ctx, out, kiln.InitOptions{Preset: *preset})
}

// oneName is a command that takes one spec's name.
func oneName(do func(*kiln.Repo, context.Context, kiln.Spec, io.Writer) error) repoCommand {
	return func(ctx context.Context, r *kiln.Repo, args []string, out *os.File) error {
		if len(args) != 1 {
			return errUsage
		}

		return do(r, ctx, kiln.Spec(args[0]), out)
	}
}

// tui is kiln tui, which takes a spec's name or none.
func tui(ctx context.Context, r *kiln.Repo, args []string, out *os.File) error {
	if len(args) > 1 {
		return errUsage
	}

	var name kiln.Spec
	if len(args) == 1 {
		name = kiln.Spec(args[0])
	}

	return r.TUI(ctx, name, os.Stdin, out)
}

// complete lists kiln's commands, or with a command, the spec names it takes.
func complete(ctx context.Context, args []string, out io.Writer) error {
	if len(args) > 1 {
		return errUsage
	}

	names, err := takes(ctx, args)
	if err != nil {
		return err
	}

	for _, name := range names {
		fmt.Fprintln(out, name)
	}

	return nil
}

// takes is kiln's commands, or with a command, the spec names it takes.
func takes(ctx context.Context, args []string) ([]string, error) {
	if len(args) == 0 {
		return commands, nil
	}

	r, err := kiln.Open(ctx, ".", os.Environ())
	if err != nil {
		return nil, err
	}

	return r.Takes(ctx, args[0])
}

// hook answers Claude Code's PreToolUse call. A hook that cannot read its call exits 2, which Claude Code reads as a
// refusal.
func hook(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("hook", flag.ContinueOnError)
	spec := flags.String("spec", "", "the spec the session builds")
	place := flags.String("place", "", "the build's worktree")
	switch err := flags.Parse(args); {
	case err != nil, *spec == "", *place == "":
		return errUsage
	}

	if err := kiln.Hook(stdin, stdout, kiln.Spec(*spec), git.Worktree(*place)); err != nil {
		fmt.Fprintf(os.Stderr, "kiln hook: %v\n", err)
		os.Exit(2)
	}

	return nil
}
