// Command kiln builds the specs a developer approved, with headless Claude Code sessions, and lands them on main.
//
//	kiln init [--preset go|python|typescript|<dir>]
//	                         write a preset of kiln's defaults into .kiln/, or merge a newer kiln's into yours,
//	                         from the preset .kiln/init.json records, or else the one your files match;
//	                         and install the pre-commit hook
//	kiln queue <name>        queue a build of .kiln/specs/<name>.md, as main holds it
//	kiln fire                build each queued spec, one at a time, until none is queued
//	kiln status              show the specs, the monitors, each gauge's reading, and whether .kiln/ is behind
//	kiln tui [<name>]        show every build, live and past, in a full-screen view; or open it on one spec
//	kiln accept <name>       take the work: retire the spec, or land its amendments first; or land a waiting fix
//	kiln reject <name>       turn the amendments away: drop the build, and its branch; or drop a waiting fix
//	kiln monitor add [<name>|<dir>]
//	                         copy a monitor into .kiln/monitors/: one kiln ships, such as bugs or go/docs, or the
//	                         one in a directory, by a path that starts with . or /; with neither, list those kiln
//	                         ships, and the preset each fits
//	kiln monitor install     set up a launchd job that runs kiln monitor run in the repository each hour
//	kiln monitor run [<name>]
//	                         run each monitor that is due; or run one now, due or not
//	kiln monitor show <name> show how a monitor's last run ended, then wait for return
//	kiln doctor [<name>]     print each problem in the project's setup, with its fix; or a spec's form too
//	kiln ledger [--kind <kind>] [--name <name>] [--last <n>]
//	                         print what kiln's ledger holds, one JSON object a line, for a script to read
//	kiln help [<topic>]      print this usage and the topics of kiln's docs, as kiln --help and kiln -h do; or the
//	                         part of kiln's docs a topic names, such as kiln help monitors
//
// Each refuses a spec in a state it does not take, as kiln help specs shows.
//
// A build that ends stopped, waiting for UAT, or waiting on its branch notifies the developer. kiln help
// notifications says how. Then the kiln that built it sums the build up, and says what to run next.
//
// kiln tui draws only on a terminal. With its output elsewhere, it draws nothing, and refuses.
//
// kiln ledger prints each kind of line, and each field, that kiln help kiln-ledger lists.
//
// kiln runs three commands of its own, which the developer never types:
//
//	kiln hook --spec <name> --place <worktree> ...   a build's session runs it before each file it writes
//	kiln precommit                                    the pre-commit hook runs it
//	kiln complete [<command>]                         _kiln, the zsh completion, runs it
//
// kiln complete lists kiln's commands, one per line. With a command, it lists the names that command would not refuse.
// For accept and reject, those include the waiting fixes. For monitor run and monitor show, they are every monitor,
// refused ones too, so monitor run can say why. For monitor add, they are the monitors kiln ships that fit the preset
// .kiln/init.json records. For help, they are the topics of kiln's docs.
//
// kiln exits 0 when the command did what it says, and 1 when it refused or failed. kiln help prints the usage to
// stdout. A usage error exits 2, and prints to stderr a line that names the mistake. Under it come the command's own
// lines of the usage, and a pointer to kiln help. A command kiln lacks gets the names of kiln's commands instead. kiln
// hook and kiln complete, which the usage does not list, print the first line alone. A kiln hook that cannot read its
// call exits 2 too, which Claude Code reads as a refusal. SIGINT and SIGTERM cancel the command.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"

	"github.com/fwilkerson/kiln/internal/kiln"
)

