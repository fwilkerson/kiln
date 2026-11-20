package proc_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/fwilkerson/kiln/internal/proc"
)

// TestRunKeepsQuietChild holds that Run kills a successful command's group only when a child held its output. A
// helper that closed its output, such as a build daemon, keeps running for the next command. The helper shows it
// lives by answering a signal, so no pause decides whether it was killed.
func TestRunKeepsQuietChild(t *testing.T) {
	dir := t.TempDir()
	helper := `trap "touch alive; exit" USR1; touch ready; while :; do sleep 1; done`
	cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", "sh -c '"+helper+"' >/dev/null 2>&1 & echo $! >pid")
	cmd.Dir = dir
	if err := proc.Run(cmd); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "pid"))
	var pid int
	if _, scanErr := fmt.Sscan(string(raw), &pid); err != nil || scanErr != nil {
		t.Fatalf("the child's pid: %v, %v", err, scanErr)
	}

	defer syscall.Kill(pid, syscall.SIGKILL)
	for _, step := range []string{"ready", "alive"} {
		if !answers(pid, filepath.Join(dir, step)) {
			t.Fatalf("the quiet child, process %d, was killed before it was %s", pid, step)
		}

		_ = syscall.Kill(pid, syscall.SIGUSR1)
	}
}

// answers waits until the process pid writes file, and reports whether it did before it exited.
func answers(pid int, file string) bool {
	for {
		if _, err := os.Stat(file); err == nil {
			return true
		}

		if p, err := proc.Of(pid); err != nil || p.Zombie {
			return false
		}

		time.Sleep(10 * time.Millisecond)
	}
}
