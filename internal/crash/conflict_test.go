package crash_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
)

// TestKilledBeforeLandingConflict kills kiln accept as its landing's conflict turn starts. kiln has recorded the
// turn, but the fix's session never got the conflict report. The next kiln recovers the landing. The first thing it
// tells the session must be which files conflict with main, and what to do with them.
func TestKilledBeforeLandingConflict(t *testing.T) {
	keepBudget(t)
	t.Parallel()
	s := newScratch(t, conflict)
	s.calm = -1
	killed := false
	var told []string
	s.watch(func(st step) string {
		l := s.holds(conflict, st, 0)
		turn := st.what == "session start" && landingRun.MatchString(l.stands)
		if killed || !turn {
			return "GO"
		}

		killed, told = true, s.fixPrompts()
		return "KILL"
	})

	s.judge("", "killed as the landing's conflict turn started", conflict.drive(s))
	recovered := slices.DeleteFunc(s.fixPrompts(), func(p string) bool { return slices.Contains(told, p) })
	if !killed || len(recovered) == 0 {
		t.Fatalf("kiln accept started no session for a conflict, or none ran after the kill:\n%s", s.said.String())
	}

	prompt, err := os.ReadFile(recovered[0])
	if err != nil {
		t.Fatal(err)
	}

	for says, want := range map[string]string{
		"names each file in conflict with main":    `(?m)^doc\.txt$`,
		"says to resolve each conflict and commit": `(?i)resolve each conflict, commit`,
	} {
		if !regexp.MustCompile(want).Match(prompt) {
			t.Errorf("the fix's session, first told after the kill, never %s: it was told\n%s\n\n%s", says, prompt,
				s.said.String())
		}
	}
}

// landingRun matches the record of an accepted landing whose code session's run kiln marked.
var landingRun = regexp.MustCompile(`"accepted":"set".*"from":"set"|"from":"set".*"accepted":"set"`)

// fixPrompts lists the prompt of each call of a fix's session, in the order its records sort.
func (s *scratch) fixPrompts() []string {
	prompts, _ := filepath.Glob(filepath.Join(s.work, scenariotest.ClaudeCallsDir, "fix", "*", "*",
		scenariotest.ClaudePromptFile))
	return prompts
}

// conflict is the flow of a fix that waits for the developer. The developer then changes, on main, the line the fix
// changed. kiln accept meets the conflict, and sends it to the fix's session.
var conflict = flow{
	name:    "conflict",
	subject: "stale",
	landed:  []string{`by hand`, `mark the doc fixed`},
	outcome: "landed",
	next: func(s *scratch) []string {
		switch shown := s.shown(s.subject); {
		case strings.HasPrefix(shown, "landed"), strings.HasPrefix(shown, "healthy"):
			return nil
		case strings.HasPrefix(shown, "waiting"):
			s.editByHand()
			return []string{"accept", "stale"}
		case strings.HasPrefix(shown, "stopped"):
			return []string{"accept", "stale"}
		}

		return []string{"monitor", "run"}
	},
}

// editByHand changes doc.txt in a commit on main, in the line the fix changed, unless main holds the edit.
func (s *scratch) editByHand() {
	path := filepath.Join(s.work, "doc.txt")
	if raw, _ := os.ReadFile(path); string(raw) != "title\nstale\n" {
		return
	}

	if err := os.WriteFile(path, []byte("title\nstale, by hand\n"), 0o644); err != nil {
		s.t.Fatal(err)
	}

	s.run("git", "commit", "-qam", "docs: edit by hand")
	s.main = ""
}
