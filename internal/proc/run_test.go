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
// helper that closed its output, such as a build daemon, keeps running for the next command.
func TestRunKeepsQuietChild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", "sleep 30 >/dev/null 2>&1 & echo $! > "+pidFile)
	if err := proc.Run(cmd); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(pidFile)
	var pid int
	if _, scanErr := fmt.Sscan(string(raw), &pid); err != nil || scanErr != nil {
		t.Fatalf("the child's pid: %v, %v", err, scanErr)
	}

	defer syscall.Kill(pid, syscall.SIGKILL)
	time.Sleep(200 * time.Millisecond)
	if err := syscall.Kill(pid, 0); err != nil {
		t.Errorf("the quiet child, process %d, was killed: %v", pid, err)
	}
}
