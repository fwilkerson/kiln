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
)

// askEnv names the directory in which a test answers each step, as the package's doc says.
const askEnv = "KILN_CRASH_ASK"

// Step marks a step at which kiln changes durable state, what. With $KILN_CRASH_ASK set, it appends the step to the
// log there, and waits for the test's answer to it. It holds the log's lock meanwhile, so the kilns that share the
// log take one step at a time. A log Step cannot write panics.
func Step(what string) {
	dir := os.Getenv(askEnv)
	if dir == "" {
		return
	}

	f, err := os.OpenFile(filepath.Join(dir, "steps"), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		panic(fmt.Sprintf("crash: %s: %v", dir, err))
	}

	defer f.Close()
	n, err := count(f, what)
	if err != nil {
		panic(fmt.Sprintf("crash: %s: %v", dir, err))
	}

	switch answer(filepath.Join(dir, strconv.Itoa(n))) {
	case "KILL":
		syscall.Kill(os.Getpid(), syscall.SIGKILL)
		select {}
	case "INT":
		syscall.Kill(os.Getpid(), syscall.SIGINT)
		// kiln's handler cancels its context on another goroutine. The wait lets it, as an earlier Ctrl-C would.
		time.Sleep(50 * time.Millisecond)
	}
}

// count locks the log f, appends what to it as the next step, and answers that step's number. The lock orders the
// steps of every kiln that shares the log, and lasts until f closes.
func count(f *os.File, what string) (int, error) {
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

// answerWait is how long a step waits for the test's answer. A test answers within a few seconds, so a kiln that
// waits longer has lost its test.
const answerWait = time.Minute

// answer waits for the file at path, and answers the line it holds. No answer comes once the test has removed the
// file's directory, or has died. So kiln exits then, or after answerWait.
func answer(path string) string {
	for asked := time.Now(); time.Since(asked) < answerWait; time.Sleep(200 * time.Microsecond) {
		raw, err := os.ReadFile(path)
		if err == nil && bytes.HasSuffix(raw, []byte("\n")) {
			return strings.TrimSpace(string(raw))
		}

		if _, err := os.Stat(filepath.Dir(path)); err != nil {
			break
		}
	}

	fmt.Fprintf(os.Stderr, "crash: no test answered the step at %s\n", path)
	os.Exit(1)
	return ""
}