const usage = `usage:
  kiln init [--preset go|python|typescript|<dir>]
                           write a preset of kiln's defaults into .kiln/, or merge a newer kiln's into yours,
                           from the preset .kiln/init.json records, or else the one your files match;
                           and install the pre-commit hook
  kiln queue <name>        queue a build of .kiln/specs/<name>.md, as main holds it
  kiln fire                build each queued spec, one at a time, until none is queued
  kiln status              show the specs, the monitors, each gauge's reading, and whether .kiln/ is behind
  kiln tui [<name>]        show every build, live and past, in a full-screen view; or open it on one spec
  kiln accept <name>       take the work: retire the spec, or land its amendments first; or land a waiting fix
  kiln reject <name>       turn the amendments away: drop the build, and its branch; or drop a waiting fix
  kiln monitor add [<name>|<dir>]
                           copy a monitor into .kiln/monitors/: one kiln ships, such as bugs or go/docs, or the
                           one in a directory, by a path that starts with . or /; with neither, list those kiln
                           ships, and the preset each fits
  kiln monitor install     set up a launchd job that runs kiln monitor run in the repository each hour
  kiln monitor run [<name>]
                           run each monitor that is due; or run one now, due or not
  kiln monitor show <name> show how a monitor's last run ended, then wait for return
  kiln doctor [<name>]     print each problem in the project's setup, with its fix; or a spec's form too
  kiln ledger [--kind <kind>] [--name <name>] [--last <n>]
                           print what kiln's ledger holds, one JSON object a line, for a script to read
  kiln help [<topic>]      print this usage and the topics of kiln's docs, as kiln --help and kiln -h do; or the
                           part of kiln's docs a topic names, such as kiln help monitors
`

// commands are the developer's commands, in the order usage lists them, as kiln complete lists them.
var commands = func() []string {
	var names []string
	for line := range strings.Lines(usage) {
		if name := opens(line); name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}

	return names
}()

// opens is the command a line of usage opens, such as "queue" for "  kiln queue <name>". It is "" for the line
// "usage:", and for a line that continues the one above it.
func opens(line string) string {
	rest, ok := strings.CutPrefix(line, "  kiln ")
	if !ok {
		return ""
	}

	return strings.Fields(rest)[0]
}

// usageOf is the lines of usage that command opens, each with the lines that continue it. It is "" for a command
// that usage does not list, such as hook.
func usageOf(command string) string {
	var lines strings.Builder
	shown := false
	for line := range strings.Lines(usage) {
		if name := opens(line); name != "" {
			shown = name == command
		}

		if shown {
			lines.WriteString(line)
		}
	}

	return lines.String()
}

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
	"doctor":  doctor,
	"ledger":  ledger,
}

// monitor is kiln monitor add [<name>|<dir>], kiln monitor install, kiln monitor run [<name>], or kiln monitor show
// <name>.
func monitor(ctx context.Context, r *kiln.Repo, args []string, out *os.File) error {
	usage := strings.Join(args[:min(1, len(args))], "") + "/" + fmt.Sprint(len(args))
	do, ok := monitorCommands[usage]
	if !ok {
		return usageError("takes add [<name>|<dir>], install, run [<name>] or show <name>")
	}

	return do(ctx, r, strings.Join(args[min(1, len(args)):], ""), out)
}

// monitorRun is kiln monitor run, for every monitor that is due, or for the one named.
func monitorRun(ctx context.Context, r *kiln.Repo, name string, out *os.File) error {
	return r.MonitorRun(ctx, kiln.Spec(name), out)
}

// monitorCommands holds kiln monitor's commands, by their name and their count of arguments, the name included. Each
// takes the argument after its name, or "".
var monitorCommands = map[string]func(context.Context, *kiln.Repo, string, *os.File) error{
	"add/1": func(_ context.Context, r *kiln.Repo, _ string, out *os.File) error {
		return r.ShippedMonitors(out)
	},
	"add/2": func(_ context.Context, r *kiln.Repo, source string, out *os.File) error {
		return r.MonitorAdd(source, out)
	},
	"install/1": func(ctx context.Context, r *kiln.Repo, _ string, out *os.File) error {
		return r.MonitorInstall(ctx, out)
	},
	"run/1": monitorRun,
	"run/2": monitorRun,
	"show/2": func(ctx context.Context, r *kiln.Repo, name string, out *os.File) error {
		return r.MonitorShow(ctx, kiln.Spec(name), os.Stdin, out)
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
		return exit("kiln", "", usageError("names no command"), stderr)
	}

	command, args := args[0], args[1:]
	listed := command
	var err error
	switch command {
	case "help", "--help", "-h":
		listed = "help"
		err = help(args, stdout)
	case "hook":
		err = hook(args, stdin, stdout)
	case "precommit":
		err = kiln.Precommit(ctx, ".", os.Environ(), stderr)
	case "complete":
		err = complete(ctx, args, stdout)
	default:
		do, ok := onRepo[command]
		if !ok {
			return exit("kiln", "", usageError("unknown command "+command), stderr)
		}

		err = repo(ctx, do, args, stdout)
	}

	return exit("kiln "+command, listed, err, stderr)
}

