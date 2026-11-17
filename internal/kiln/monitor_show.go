package kiln

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/fwilkerson/kiln/internal/proc"
)

// standing is where a monitor stands, as kiln status, kiln monitor show and kiln tui show it.
type standing struct {
	// result is what its last run came to, such as healthy, or held: stopped.
	result string
	// outcome is the outcome of the fix the result names, or "". fixed is whether the result names a fix.
	outcome string
	fixed   bool
	// summary is the check's summary, the report's summary, the session's error, or why the check couldn't run.
	summary, report string
	// last is when its last run started, and next when it is due again. Each is zero for a monitor that never ran.
	last, next time.Time
}

// neverRun is the result of a monitor that never ran.
const neverRun = "never run"

// standing is where m stands, from its history.
func (h *history) standing(m monitor) standing {
	if h == nil || h.last.Kind == "" {
		return standing{result: neverRun}
	}

	s := standing{last: h.last.At, next: h.next(m)}
	e := h.last
	switch {
	case e.Found == foundHeld && h.lastFix.Kind != "":
		s.outcome, s.fixed = h.result(h.lastFix), true
		s.result = foundHeld + ": " + fixResult(s.outcome)
		s.summary, s.report = h.fixSummary(h.lastFix), h.outcomes[h.lastFix.Build].Report
	case e.Found == foundFix:
		s.outcome, s.fixed = h.result(e), true
		s.result = fixResult(s.outcome)
		s.summary, s.report = h.fixSummary(e), h.outcomes[e.Build].Report
	default:
		s.result, s.summary = e.Found, e.Summary
	}

	return s
}

// fixResult is a fix's outcome, in the words kiln status shows.
func fixResult(outcome string) string {
	switch outcome {
	case "":
		return "fix building"
	case outWaits:
		return "waiting on the developer"
	case outDropped:
		return "cut short"
	}

	return outcome
}

// monitorStatus shows each monitor kiln.json names at main's tip. A line holds its result, when it last ran, and
// when it runs next. Its summary follows.
func (r *Repo) monitorStatus(ctx context.Context, out io.Writer) error {
	main, err := r.git.Tip(ctx, mainBranch)
	if err != nil {
		return err
	}

	c, err := r.config(ctx, main)
	if err != nil || len(c.Monitors) == 0 {
		return nil
	}

	entries, err := r.store.entries()
	if err != nil {
		return err
	}

	k, err := r.store.read()
	if err != nil {
		return err
	}

	all := histories(entries)
	now := r.now()
	fmt.Fprintln(out, "monitors:")
	for _, m := range c.Monitors {
		h := all[m.Name]
		fmt.Fprintf(out, "%s: %s, %s\n", m.Name, h.standing(m).result, h.standing(m).when(h.due(m, now)))
		for _, line := range h.standing(m).lines(m.Name, k.find(m.Name)) {
			fmt.Fprintln(out, indent(line))
		}
	}

	return nil
}

// lines are what kiln status shows below the monitor name's line. They are its summary, why its fix stopped, and the
// command to run next. rec is the record of its fix, if it has one.
func (s standing) lines(name Spec, rec *record) []string {
	var lines []string
	if s.summary != "" {
		lines = append(lines, s.summary)
	}

	// A report is long, so status shows only a stopped fix's, which says what failed.
	if s.outcome == outStopped && s.report != "" {
		lines = append(lines, s.report)
	}

	if next := s.step(name, rec); next != "" {
		lines = append(lines, "next: "+next)
	}

	return lines
}

// step is the command the developer runs next on the monitor name. rec is the record of its fix, if the book holds
// one. It says where the fix stands now, such as an accepted landing that stopped, which the ledger's outcome can't.
func (s standing) step(name Spec, rec *record) string {
	if rec != nil && rec.Monitor {
		return fixStep(*rec)
	}

	return fixNext(name, s.outcome)
}

// when says when the monitor last ran, and when it runs next. due is whether it is due now.
func (s standing) when(due bool) string {
	last := "never ran"
	if !s.last.IsZero() {
		last = "last ran " + shownTime(s.last.Local())
	}

	if due {
		return last + ", runs at the next kiln monitor run"
	}

	return last + ", runs next after " + shownTime(s.next.Local())
}

// fixNext is the command the developer runs next on the fix of the monitor name, which ended with outcome. It is ""
// when the developer has nothing to run.
func fixNext(name Spec, outcome string) string {
	switch outcome {
	case outStopped:
		return fmt.Sprintf("read why it stopped, fix main, then run kiln monitor run %s", name)
	case outFailed:
		return fmt.Sprintf("fix what the error names, then run kiln monitor run %s", name)
	case outNeedsYou:
		return fmt.Sprintf("read the report, see to it, then run kiln monitor run %s", name)
	case outWaits:
		// A fix holds a commit for each round, so git show would print the last round alone.
		return fmt.Sprintf("read it with git diff main...kiln/%s, then run kiln accept %s, or kiln reject %s", name,
			name, name)
	}

	return ""
}

// MonitorShow shows how the last run of the monitor name ended, in the window kiln opens for it. It writes an OSC 9
// sequence, which Ghostty shows as a notification. The result follows, with its summary, report or error, and the
// command to run next. Then it waits for return on in.
func (r *Repo) MonitorShow(ctx context.Context, name Spec, in io.Reader, out io.Writer) error {
	entries, err := r.store.entries()
	if err != nil {
		return err
	}

	h := histories(entries)[name]
	if h == nil {
		return fmt.Errorf("%w: the ledger holds no run of a monitor %s", errRefused, name)
	}

	k, err := r.store.read()
	if err != nil {
		return err
	}

	s := h.standing(monitor{Name: name})
	// The body starts with kiln:, so a monitor named 4 can't read as OSC 9;4, a progress bar Ghostty draws.
	fmt.Fprintf(out, "\x1b]9;kiln: %s %s\a", name, s.result)
	fmt.Fprintf(out, "%s: %s, at %s\n", name, s.result, shownTime(s.last.Local()))
	for _, text := range []string{s.summary, s.report} {
		if text != "" {
			fmt.Fprintln(out, indent(text))
		}
	}

	if next := s.step(name, k.find(name)); next != "" {
		fmt.Fprintln(out, indent("next: "+next))
	}

	fmt.Fprint(out, "\nPress return to close.")
	_, _ = bufio.NewReader(in).ReadString('\n')
	return nil
}

// openVar names the command kiln runs in place of open, to open a window.
const openVar = "KILN_OPEN"

// open opens a Ghostty window in the background, which runs kiln monitor show for the monitor name. LaunchServices
// passes on neither the working directory nor the PATH, so kiln names its own path and the repository's.
func (r *Repo) open(ctx context.Context, name Spec) error {
	args := []string{"-gna", "Ghostty", "--args", "--working-directory=" + r.top, "-e", r.exe, "monitor", "show",
		string(name)}
	cmd := exec.CommandContext(ctx, "open", args...)
	if opener := envValue(r.env, openVar); opener != "" {
		cmd = opening(ctx, opener, args)
	}

	cmd.Env = proc.Shell.Env(r.env)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", cmd.Path, err, strings.TrimSpace(string(out)))
	}

	return nil
}

// opening is the command that runs opener with args. An opener that is no file is a shell command line.
func opening(ctx context.Context, opener string, args []string) *exec.Cmd {
	if _, err := os.Stat(opener); err == nil || !strings.ContainsAny(opener, " \t") {
		return exec.CommandContext(ctx, opener, args...)
	}

	return exec.CommandContext(ctx, "/bin/sh", append([]string{"-c", opener + ` "$@"`, "sh"}, args...)...)
}
