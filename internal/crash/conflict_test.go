package crash_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
	"golang.org/x/sys/unix"
)

// TestKilledBeforeLandingConflict kills kiln accept as its landing's conflict turn starts. kiln has recorded the
// turn, but the fix's session never got the conflict report. kiln monitor run then recovers the landing. It must tell
// the session which files conflict with main, and what to do with them.
func TestKilledBeforeLandingConflict(t *testing.T) {
	keepBudget(t)
	t.Parallel()
	s := newScratch(t, flow{name: "conflict", subject: "stale"})
	s.run(s.env, "kiln", "monitor", "run")
	if shown := s.shown(s.subject); !strings.HasPrefix(shown, "waiting") {
		t.Fatalf("kiln status shows the fix %q, not waiting:\n%s", shown, s.said.String())
	}

	if err := os.WriteFile(filepath.Join(s.work, "doc.txt"), []byte("title\nstale, by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s.run(s.env, "git", "commit", "-qam", "docs: edit by hand")

	// A run that no kill cuts short labels the steps of kiln accept. The kill then comes at its first session's start.
	root, before := filepath.Dir(s.work), filepath.Join(t.TempDir(), "before")
	if err := unix.Clonefile(root, before, unix.CLONE_NOFOLLOW); err != nil {
		t.Fatal(err)
	}

	steps := filepath.Join(t.TempDir(), "steps")
	s.run(append(slices.Clone(s.env), "KILN_CRASH_LOG="+steps), "kiln", "accept", "stale")
	at := slices.Index(labels(steps), "session start") + 1
	if at == 0 {
		t.Fatalf("kiln accept started no session:\n%s", s.said.String())
	}

	if err := restore(before, root); err != nil {
		t.Fatal(err)
	}

	killed := filepath.Join(t.TempDir(), "killed")
	s.run(append(slices.Clone(s.env), "KILN_CRASH_LOG="+killed, "KILN_CRASH_AT="+strconv.Itoa(at)), "kiln", "accept",
		"stale")
	told := s.fixPrompts()
	s.run(s.env, "kiln", "monitor", "run")
	recovered := slices.DeleteFunc(s.fixPrompts(), func(p string) bool { return slices.Contains(told, p) })
	if len(recovered) == 0 {
		t.Fatalf("no fix session ran after the kill:\n%s", s.said.String())
	}

	raw, err := os.ReadFile(recovered[0])
	if err != nil {
		t.Fatal(err)
	}

	prompt := string(raw)
	for _, c := range []struct {
		name string
		want *regexp.Regexp
	}{
		{"names each file in conflict with main", regexp.MustCompile(`(?m)^doc\.txt$`)},
		{"says to resolve each conflict and commit", regexp.MustCompile(`(?i)resolve each conflict, commit`)},
	} {
		if !c.want.MatchString(prompt) {
			t.Errorf("the fix's session, first told after the kill, %s: it was told\n%s\n\n%s", c.name, prompt,
				s.said.String())
		}
	}
}

// fixPrompts lists the prompt of each call of a fix's session, in the order its records sort.
func (s *scratch) fixPrompts() []string {
	prompts, _ := filepath.Glob(filepath.Join(s.work, scenariotest.ClaudeCallsDir, "fix", "*", "*",
		scenariotest.ClaudePromptFile))
	return prompts
}
