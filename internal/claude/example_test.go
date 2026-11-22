package claude_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fwilkerson/kiln/internal/claude"
)

// fake stands in for the claude command. It reads the prompt and prints one result event, in the stream-json format of
// claude -p.
const fake = `#!/bin/sh
cat >/dev/null
echo '{"type":"result","is_error":false,"result":"Built.","num_turns":3}'
`

// Example runs a session, reads how it ended, and lists the files kiln keeps of it.
func Example() {
	dir, err := os.MkdirTemp("", "claude-example")
	if err != nil {
		panic(err)
	}

	defer os.RemoveAll(dir)
	program := filepath.Join(dir, "claude")
	if err := os.WriteFile(program, []byte(fake), 0o700); err != nil {
		panic(err)
	}

	sessions := filepath.Join(dir, "sessions")
	c := claude.New(claude.Config{Program: program, Env: os.Environ(), Sessions: sessions})
	id := "0b8e6c1e-2f4d-4f6a-9b1c-3d5e7f9a1b2c"
	ended, err := c.Run(context.Background(), claude.Launch{
		Session: id, Role: "build", System: "You build specs.", Prompt: "Build x.", Dir: dir, Silence: time.Hour,
	})
	fmt.Println(ended.Text, ended.Failed, ended.Limit, *ended.Totals.Turns, err)

	files, err := os.ReadDir(filepath.Join(sessions, id))
	for _, f := range files {
		fmt.Print(f.Name(), " ")
	}

	fmt.Println(err)
	// Output:
	// Built. false <nil> 3 <nil>
	// build.md mcp.json settings.json stderr stream.jsonl <nil>
}
