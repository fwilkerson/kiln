package scenariotest

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// A session's transcript is where Claude Code keeps it, under $HOME, so a resume finds it as Claude Code would.
var nonAlphanumeric = regexp.MustCompile(`[^A-Za-z0-9]`)

func claudeProjects() string {
	return filepath.Join(os.Getenv("HOME"), ".claude", "projects")
}

func newTranscript(dir, sessionID string) string {
	return filepath.Join(claudeProjects(), nonAlphanumeric.ReplaceAllString(dir, "-"), sessionID+".jsonl")
}

// findTranscript finds a session's transcript in any project, or answers "" when none holds it.
func findTranscript(sessionID string) string {
	projects, _ := os.ReadDir(claudeProjects())
	for _, p := range projects {
		path := filepath.Join(claudeProjects(), p.Name(), sessionID+".jsonl")
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}

	return ""
}

func (s *claudeSession) keepTranscript(prompt []byte) error {
	if err := os.MkdirAll(filepath.Dir(s.transcript), 0o755); err != nil {
		return err
	}

	line, err := marshalJSON(jsonObject{
		{"type", "user"}, {"message", jsonObject{{"role", "user"}, {"content", string(prompt)}}},
		{"sessionId", s.call.SessionID}, {"cwd", s.call.Dir}, {"uuid", newUUID()},
		{"timestamp", time.Now().UTC().Format(time.RFC3339Nano)},
	})
	if err != nil {
		return err
	}

	f, err := os.OpenFile(s.transcript, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}

	_, err = f.Write(append(line, '\n'))
	return errors.Join(err, f.Close())
}
