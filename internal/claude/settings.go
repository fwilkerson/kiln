package claude

import (
	"bytes"
	"encoding/json"
	"slices"
)

const settingsFile = "settings.json"

// mcpFile holds a session's MCP servers, as Claude Code's --mcp-config reads them.
const mcpFile = "mcp.json"

// trustd verifies certificates on macOS. A Go program reaches it to check a host's TLS certificate, as go get does.
const trustd = "com.apple.trustd.agent"

// writeTools are the tools that write a file Claude Code names, and so the ones Hook is asked about.
const writeTools = "Edit|Write|MultiEdit|NotebookEdit"

// settings is the settings file a session is started with, over the project's own.
func settings(l Launch) ([]byte, error) {
	own := map[string]any{
		// No one answers a session's prompt. Without this key, auto mode aborts a headless session after 3 refusals.
		"permissions": map[string]any{"deny": nonNil(l.Deny), "denyInsteadOfPrompting": true},
		"autoMode": map[string]any{
			"environment": []string{"$defaults",
				"Nobody is at the keyboard: no question the session asks is answered.",
				"Remotes: nothing is pushed to any remote, by this session or any other."},
			"hard_deny": slices.Concat([]string{"$defaults"}, l.Forbidden),
		},
		"disableAllHooks": false,
		"sandbox": map[string]any{
			"enabled":                  true,
			"allowUnsandboxedCommands": false,
			"network": map[string]any{"allowedDomains": nonNil(l.Domains),
				"allowMachLookup": []string{trustd}},
			"filesystem": map[string]any{"allowWrite": nonNil(l.Writable), "denyRead": nonNil(l.DenyRead),
				"allowRead": nonNil(l.AllowRead)},
		},
	}
	// Claude Code has no flag for the advisor, and the user's settings, where it would be set, are never read.
	if l.Advisor != "" {
		own["advisorModel"] = l.Advisor
	}

	if l.Hook != "" {
		own["hooks"] = map[string]any{"PreToolUse": []any{map[string]any{
			"matcher": writeTools,
			"hooks":   []any{map[string]any{"type": "command", "command": l.Hook}},
		}}}
	}

	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	err := enc.Encode(own)
	return b.Bytes(), err
}

func nonNilMap[V any](items map[string]V) map[string]V {
	if items == nil {
		return map[string]V{}
	}

	return items
}

func nonNil(items []string) []string {
	if items == nil {
		return []string{}
	}

	return items
}