// exit reports err on stderr, after who, and returns the status kiln exits with. A usage error prints its hint, for
// command, under err.
func exit(who, command string, err error, stderr io.Writer) int {
	switch {
	case errors.Is(err, errUsage):
		fmt.Fprintf(stderr, "%s: %v\n%s", who, err, hint(command))
		return 2
	case err != nil:
		fmt.Fprintf(stderr, "%s: %v\n", who, err)
		return 1
	}

	return 0
}

// hint is what a usage error of command prints under the line that names the mistake. It is the lines of usage
// that command opens, then a pointer to kiln help. With command "", it is the names of kiln's commands. It is "" for
// a command that usage does not list.
func hint(command string) string {
	lines := usageOf(command)
	switch {
	case command == "":
		return "commands: " + strings.Join(commands, " ") + "\nrun kiln help for what each does\n"
	case lines == "":
		return ""
	}

	return "usage:\n" + lines + "run kiln help for every command\n"
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

// initRepo is kiln init, which takes the preset's name or directory in --preset, and no other argument.
func initRepo(ctx context.Context, r *kiln.Repo, args []string, out *os.File) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	preset := flags.String("preset", "", "the preset to write: go, python, typescript, or a directory laid out as one")
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
	name, err := nameOrNone(args)
	if err != nil {
		return err
	}

	return r.TUI(ctx, name, os.Stdin, out)
}

// doctor is kiln doctor, which takes a spec's name or none.
func doctor(ctx context.Context, r *kiln.Repo, args []string, out *os.File) error {
	name, err := nameOrNone(args)
	if err != nil {
		return err
	}

	return r.Doctor(ctx, name, out)
}

// nameOrNone is the one spec's name args hold, or "" for none.
func nameOrNone(args []string) (kiln.Spec, error) {
	if len(args) > 1 {
		return "", usageError("takes one spec's name, or none")
	}

	return kiln.Spec(strings.Join(args, "")), nil
}

// complete lists kiln's commands, or with a command, the names it takes. kiln monitor takes its own commands. kiln
// monitor run and show take the monitors' names. kiln monitor add takes the names of those kiln ships that fit. kiln
// help takes the topics of kiln's docs.
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

// takes is kiln's commands, or with a command, the names it takes.
func takes(ctx context.Context, args []string) ([]string, error) {
	switch {
	case len(args) == 0:
		return commands, nil
	case len(args) == 1 && args[0] == "monitor":
		return []string{"add", "install", "run", "show"}, nil
	case len(args) == 1 && args[0] == "help":
		return kiln.Topics(), nil
	}

	r, err := kiln.Open(ctx, ".", os.Environ())
	if err != nil {
		return nil, err
	}

	return r.Takes(ctx, strings.Join(args, " "))
}

// help prints the usage, then the topics of kiln's docs. With a topic, it prints that part of the docs alone.
func help(args []string, out io.Writer) error {
	switch len(args) {
	case 0:
		fmt.Fprintf(out, "%s\ntopics, for kiln help <topic>:\n  %s\n", usage, strings.Join(kiln.Topics(), " "))
		return nil
	case 1:
		return kiln.Help(out, args[0])
	}

	return usageError("takes one topic, or none")
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
