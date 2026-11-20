package claude_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// forking stands in for a claude that prints first, then forks children in a loop. A child that escapes the kill holds
// the session's output open for 20 s.
const forking = `#!/bin/bash
cat >/dev/null
echo '%s'
while :; do (sleep 20 &); done
`

// TestKillForks holds that Run returns promptly once it kills a session, at overage or when its context ends.
// No child the session forked as it died holds Run past the kill. A child that did would hold Run for the whole grace
// that the package doc gives a child, 10 s. A run that kills every child takes a fraction of that, however loaded.
func TestKillForks(t *testing.T) {
	const grace = 10 * time.Second
	cases := []struct {
		name, first string
		limited     bool
	}{
		{"overage", `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","isUsingOverage":true}}`, true},
		{"interrupted", `{"type":"system"}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for range 6 {
				began := time.Now()
				ended := runForking(t, c.first, !c.limited)
				switch took := time.Since(began); {
				case (ended.Limit != nil) != c.limited:
					t.Fatalf("Run answered %+v; want a usage limit: %v", ended, c.limited)
				case took >= grace:
					t.Fatalf("Run took %v to return after the kill, so a child held the session's output", took)
				}
			}
		})
	}
}

// runForking runs forking, which prints first, through Run in a directory of its own, and answers how it ended. With
// interrupt, Run's context ends once the session has printed, and so forks as it dies.
func runForking(t *testing.T, first string, interrupt bool) claude.Ended {
	const session = "0b8e6c1e-2f4d-4f6a-9b1c-3d5e7f9a1b2d"
	dir := t.TempDir()
	program := filepath.Join(dir, "claude")
	if err := os.WriteFile(program, fmt.Appendf(nil, forking, first), 0o700); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sessions := filepath.Join(dir, "sessions")
	if interrupt {
		go func() {
			defer cancel()
			for ctx.Err() == nil {
				info, err := os.Stat(filepath.Join(sessions, session, "stream.jsonl"))
				if err == nil && info.Size() > 0 {
					return
				}

				time.Sleep(10 * time.Millisecond)
			}
		}()
	}

	c := claude.New(claude.Config{Program: program, Env: os.Environ(), Sessions: sessions})
	ended, _ := c.Run(ctx, claude.Launch{Session: session, Role: "build", Prompt: "Build x.", Dir: dir})
	return ended
}
