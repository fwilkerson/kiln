//go:build crash

package crash

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// The variables a build with the tag crash reads.
const (
	logEnv       = "KILN_CRASH_LOG"
	atEnv        = "KILN_CRASH_AT"
	signalEnv    = "KILN_CRASH_SIGNAL"
	snapshotsEnv = "KILN_CRASH_SNAPSHOTS"
	rootEnv      = "KILN_CRASH_ROOT"
)

// Step marks a step at which kiln changes durable state, what. It appends the step to the log $KILN_CRASH_LOG
// names. At a step $KILN_CRASH_AT names, it kills kiln, or takes a snapshot when $KILN_CRASH_SNAPSHOTS is set. A
// log Step cannot write, or a snapshot it cannot take, panics.
func Step(what string) {
	log := os.Getenv(logEnv)
	if log == "" {
		return
	}

	n, err := count(log, what)
	if err != nil {
		panic(fmt.Sprintf("crash: %s: %v", log, err))
	}

	switch {
	case !named(os.Getenv(atEnv), n):
	case os.Getenv(snapshotsEnv) != "":
		snapshot := filepath.Join(os.Getenv(snapshotsEnv), strconv.Itoa(n))
		if err := unix.Clonefile(os.Getenv(rootEnv), snapshot, unix.CLONE_NOFOLLOW); err != nil {
			panic(fmt.Sprintf("crash: snapshot %s: %v", snapshot, err))
		}
	case os.Getenv(signalEnv) == "INT":
		syscall.Kill(os.Getpid(), syscall.SIGINT)
		// kiln's handler cancels its context on another goroutine. The wait lets it, as an earlier Ctrl-C would.
		time.Sleep(50 * time.Millisecond)
	default:
		syscall.Kill(os.Getpid(), syscall.SIGKILL)
		select {}
	}
}

// named is whether the list at names the step n. Its items are split by commas. An item is a step, or a step and
// an interval, such as 3+8 for 3, 11, 19 and on.
func named(at string, n int) bool {
	for item := range strings.SplitSeq(at, ",") {
		first, every, _ := strings.Cut(item, "+")
		from, err := strconv.Atoi(first)
		interval, _ := strconv.Atoi(every)
		switch {
		case err != nil, n < from:
		case n == from:
			return true
		case interval > 0 && (n-from)%interval == 0:
			return true
		}
	}

	return false
}

// count appends what to the log as the next step, and answers that step's number. The file's lock orders the steps
// of every kiln that shares the log.
func count(log, what string) (int, error) {
	f, err := os.OpenFile(log, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}

	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return 0, err
	}

	steps, err := io.ReadAll(f)
	if err != nil {
		return 0, err
	}

	n := bytes.Count(steps, []byte("\n")) + 1
	_, err = fmt.Fprintf(f, "%d %d %s\n", n, os.Getpid(), what)
	return n, err
}
