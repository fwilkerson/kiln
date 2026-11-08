package proc_test

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"

	"github.com/fwilkerson/kiln/internal/proc"
)

func Example() {
	env := []string{"PATH=/usr/bin:/bin", "HOME=/home/dev", "GIT_DIR=/elsewhere", "ANTHROPIC_API_KEY=key"}
	fmt.Println(proc.Session.Env(env))
	fmt.Println(proc.Shell.Env(env))

	path, err := proc.Look(env, "sleep")
	if err != nil {
		fmt.Println(err)
		return
	}

	cmd := exec.CommandContext(context.Background(), path, "60")
	cmd.Env = proc.Shell.Env(env)
	proc.OwnGroup(cmd)
	if err := cmd.Start(); err != nil {
		fmt.Println(err)
		return
	}

	p, err := proc.Of(cmd.Process.Pid)
	fmt.Println("leads a group of its own:", err == nil && p.Group == p.PID)
	fmt.Println(syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL), cmd.Wait())

	_, err = proc.Look(env, "no-such-program")
	fmt.Println(err)

	stderr := proc.NewTail(7)
	_, _ = fmt.Fprint(stderr, "a long line, and its end")
	fmt.Printf("%q\n", stderr)
	// Output:
	// [PATH=/usr/bin:/bin HOME=/home/dev]
	// [PATH=/usr/bin:/bin HOME=/home/dev ANTHROPIC_API_KEY=key]
	// leads a group of its own: true
	// <nil> signal: killed
	// no-such-program is not on the PATH: /usr/bin:/bin
	// "its end"
}
