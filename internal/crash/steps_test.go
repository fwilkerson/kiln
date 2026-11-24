package crash_test

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// step is one step a kiln took in a scratch repository, by its number in the log.
type step struct {
	n, pid int
	what   string
}

// kind names the function that marked the step: the book's write, the ledger's append, git's run, or a session's.
func (st step) kind() string {
	for _, shared := range []string{"git", "ledger.jsonl"} {
		if strings.HasPrefix(st.what, shared+" ") {
			return shared
		}
	}

	return st.what
}

// session is whether the step starts or ends a session, where kiln reads its context's end.
func (st step) session() bool {
	return strings.HasPrefix(st.what, "session ") || strings.HasPrefix(st.what, "attached ")
}

// started is how many more sessions the step's kiln runs once the step goes on: one more, or one fewer.
func (st step) started() int {
	switch st.what {
	case "session start":
		return 1
	case "session end":
		return -1
	}

	return 0
}

// leaves names the step st, which starts or ends a session, by what a kill there would leave: l.
func (st step) leaves(l leftover) string {
	return fmt.Sprintf("%s, %s, %q", st.what, l.stands, l.roles)
}

// signals are the real signals one run of a flow gets. kiln gets SIGKILL at its first step of each kind in kills. It
// gets SIGINT at its first step that leaves each session's leftover, as leaves names it, and sent keeps each. A run
// decides by its own steps alone, since no other run is sure to show the same leftovers.
type signals struct {
	kills []string
	sent  map[string]bool
}

// at answers what kiln gets at a step of kind: GO, INT or KILL. session names the step, if it starts or ends a
// session, and is "" if not. at counts the signal sent.
func (sig *signals) at(session, kind string) string {
	if session != "" && !sig.sent[session] {
		sig.sent[session] = true
		return "INT"
	}

	if i := slices.Index(sig.kills, kind); i >= 0 {
		sig.kills = slices.Delete(sig.kills, i, i+1)
		return "KILL"
	}

	return "GO"
}

// watch makes each kiln that runs in s ask at each of its steps, as package crash says. decide answers a step with
// GO, KILL or INT. output calls it, while the command that took the step runs.
func (s *scratch) watch(decide func(step) string) {
	asks := s.t.TempDir()
	log, err := os.Create(filepath.Join(asks, "steps"))
	if err != nil {
		s.t.Fatal(err)
	}

	// A scratch that a round watched before asks in that round's directory.
	s.env = slices.DeleteFunc(s.env, func(v string) bool { return strings.HasPrefix(v, "KILN_CRASH_ASK=") })
	s.env = append(s.env, "KILN_CRASH_ASK="+asks)
	s.steps = make(chan step)
	s.answer = func(st step) {
		if err := os.WriteFile(filepath.Join(asks, strconv.Itoa(st.n)), []byte(decide(st)+"\n"), 0o600); err != nil {
			s.t.Error(err)
		}
	}

	done, ended := make(chan struct{}), make(chan struct{})
	s.t.Cleanup(func() {
		close(done)
		<-ended
		log.Close()
	})

	go func() {
		defer close(ended)
		lines := bufio.NewReader(log)
		for line := ""; ; {
			// A kiln holds the log's lock until its step is answered. So the log grows by one line at a time.
			more, err := lines.ReadString('\n')
			line += more
			// A whole line waits for the test to take it, however long. Half a line waits for its rest.
			next, wait := s.steps, (<-chan time.Time)(nil)
			if err != nil {
				next, wait = nil, time.After(time.Millisecond)
			}

			select {
			case next <- stepOf(line):
				line = ""
			case <-wait:
			case <-done:
				return
			}
		}
	}()
}

// stepOf reads a line of the log, as package crash writes it.
func stepOf(line string) step {
	var st step
	fmt.Sscanf(line, "%d %d", &st.n, &st.pid)
	_, rest, _ := strings.Cut(strings.TrimSpace(line), " ")
	_, st.what, _ = strings.Cut(rest, " ")
	return st
}

// output runs cmd, and answers what it printed. While cmd runs, it answers each step a kiln asks at, as watch set.
func (s *scratch) output(cmd *exec.Cmd) (string, error) {
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		return "", err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	for {
		select {
		case st := <-s.steps:
			s.answer(st)
		case err := <-done:
			return out.String(), err
		}
	}
}
