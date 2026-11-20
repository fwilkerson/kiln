// Command kiln builds the specs a developer approved, with headless Claude Code sessions, and lands them on main.
//
//	kiln init [--preset go|python]
//	                         write a preset of kiln's defaults into .kiln/, Go's by default, and install the
//	                         pre-commit hook
//	kiln queue <name>        queue a build of .kiln/specs/<name>.md, as main holds it
//	kiln fire                build each queued spec, one at a time, until none is queued
//	kiln status              show where each spec stands, and each gauge's latest reading
//	kiln tui [<name>]        show every build, live and past, in a full-screen view; or open it on one spec
//	kiln accept <name>       take the work: retire the spec, or land its amendments first; or land a waiting fix
//	kiln reject <name>       turn the amendments away: drop the build, and its branch; or drop a waiting fix
//	kiln monitor install     set up a launchd job that runs kiln monitor run in the repository each hour
//	kiln monitor run [<name>]
//	                         run each monitor kiln.json names that is due; or run one now, due or not
//	kiln monitor show <name> show how a monitor's last run ended, in the window kiln opens for it
//	kiln ledger [--kind <kind>] [--name <name>] [--last <n>]
//	                         print what kiln's ledger holds, one JSON object a line, for a script to read
//	kiln help                print this usage, as kiln --help and kiln -h do
//
// Each refuses a spec in a state it does not take, as go doc ./internal/kiln shows.
//
// A build that ends stopped, waiting for UAT, or waiting on its branch prints a terminal notification. The kiln fire,
// kiln accept or kiln monitor run that built it prints it, as OSC 9. Then it sums the build up, and says what to run
// next.
//
// kiln tui draws only on a terminal. With its output elsewhere, it draws nothing, and refuses.
//
// kiln ledger prints each kind of line, and each field, that go doc ./internal/kiln lists under kiln ledger.
//
// kiln runs three commands of its own, which the developer never types:
//
//	kiln hook --spec <name> --place <worktree> ...   a build's session runs it before each file it writes
//	kiln precommit                                    the pre-commit hook runs it
//	kiln complete [<command>]                         _kiln, the zsh completion, runs it
//
// kiln complete lists kiln's commands, one per line. With a command, it lists the names that command would not refuse.
// For accept and reject, those include the waiting fixes. For monitor run and monitor show, they are the monitors.
//
// kiln exits 0 when the command did what it says, and 1 when it refused or failed. kiln help prints the usage to
// stdout. A usage error exits 2, and prints the usage to stderr under a line that names the mistake. A kiln hook
// that cannot read its call exits 2 too, which Claude Code reads as a refusal. SIGINT and SIGTERM cancel the command.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/fwilkerson/kiln/internal/kiln"
)

const usage = `usage:
  kiln init [--preset go|python]
                           write a preset of kiln's defaults into .kiln/, Go's by default, and install the
                           pre-commit hook
  kiln queue <name>        queue a build of .kiln/specs/<name>.md, as main holds it
  kiln fire                build each queued spec, one at a time, until none is queued
  kiln status              show where each spec stands, and each gauge's latest reading
  kiln tui [<name>]        show every build, live and past, in a full-screen view; or open it on one spec
  kiln accept <name>       take the work: retire the spec, or land its amendments first; or land a waiting fix
  kiln reject <name>       turn the amendments away: drop the build, and its branch; or drop a waiting fix
  kiln monitor install     set up a launchd job that runs kiln monitor run in the repository each hour
  kiln monitor run [<name>]
                           run each monitor kiln.json names that is due; or run one now, due or not
  kiln monitor show <name> show how a monitor's last run ended, in the window kiln opens for it
  kiln ledger [--kind <kind>] [--name <name>] [--last <n>]
                           print what kiln's ledger holds, one JSON object a line, for a script to read
  kiln help                print this usage, as kiln --help and kiln -h do
`

// commands are the developer's commands, as kiln complete lists them.
var commands = []string{"init", "queue", "fire", "status", "tui", "accept", "reject", "monitor", "ledger"}

// onRepo holds each of the developer's commands, which act on the repository in the working directory.
var onRepo = map[string]repoCommand{
	"init":    initRepo,
	"fire":    noArgs((*kiln.Repo).Fire),
	"status":  noArgs((*kiln.Repo).Status),
	"tui":     tui,
	"queue":   oneName((*kiln.Repo).Queue),
	"accept":  oneName((*kiln.Repo).Accept),
	"reject":  oneName((*kiln.Repo).Reject),
	"monitor": monitor,
	"ledger":  ledger,
}

// monitor is kiln monitor install, kiln monitor run [<name>], or kiln monitor show <name>.
func monitor(ctx context.Context, r *kiln.Repo, args []string, out *os.File) error {
	usage := strings.Join(args[:min(1, len(args))], "") + "/" + fmt.Sprint(len(args))
	do, ok := monitorCommands[usage]
	if !ok {
		return usageError("takes install, run [<name>] or show <name>")
	}

	name := kiln.Spec(strings.Join(args[min(1, len(args)):], ""))
	return do(ctx, r, name, out)
}

// monitorRun is kiln monitor run, for every monitor that is due, or for the one named.
func monitorRun(ctx context.Context, r *kiln.Repo, name kiln.Spec, out *os.File) error {
	return r.MonitorRun(ctx, name, out)
}

