package claude_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fwilkerson/kiln/internal/claude"
)

// overage stands in for a claude that bills overage, then works on for 2 s to a result. It writes ran-on in its
// working directory once it has worked on. After the overage it runs no other program, so no child escapes a kill.
const overage = `#!/bin/bash
cat >/dev/null
mkfifo never && exec 3<>never
echo '{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","isUsingOverage":true}}'
echo '{"type":"assistant","message":{"id":"m","content":[{"type":"text","text":"worked on"}]}}'
read -t 2 -u 3 _
echo '{"type":"result","is_error":false,"result":"Worked on at overage."}'
: >ran-on
`

// TestOverage holds that Run kills a session at its first event that bills overage. It reads none of the stream
// after, and answers a usage limit.
func TestOverage(t *testing.T) {
	const session = "0b8e6c1e-2f4d-4f6a-9b1c-3d5e7f9a1b2c"
	dir := t.TempDir()
	program := filepath.Join(dir, "claude")
	if err := os.WriteFile(program, []byte(overage), 0o700); err != nil {
		t.Fatal(err)
	}

	c := claude.New(claude.Config{Program: program, Env: os.Environ(), Sessions: filepath.Join(dir, "sessions")})
	ended, err := c.Run(t.Context(), claude.Launch{Session: session, Role: "build", Prompt: "Build x.", Dir: dir})
	stream, _ := os.ReadFile(filepath.Join(dir, "sessions", session, "stream.jsonl"))
	_, ranOn := os.Stat(filepath.Join(dir, "ran-on"))
	switch {
	case err != nil || ended.Limit == nil:
		t.Errorf("Run answered %+v, %v; want a usage limit", ended, err)
	case strings.Contains(string(stream), "worked on"):
		t.Errorf("Run read the stream past the overage:\n%s", stream)
	case !errors.Is(ranOn, fs.ErrNotExist):
		t.Errorf("the session worked on past the overage: %v", ranOn)
	}
}
