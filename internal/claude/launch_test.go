package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestMCP holds the rule that a session calls only the MCP servers its Launch names. With none named, it calls none.
func TestMCP(t *testing.T) {
	for name, tc := range map[string]struct {
		servers map[string]json.RawMessage
		want    string
	}{
		"a Launch with no servers reaches none": {want: `{"mcpServers":{}}`},
		"a Launch's servers are the session's": {
			servers: map[string]json.RawMessage{"codescene": json.RawMessage(`{"command":"cs-mcp-auth"}`)},
			want:    `{"mcpServers":{"codescene":{"command":"cs-mcp-auth"}}}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			l := Launch{Session: "s", Role: "build", MCP: tc.servers}
			if err := write(dir, l); err != nil {
				t.Fatal(err)
			}

			args := arguments(l, dir)
			at := slices.Index(args, "--mcp-config")
			if at < 0 || !slices.Contains(args, "--strict-mcp-config") {
				t.Fatalf("the session is not held to its own servers: %q", args)
			}

			got, err := os.ReadFile(args[at+1])
			if err != nil || string(got) != tc.want {
				t.Fatalf("%s holds %s, %v; want %s", filepath.Base(args[at+1]), got, err, tc.want)
			}
		})
	}
}

// TestSandbox holds the rules that a session's commands may listen on a local port, and never run unsandboxed.
// Without the first, the sandbox refuses each test that starts a server. They reach Unix sockets when UnixSockets.
func TestSandbox(t *testing.T) {
	dir := t.TempDir()
	if err := write(dir, Launch{Session: "s", Role: "build", UnixSockets: true}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, settingsFile))
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Sandbox struct {
			FailIfUnavailable bool `json:"failIfUnavailable"`
			Network           struct {
				AllowLocalBinding   bool `json:"allowLocalBinding"`
				AllowAllUnixSockets bool `json:"allowAllUnixSockets"`
			} `json:"network"`
		} `json:"sandbox"`
	}
	if err := json.Unmarshal(raw, &got); err != nil || !got.Sandbox.Network.AllowLocalBinding {
		t.Fatalf("%s lets no command listen on a local port: %v\n%s", settingsFile, err, raw)
	}

	if !got.Sandbox.FailIfUnavailable {
		t.Fatalf("%s runs commands unsandboxed when the sandbox cannot start:\n%s", settingsFile, raw)
	}

	if !got.Sandbox.Network.AllowAllUnixSockets {
		t.Fatalf("%s refuses each Unix socket, though the Launch allows them:\n%s", settingsFile, raw)
	}
}

// TestInteract holds the interactive launch: its arguments, its directory, and the file of its system prompt. It
// passes kiln's environment whole, Claude Code's own variables included.
func TestInteract(t *testing.T) {
	dir, work := t.TempDir(), t.TempDir()
	program := filepath.Join(dir, "claude")
	record := filepath.Join(dir, "record")
	script := "#!/bin/sh\n{ pwd -P; for a in \"$@\"; do echo \"$a\"; done; " +
		"echo \"$CLAUDECODE $ANTHROPIC_API_KEY\"; } >" + record + "\nexit 3\n"
	if err := os.WriteFile(program, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	env := []string{"PATH=/usr/bin:/bin", "CLAUDECODE=1", "ANTHROPIC_API_KEY=k"}
	c := New(Config{Program: program, Env: env, Sessions: filepath.Join(dir, "sessions")})
	err := c.Interact(Interactive{Session: "s", Role: "tend", System: "know-how", Prompt: "first\nmessage", Dir: work})
	if err == nil {
		t.Error("a session that exited 3 answered no error")
	}

	system := filepath.Join(c.Dir("s"), "tend.md")
	work, _ = filepath.EvalSymlinks(work)
	want := strings.Join([]string{work, "--session-id", "s", "--append-system-prompt-file", system, "first",
		"message", "1 k"}, "\n") + "\n"
	got, _ := os.ReadFile(record)
	held, _ := os.ReadFile(system)
	if string(got) != want || string(held) != "know-how" {
		t.Errorf("the session ran as\n%s\nwith %s holding %q; want\n%s\nand %q", got, system, held, want, "know-how")
	}
}
