package claude

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/fwilkerson/kiln/internal/proc"
)

// Interactive is the one session a developer works in at kiln's own terminal, as kiln tend starts it.
type Interactive struct {
	// Session is the new session's ID, a UUID.
	Session string
	// Role names the file under the session's directory that holds System, such as "tend".
	Role   string
	System string
	// Prompt is the session's first message.
	Prompt string
	// Dir is the absolute directory the session works in.
	Dir string
	// In, Out and Err are the session's standard input, output and error: kiln's own.
	In       io.Reader
	Out, Err io.Writer
}

// Interact runs the session i by [proc.RunAttached], with the developer at the keyboard, and waits for it to
// exit. The session inherits Config.Env whole. Its error is a *[proc.NotOnPath] for a claude it cannot find, or the
// session's own exit when not 0.
func (c Claude) Interact(i Interactive) error {
	program, err := c.Program()
	if err != nil {
		return err
	}

	dir := c.Dir(i.Session)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	system := filepath.Join(dir, i.Role+".md")
	if err := os.WriteFile(system, []byte(i.System), 0o600); err != nil {
		return err
	}

	// The developer leaves Claude Code with Ctrl-C, which must end only the session, so no context kills it.
	args := []string{"--session-id", i.Session, "--append-system-prompt-file", system, i.Prompt}
	err = proc.RunAttached(proc.Command{Path: program, Args: args, Dir: i.Dir, Env: c.c.Env, Stdin: i.In,
		Stdout: i.Out, Stderr: i.Err})
	if exit, ok := errors.AsType[*proc.Exited](err); ok {
		return fmt.Errorf("the session ended: %s", exit.State)
	}

	return err
}

// Dir is the directory that holds the files of the session named session.
func (c Claude) Dir(session string) string {
	return filepath.Join(c.c.Sessions, session)
}

// Program is the claude a session runs, as Config.Program finds it. Its error is a *[proc.NotOnPath].
func (c Claude) Program() (string, error) {
	return proc.Look(c.c.Env, c.c.Program)
}
