package scenariotest

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"github.com/rogpeppe/go-internal/txtar"
)

// exitsMain runs exits, the program a script expects a program to fail with:
//
//	exits <status> <program> [args...]
//
// It runs the program with exits's own input, output and variables, and exits 0 when the program ends with status.
func exitsMain() int {
	const usage = "usage: exits <status> <program> [args...], with a status from 1 to 255"
	args := os.Args[1:]
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}

	// A bit size of 8 limits the status to 255, as a process's own is.
	want, err := strconv.ParseUint(args[0], 10, 8)
	if err != nil || want == 0 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}

	cmd := exec.Command(args[1], args[2:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return exits(int(want), cmd, os.Stderr)
}

// exits runs cmd, and answers 0 when cmd ends with the status want. Otherwise it writes what cmd ended with to
// stderr, and answers 1. A signal that ends cmd gives it the status sh reports: 128 plus the signal's number. exits
// passes SIGINT and SIGTERM on to cmd, since a script's kill signals exits itself.
func exits(want int, cmd *exec.Cmd, stderr io.Writer) int {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(stderr, "exits:", err)
		return 1
	}

	go func() {
		for s := range signals {
			_ = cmd.Process.Signal(s)
		}
	}()

	_ = cmd.Wait()
	got := cmd.ProcessState.ExitCode()
	if status := cmd.ProcessState.Sys().(syscall.WaitStatus); status.Signaled() {
		got = signaledBase + int(status.Signal())
	}

	if got != want {
		fmt.Fprintf(stderr, "exits: %q exited %d, not %d\n", cmd.Args, got, want)
		return 1
	}

	return 0
}

// signaledBase is what sh adds to the number of the signal that ends a program, for its status.
const signaledBase = 128

// unstated matches a script's line that lets kiln fail with any status.
var unstated = regexp.MustCompile(`^\s*[!?]\s*exec\s(.*[\s'"();&|])?kiln(\s|$)`)

// refuseUnstated fails t for each line of the scripts that runs kiln under ! exec or ? exec. Either accepts any
// status, so a usage error that exits 1 would pass where kiln's docs promise 2.
func refuseUnstated(t testing.TB, scripts testscript.Params) {
	files := scripts.Files
	for _, ext := range []string{"*.txtar", "*.txt"} {
		if scripts.Dir == "" {
			break
		}

		matches, err := filepath.Glob(filepath.Join(scripts.Dir, ext))
		if err != nil {
			t.Fatal("scenariotest:", err)
		}

		files = append(files, matches...)
	}

	for _, file := range files {
		ar, err := txtar.ParseFile(file)
		if err != nil {
			t.Fatal("scenariotest:", err)
		}

		for i, line := range strings.Split(string(ar.Comment), "\n") {
			if unstated.MatchString(line) {
				t.Errorf("scenariotest: %s:%d: %q accepts any status kiln fails with: "+
					"write exec exits <status> <program> [args...] instead", file, i+1, strings.TrimSpace(line))
			}
		}
	}
}