// monitorCommands holds kiln monitor's commands, by their name and their count of arguments, the name included.
var monitorCommands = map[string]func(context.Context, *kiln.Repo, kiln.Spec, *os.File) error{
	"install/1": func(ctx context.Context, r *kiln.Repo, _ kiln.Spec, out *os.File) error {
		return r.MonitorInstall(ctx, out)
	},
	"run/1": monitorRun,
	"run/2": monitorRun,
	"show/2": func(ctx context.Context, r *kiln.Repo, name kiln.Spec, out *os.File) error {
		return r.MonitorShow(ctx, name, os.Stdin, out)
	},
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
		return exit("kiln", usageError("names no command"), stderr)
	}

	command, args := args[0], args[1:]
	var err error
	switch command {
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
		return 0
	case "hook":
		err = hook(args, stdin, stdout)
	case "precommit":
		err = kiln.Precommit(ctx, ".", os.Environ(), stderr)
	case "complete":
		err = complete(ctx, args, stdout)
	default:
		do, ok := onRepo[command]
		if !ok {
			return exit("kiln", usageError("unknown command "+command), stderr)
		}

		err = repo(ctx, do, args, stdout)
	}

	return exit("kiln "+command, err, stderr)
}

// exit reports err on stderr, after who, and returns the status kiln exits with.
func exit(who string, err error, stderr io.Writer) int {
	switch {
	case errors.Is(err, errUsage):
		fmt.Fprintf(stderr, "%s: %v\n", who, err)
		fmt.Fprint(stderr, usage)
		return 2
	case err != nil:
		fmt.Fprintf(stderr, "%s: %v\n", who, err)
		return 1
	}

	return 0
}

// errUsage is a usage error. kiln's own refusal of an argument has the same kind.
var errUsage = kiln.ErrUsage

// usageError is a usage error whose text names the mistake, such as "takes one spec's name".
type usageError string

func (e usageError) Error() string { return string(e) }

// Is makes a usageError match errUsage.
func (usageError) Is(target error) bool { return target == errUsage }

// repo opens the repository in the working directory and runs do on it.
func repo(ctx context.Context, do repoCommand, args []string, out *os.File) error {
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
			return usageError("takes no arguments")
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
		return usageError("takes only --preset <name>")
	}

	return r.Init(ctx, out, kiln.InitOptions{Preset: *preset})
}

// ledger is kiln ledger, which takes --kind, --name and --last, and no other argument. It refuses a --last of 0,
// which would otherwise print every line.
func ledger(_ context.Context, r *kiln.Repo, args []string, out *os.File) error {
	flags := flag.NewFlagSet("ledger", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	kind := flags.String("kind", "", "the kind of line to print")
	name := flags.String("name", "", "the spec or monitor whose lines to print")
	last := flags.Uint("last", 0, "how many of the last lines to print")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return usageError("takes only --kind <kind>, --name <name> and --last <n>")
	}

	if flagSet(flags, "last") && *last == 0 {
		return usageError("takes a --last of 1 or more")
	}

	return r.Ledger(out, kiln.LedgerOptions{Kind: *kind, Name: kiln.Spec(*name), Last: int(*last)})
}

// flagSet reports whether the command line named the flag name.
func flagSet(flags *flag.FlagSet, name string) bool {
	set := false
	flags.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}

// oneName is a command that takes one spec's name.
func oneName(do func(*kiln.Repo, context.Context, kiln.Spec, io.Writer) error) repoCommand {
	return func(ctx context.Context, r *kiln.Repo, args []string, out *os.File) error {
		if len(args) != 1 {
			return usageError("takes one spec's name")
		}

		return do(r, ctx, kiln.Spec(args[0]), out)
	}
}

// tui is kiln tui, which takes a spec's name or none.
func tui(ctx context.Context, r *kiln.Repo, args []string, out *os.File) error {
	if len(args) > 1 {
		return usageError("takes one spec's name, or none")
	}

	var name kiln.Spec
	if len(args) == 1 {
		name = kiln.Spec(args[0])
	}

	return r.TUI(ctx, name, os.Stdin, out)
}

// complete lists kiln's commands, or with a command, the names it takes. kiln monitor takes its own commands, and
// kiln monitor run and show take the monitors' names.
func complete(ctx context.Context, args []string, out io.Writer) error {
	if len(args) > 2 {
		return usageError("takes a command, and its subcommand")
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
	switch {
	case len(args) == 0:
		return commands, nil
	case len(args) == 1 && args[0] == "monitor":
		return []string{"install", "run", "show"}, nil
	}

	r, err := kiln.Open(ctx, ".", os.Environ())
	if err != nil {
		return nil, err
	}

	return r.Takes(ctx, strings.Join(args, " "))
}

// hook answers Claude Code's PreToolUse call, for the session its arguments name. A hook that cannot read its call
// exits 2, which Claude Code reads as a refusal.
func hook(args []string, stdin io.Reader, stdout io.Writer) error {
	err := kiln.Hook(args, stdin, stdout)
	if err != nil && !errors.Is(err, errUsage) {
		fmt.Fprintf(os.Stderr, "kiln hook: %v\n", err)
		os.Exit(2)
	}

	return err
}
