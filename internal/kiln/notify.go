package kiln

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/proc"
)

// notice is one notification: a spec's build or a monitor's fix that needs the developer. The notify program reads
// it as JSON in $KILN_NOTICE, as the package doc's Notifications says.
type notice struct {
	Kind    string  `json:"kind"`
	Name    Spec    `json:"name"`
	State   string  `json:"state"`
	Summary string  `json:"summary"`
	Next    string  `json:"next"`
	Top     string  `json:"top"`
	Kiln    string  `json:"kiln"`
	Commit  git.Rev `json:"commit,omitempty"`
}

// The kinds of notice.
const (
	specNotice    = "spec"
	monitorNotice = "monitor"
)

// notifyWait is how long kiln waits for the notify program before it kills the program, and what it started.
const notifyWait = 30 * time.Second

// notify hands n to the program kiln.json's notify names at main's tip. It prints n as OSC 9 on out when kiln.json
// names none, or is unreadable, or the program fails. So no notification is lost, and kiln says why on out.
func (r *Repo) notify(ctx context.Context, n notice, out io.Writer) {
	command, err := r.notifyCommand(ctx)
	if err == nil && command != "" {
		err = r.deliver(ctx, command, n, out)
		if err == nil {
			return
		}
	}

	if err != nil {
		fmt.Fprintf(out, "%s: kiln could not notify: %v\n", n.Name, err)
	}

	// The body starts with kiln:, so a spec named 4 can't read as OSC 9;4, a progress bar Ghostty draws.
	fmt.Fprintf(out, "\x1b]9;kiln: %s %s\a", n.Name, n.State)
}

// notifyCommand is the command kiln.json's notify names at main's tip, or "".
func (r *Repo) notifyCommand(ctx context.Context) (string, error) {
	main, err := r.mainTip(ctx)
	if err != nil {
		return "", err
	}

	c, err := r.config(ctx, main)
	return c.Notify, err
}

// noticeVar names the variable that holds the notice, as JSON, in the notify program's environment.
const noticeVar = "KILN_NOTICE"

// deliver runs command in the repository's top, outside the sandbox, with n as JSON in $KILN_NOTICE. What it printed
// goes to out. It answers why the command failed, or ran past notifyWait.
func (r *Repo) deliver(ctx context.Context, command string, n notice, out io.Writer) error {
	raw, err := json.Marshal(n)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, notifyWait)
	defer cancel()
	env := append(slices.Clone(proc.Shell.Env(r.env)), noticeVar+"="+string(raw))
	printed, err := runShell(ctx, git.Worktree(r.top), command, env)
	fmt.Fprint(out, printed.both)
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("%s ran past %s, so kiln stopped it", command, notifyWait)
	case err != nil:
		return fmt.Errorf("%s: %w", command, err)
	}

	return nil
}

// specNoticeOf is the notice of rec, a spec's build that ended waiting on the developer. lines sum the build up, as
// sumUp does, and landed is the commit it landed, or "".
func (r *Repo) specNoticeOf(rec record, tree git.Worktree, lines []string, landed git.Rev) notice {
	// sumUp's last line is the next step, which the notice holds apart.
	summary := lines[:len(lines)-1]
	if why, _, _ := strings.Cut(rec.Why, "\n"); rec.State == stopped && why != "" {
		summary = append([]string{why}, summary...)
	}

	return notice{Kind: specNotice, Name: rec.Name, State: shownState(&rec), Summary: strings.Join(summary, "\n"),
		Next: nextStep(rec, tree, landed), Top: r.top, Kiln: r.exe, Commit: landed}
}

// notifyFix notifies how the last run of the monitor name ended, as kiln monitor show shows it. landed is the commit
// its fix landed, or "".
func (r *Repo) notifyFix(ctx context.Context, name Spec, landed git.Rev, out io.Writer) {
	s, next, err := r.lastStanding(name)
	if err != nil {
		fmt.Fprintf(out, "%s: kiln could not notify: %v\n", name, err)
		return
	}

	summary := strings.Join(slices.DeleteFunc([]string{s.summary, s.report}, isEmpty), "\n")
	r.notify(ctx, notice{Kind: monitorNotice, Name: name, State: s.result, Summary: summary, Next: next, Top: r.top,
		Kiln: r.exe, Commit: landed}, out)
}

// isEmpty is whether s is "".
func isEmpty(s string) bool { return s == "" }

// notified are the outcomes of a fix that notify.
var notified = []string{outLanded, outStopped, outWaits, outNeedsYou, outFailed}

// notifies is whether a fix that ended with outcome notifies. h is its monitor's history before this run. A session
// that failed as the monitor's last run did notifies nothing. An expired sign-in then notifies once per monitor.
func notifies(outcome string, h *history) bool {
	if !slices.Contains(notified, outcome) {
		return false
	}

	return outcome != outFailed || h.lastResult() != outFailed
}

// lastResult is what the monitor's last run came to, or "" if it never ran.
func (h *history) lastResult() string {
	if h == nil || h.last.Kind == "" {
		return ""
	}

	return h.result(h.last)
}
