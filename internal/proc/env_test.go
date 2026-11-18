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

func TestGitEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kv   string
		kept bool
	}{
		{"the author a direnv or CI shell names", "GIT_AUTHOR_NAME=Dev", true},
		{"the author's email", "GIT_AUTHOR_EMAIL=dev@example.com", true},
		{"the committer", "GIT_COMMITTER_NAME=Dev", true},
		{"the committer's email", "GIT_COMMITTER_EMAIL=dev@example.com", true},
		{"a repository elsewhere", "GIT_DIR=/elsewhere", false},
		{"an index elsewhere", "GIT_INDEX_FILE=/elsewhere", false},
	}
	for _, tt := range tests {
		for _, c := range []proc.Child{proc.Shell, proc.Session, proc.Git} {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				if got := slices.Contains(c.Env([]string{tt.kv}), tt.kv); got != tt.kept {
					t.Errorf("child %d keeps %s: %v, want %v", c, tt.kv, got, tt.kept)
				}
			})
		}
	}
}
