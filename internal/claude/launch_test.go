package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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

// TestListens holds the rule that a session's commands may listen on a local port. Without it, the sandbox refuses
// each test that starts a server.
func TestListens(t *testing.T) {
	dir := t.TempDir()
	if err := write(dir, Launch{Session: "s", Role: "build"}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, settingsFile))
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Sandbox struct {
			Network struct {
				AllowLocalBinding bool `json:"allowLocalBinding"`
			} `json:"network"`
		} `json:"sandbox"`
	}
	if err := json.Unmarshal(raw, &got); err != nil || !got.Sandbox.Network.AllowLocalBinding {
		t.Fatalf("%s lets no command listen on a local port: %v\n%s", settingsFile, err, raw)
	}
}
