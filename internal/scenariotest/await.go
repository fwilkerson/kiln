package scenariotest

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"syscall"
	"time"
)

// testPIDEnv names the variable that holds the test process's ID. await and the scripted claude end once it ends.
const testPIDEnv = "SCENARIOTEST_PID"

// awaitPoll is how often await looks at its file again.
const awaitPoll = 20 * time.Millisecond

// awaitMain runs await, the program a script waits for an event with:
//
//	await <file> [<regexp>]
//
// It waits until file exists, or with regexp, until a line of it matches. Its only limit is the test's own life.
func awaitMain() int {
	args := os.Args[1:]
	if len(args) == 0 || len(args) > 2 {
		fmt.Fprintln(os.Stderr, "usage: await <file> [<regexp>]")
		return 2
	}

	holds, err := awaited(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "await:", err)
		return 2
	}

	ended := testEnded()
	for !holds() {
		select {
		case <-ended:
			fmt.Fprintf(os.Stderr, "await: the test that ran it ended before %q\n", args)
			return 1
		case <-time.After(awaitPoll):
		}
	}

	return 0
}

// awaited answers the check of what args await: a file, and with a regexp, a line of it that matches.
func awaited(args []string) (func() bool, error) {
	file := args[0]
	if len(args) == 1 {
		return func() bool { _, err := os.Stat(file); return err == nil }, nil
	}

	line, err := regexp.Compile("(?m)" + args[1])
	if err != nil {
		return nil, err
	}

	return func() bool {
		text, err := os.ReadFile(file)
		return err == nil && line.Match(text)
	}, nil
}

// testEnded closes once the test process that testPIDEnv names has exited. A program that a test left behind then
// ends, rather than outlive the run. It closes at once when the variable names no process.
func testEnded() <-chan struct{} {
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		if pid, err := strconv.Atoi(os.Getenv(testPIDEnv)); err == nil {
			waitExit(pid)
		}
	}()

	return ended
}

// waitExit returns once the process pid has exited. Where kqueue can't watch the process, waitExit asks every
// exitPoll. Only ESRCH says the process has gone: EPERM names a live process that this one may not signal.
func waitExit(pid int) {
	if watchExit(pid) == nil {
		return
	}

	for !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		time.Sleep(exitPoll)
	}
}

// exitPoll is how often waitExit asks whether a process still runs, where kqueue can't watch it.
const exitPoll = time.Second

// watchExit waits on kqueue until the process pid exits. It answers nil once the process has exited, or when no
// process has that ID. It answers an error when kqueue can't watch the process.
func watchExit(pid int) error {
	kq, err := syscall.Kqueue()
	if err != nil {
		return err
	}

	defer syscall.Close(kq)
	var watch syscall.Kevent_t
	syscall.SetKevent(&watch, pid, syscall.EVFILT_PROC, syscall.EV_ADD|syscall.EV_ONESHOT)
	watch.Fflags = syscall.NOTE_EXIT
	events := make([]syscall.Kevent_t, 1)
	for {
		n, err := syscall.Kevent(kq, []syscall.Kevent_t{watch}, events, nil)
		switch {
		case errors.Is(err, syscall.EINTR):
			continue
		case err != nil:
			return err
		case n == 1 && events[0].Flags&syscall.EV_ERROR != 0:
			// kevent reports a failed watch as an event, when its list has room for one. ESRCH means the process
			// has ended.
			if errno := syscall.Errno(events[0].Data); errno != syscall.ESRCH {
				return errno
			}
		}

		return nil
	}
}
