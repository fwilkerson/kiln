package proc_test

import (
	"slices"
	"testing"

	"github.com/fwilkerson/kiln/internal/proc"
)

func TestSessionEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kv   string
		kept bool
	}{
		{"the token claude setup-token signs an unattended session in with", "CLAUDE_CODE_OAUTH_TOKEN=token", true},
		{"the account a developer with two chose", "CLAUDE_CONFIG_DIR=/home/dev/.claude-work", true},
		{"the provider Bedrock", "CLAUDE_CODE_USE_BEDROCK=1", true},
		{"the provider Vertex", "CLAUDE_CODE_USE_VERTEX=1", true},
		{"the session kiln may run inside", "CLAUDECODE=1", false},
		{"the entry point of that session", "CLAUDE_CODE_ENTRYPOINT=cli", false},
		{"a name the kept one only begins", "CLAUDE_CONFIG_DIRS=/elsewhere", false},
		{"an API key", "ANTHROPIC_API_KEY=key", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := slices.Contains(proc.Session.Env([]string{tt.kv}), tt.kv); got != tt.kept {
				t.Errorf("Session.Env keeps %s: %v, want %v", tt.kv, got, tt.kept)
			}
		})
	}
}
