package proc_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/fwilkerson/kiln/internal/proc"
)

func Example() {
	env := []string{"PATH=/usr/bin:/bin", "HOME=/home/dev", "GIT_DIR=/elsewhere", "ANTHROPIC_API_KEY=key"}
	fmt.Println(proc.Session.Env(env))
	fmt.Println(proc.Shell.Env(env))

	shell := proc.Command{Path: "sh", Args: []string{"-c", "echo $HOME; exit 3"}, Env: proc.Shell.Env(env),
		Stdout: os.Stdout}
	fmt.Println(proc.Run(context.Background(), shell))

	ctx, stop := context.WithTimeoutCause(context.Background(), 50*time.Millisecond, errors.New("ran past its limit"))
	defer stop()
	fmt.Println(proc.Run(ctx, proc.Command{Path: "sleep", Args: []string{"60"}, Env: env}))

	fmt.Println(proc.Run(context.Background(), proc.Command{Path: "no-such-program", Env: env}))

	stderr := proc.NewTail(7)
	_, _ = fmt.Fprint(stderr, "a long line, and its end")
	fmt.Printf("%q\n", stderr)
	// Output:
	// [PATH=/usr/bin:/bin HOME=/home/dev]
	// [PATH=/usr/bin:/bin HOME=/home/dev ANTHROPIC_API_KEY=key]
	// /home/dev
	// exit status 3
	// ran past its limit
	// no-such-program is not on the PATH: /usr/bin:/bin
	// "its end"
}
