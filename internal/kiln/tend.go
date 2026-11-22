package kiln

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/tui"
)

// tendContract is the know-how kiln appends to the system prompt of a kiln tend session. No file of a project changes
// it.
//
//go:embed contract/tend.md
var tendContract string

// tendRole names the file that holds tendContract, in the tend session's directory.
const tendRole = "tend"

// noteFile is the note a tend session leaves for the stopped build, in the tend session's directory.
const noteFile = "note.md"

// Terminal is kiln's own standard input, output and error, which an interactive session takes in turn.
type Terminal struct {
	In       io.Reader
	Out, Err io.Writer
}

// Tend opens an interactive Claude Code session of the developer's own on name, in term. That is a spec or a monitor
// that waits on a repair. Its first message says where name stands, and why. It says where name's work, sessions and
// files are. Once the session exits, kiln goes on with name at once, as the package doc's "kiln tend" says. Tend
// refuses a name that waits on no repair. It refuses while another kiln tend is at work on name. It fails with a
// *proc.NotOnPath, before anything else, when claude is on no PATH.
func (r *Repo) Tend(ctx context.Context, name Spec, term Terminal) error {
	if _, err := r.claude.Program(); err != nil {
		return err
	}

	if refused := name.misnamed(); refused != nil {
		return fmt.Errorf("%w: %w", errRefused, refused)
	}

	unlock, err := r.lockTend(name)
	if err != nil {
		return err
	}

	defer unlock()
	t, err := r.tending(ctx, name)
	switch {
	case err != nil:
		return err
	case t.refused != "":
		return fmt.Errorf("%w: %s: %s", errRefused, name, t.refused)
	}

	session := claude.NewID()
	note := filepath.Join(r.claude.Dir(session), noteFile)
	err = r.claude.Interact(claude.Interactive{Session: session, Role: tendRole, System: tendContract,
		Prompt: t.message(r, name, note), Dir: r.top, In: term.In, Out: term.Out, Err: term.Err})
	if err != nil {
		return fmt.Errorf("%s: %w, so kiln goes on with nothing", name, err)
	}

	// The terminal sent each Ctrl-C of the session to kiln too, which ended ctx. A Ctrl-C from now on ends kiln.
	ctx, stop := signal.NotifyContext(context.WithoutCancel(ctx), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return r.goOnTended(ctx, name, note, term.Out)
}

// goOnTended goes on with name once its tend session has exited. It reads where name stands again, since the session
// may have moved it. A name that waits on no repair now is left where it stands, and kiln says where. note is the
// path of the session's note for a stopped build.
func (r *Repo) goOnTended(ctx context.Context, name Spec, note string, out io.Writer) error {
	t, err := r.tending(ctx, name)
	switch {
	case err != nil:
		return err
	case t.refused != "":
		fmt.Fprintf(out, "%s: %s\n", name, t.refused)
		return nil
	case t.rec != nil && t.rec.Accepted:
		fmt.Fprintf(out, "%s: lands again, as kiln accept %s would\n", name, name)
		return r.Accept(ctx, name, out)
	case t.monitor:
		return r.tendMonitor(ctx, name, out)
	}

	return r.tendBuild(ctx, name, note, out)
}

// tendBuild queues the stopped build of name again, with the message owed to its sessions, and runs it in place. The
// message holds note's text, if the session wrote any. While another kiln holds the specs' lane, that kiln goes on
// with the build.
func (r *Repo) tendBuild(ctx context.Context, name Spec, note string, out io.Writer) error {
	noted, err := os.ReadFile(note)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := r.store.update(func(b *book) error { return b.resume(name, string(noted)) }); err != nil {
		return err
	}

	fmt.Fprintf(out, "%s: goes on with its build, in its own sessions, with what you decided\n", name)
	err = r.run(ctx, out, inLane{lane: specsLane, only: name, holder: tendHolder})
	if held, ok := errors.AsType[*laneHeld](err); ok {
		fmt.Fprintf(out, "%s: %s; %s goes on with it\n", name, queued, cmpOr(held.holder, "another kiln"))
		return nil
	}

	return err
}

// tendMonitor runs the monitor name again, as kiln monitor run with its name does. It waits while another kiln holds
// the monitors' lane. Interrupted while it waits, it says what to run, and leaves the monitor as it stands.
func (r *Repo) tendMonitor(ctx context.Context, name Spec, out io.Writer) error {
	fmt.Fprintf(out, "%s: runs the monitor again, as kiln monitor run %s would\n", name, name)
	unlock, err := r.waitLane(ctx, monitorsLane, name, out)
	if ctx.Err() != nil {
		fmt.Fprintf(out, "%s: next: run kiln monitor run %s\n", name, name)
		return nil
	}

	if err != nil {
		return err
	}

	defer unlock()
	return r.monitorRunHeld(ctx, name, out, name)
}

// waitLane takes the lock of the lane l for kiln tend on name. While another kiln holds it, it says so once on out,
// and tries it again each commandsRetry. It answers the context's error once kiln is interrupted.
func (r *Repo) waitLane(ctx context.Context, l lane, name Spec, out io.Writer) (unlock func(), err error) {
	for said := false; ; said = true {
		unlock, err := r.lockLane(l, tendHolder)
		held, isHeld := errors.AsType[*laneHeld](err)
		if !isHeld {
			return unlock, err
		}

		if !said {
			fmt.Fprintf(out, "%s: waits while %s works in the lane of %s\n", name, cmpOr(held.holder, "another kiln"),
				l)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(commandsRetry):
		}
	}
}

// tendLock is the lock the kiln tend of name holds, from before it reads where name stands until it exits.
func tendLock(name Spec) string { return "tend-" + string(name) + ".lock" }

// lockTend takes the lock of kiln tend on name, or refuses at once while another kiln tend holds it.
func (r *Repo) lockTend(name Spec) (unlock func(), err error) {
	unlock, err = r.store.lock(tendLock(name), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return nil, fmt.Errorf("%w: %s: a kiln tend is at work on it", errRefused, name)
	}

	return unlock, err
}

// tended is whether a kiln tend is at work on name. A lock kiln could not read counts as none.
func (r *Repo) tended(name Spec) bool {
	if _, err := os.Stat(filepath.Join(r.store.dir, tendLock(name))); err != nil {
		return false
	}

	unlock, err := r.lockTend(name)
	if err != nil {
		return errors.Is(err, errRefused)
	}

	unlock()
	return false
}

// tendNames answers the specs in the book, and the monitors main's tip holds, that kiln tend takes.
func (r *Repo) tendNames(ctx context.Context) ([]string, error) {
	k, err := r.store.read()
	if err != nil {
		return nil, err
	}

	names, err := r.monitorNames(ctx)
	for _, s := range k.Specs {
		if !slices.Contains(names, string(s.Name)) {
			names = append(names, string(s.Name))
		}
	}

	var taken []string
	for _, name := range names {
		t, err := r.tending(ctx, Spec(name))
		if err != nil {
			return nil, err
		}

		if t.refused == "" {
			taken = append(taken, name)
		}
	}

	return taken, err
}

// tending is where a name stands, as kiln tend reads it.
type tending struct {
	// kind is "The spec" or "The monitor", and state where the name stands, as kiln status words it.
	kind, state string
	// name is the spec's or the monitor's name.
	name Spec
	// why is why it waits: a build's why and what failed, a monitor's summary and report, or kiln's refusal.
	why string
	// rec is the book's record of the name, or nil.
	rec     *record
	monitor bool
	// refused says why kiln tend refuses the name, or is "" when it takes it.
	refused string
}

// tending reads where name stands. The book's record of a spec decides, so a name both a spec's and a monitor's is
// the spec's.
func (r *Repo) tending(ctx context.Context, name Spec) (tending, error) {
	k, err := r.store.read()
	if err != nil {
		return tending{}, err
	}

	rec := k.find(name)
	if rec == nil || rec.Monitor {
		return r.monitorTending(ctx, name, rec)
	}

	why, _ := k.refusal(tendCommand, name, nil)
	t := tending{kind: "The spec", state: shownState(rec), rec: rec, refused: why, why: rec.Why}
	if len(rec.Failing) > 0 {
		t.why = strings.Join(nonEmpty(strings.TrimRight(rec.Why, "\n"), "failing: "+strings.Join(rec.Failing, ", ")),
			"\n")
	}

	return t, nil
}

// monitorTending reads where the monitor name stands. rec is the book's record of its fix, or nil. A monitor kiln
// refuses at main's tip waits on a repair. So does one whose fix stopped, or whose last run waits on one.
func (r *Repo) monitorTending(ctx context.Context, name Spec, rec *record) (tending, error) {
	t := tending{kind: "The monitor", name: name, rec: rec, monitor: true}
	main, _, reads, err := r.mainMonitors(ctx)
	switch {
	case main == "":
		return tending{}, err
	case err != nil:
		return r.unreadMonitor(ctx, t, main, err)
	}

	i := slices.IndexFunc(reads, func(d monitorRead) bool { return d.name == name })
	switch {
	case i >= 0 && reads[i].refused != nil:
		t.state, t.why = foundRefused, reads[i].refused.Error()
		return t, nil
	case i < 0 && rec == nil:
		t.refused = "kiln holds no record of it, and main's tip holds no monitor of that name"
		return t, nil
	}

	entries, err := r.store.entries()
	if err != nil {
		return tending{}, err
	}

	h := histories(entries)[name]
	s := h.standing(monitor{Name: name}, rec)
	t.state, t.why = cmpOr(s.unheld(), tui.NeverRun.String()), strings.Join(nonEmpty(checkSummary(h, s), s.summary,
		s.report), "\n")
	switch {
	case rec != nil:
		t.refused = rec.tendRefusal()
	case !s.waitsOnRepair():
		t.refused = fmt.Sprintf("it is %s, and waits on no repair", t.state)
	}

	return t, nil
}

// unreadMonitor is where the monitor t names stands while kiln could not read the monitors at main, main's tip.
// unread says why. It is often a kiln.json kiln refuses, which then is why the monitor waits on a repair. It answers
// unread for a name that neither main's tip nor the book holds as a monitor.
func (r *Repo) unreadMonitor(ctx context.Context, t tending, main git.Rev, unread error) (tending, error) {
	dirs, err := r.monitorDirs(ctx, main)
	if err != nil {
		return tending{}, err
	}

	if t.rec == nil && !slices.ContainsFunc(dirs, func(d monitorDir) bool { return d.name == t.name }) {
		return tending{}, unread
	}

	t.state, t.why = foundRefused, fmt.Sprintf("kiln could not read the monitors at main's tip: %v", unread)
	return t, nil
}

// checkSummary is the summary of the check that started the fix s names, from the monitor's history h. It is ""
// where s names no fix, or where s.summary is that summary already.
func checkSummary(h *history, s standing) string {
	if !s.fixed || h.lastFinding.Summary == s.summary {
		return ""
	}

	return h.lastFinding.Summary
}

// nonEmpty is each of texts that is not "".
func nonEmpty(texts ...string) []string {
	return slices.DeleteFunc(texts, isEmpty)
}

// message is the tend session's first message on name, as the package doc's "kiln tend" shows it. note is the path
// of the note for a stopped build.
func (t tending) message(r *Repo, name Spec, note string) string {
	lines := []string{fmt.Sprintf("%s %s: %s", t.kind, name, t.state)}
	if t.why != "" {
		lines = append(lines, strings.TrimRight(t.why, "\n"))
	}

	if work := r.workOf(t.rec); work != "" {
		lines = append(lines, "Its work: "+work)
	}

	if sessions := r.sessionsOf(t.rec); sessions != "" {
		lines = append(lines, "Its sessions: "+sessions)
	}

	if t.monitor {
		lines = append(lines, fmt.Sprintf("Its directory: %s/%s/", monitorsDir, name))
	} else if !t.rec.Accepted {
		lines = append(lines, "A note for the build: "+note)
	}

	return strings.Join(lines, "\n") + "\n\n" + tendHere
}

// tendHere follows the lines of a tend session's first message.
const tendHere = "The developer is here, in their own checkout, and ran kiln tend to repair this. Read what you " +
	"need, then ask them what to do."

// workOf says where the work of rec, a stopped record, is: its branch, and the worktree that holds it. It is "" for
// any other record, or while no worktree holds the work.
func (r *Repo) workOf(rec *record) string {
	if rec == nil || rec.State != stopped {
		return ""
	}

	tree := r.tree(rec.Name)
	if _, err := os.Stat(string(tree)); err != nil {
		return ""
	}

	return fmt.Sprintf("%s, in %s", rec.Name.branch(), tree)
}

// sessionsOf names each session rec holds, with the checkout it runs in. It is "" when rec holds none.
func (r *Repo) sessionsOf(rec *record) string {
	switch {
	case rec == nil:
		return ""
	case rec.TestSession != "" && rec.Session == "":
		return fmt.Sprintf("%s %s, in %s", testRole, rec.TestSession, r.clone(rec.Name, testRole))
	case rec.TestSession != "":
		return fmt.Sprintf("%s %s, in %s; %s %s, in %s", testRole, rec.TestSession, r.clone(rec.Name, testRole),
			fixRole, rec.Session, r.tree(rec.Name))
	case rec.Session == "":
		return ""
	case rec.Verify != "":
		return fmt.Sprintf("%s %s, in %s; %s %s, in %s", buildRole, rec.Session, r.clone(rec.Name, buildRole),
			verifyRole, rec.Verify, r.clone(rec.Name, verifyRole))
	case rec.Monitor:
		return fmt.Sprintf("%s %s, in %s", fixRole, rec.Session, r.tree(rec.Name))
	}

	return fmt.Sprintf("%s %s, in %s", buildRole, rec.Session, r.tree(rec.Name))
}

// underAny is whether the path p lies within any of roots, with case folded.
func underAny(p string, roots []string) bool {
	return slices.ContainsFunc(roots, func(root string) bool { return within(fold(p), root) })
}

// owedMessage is the message kiln owes each session of a stopped build that kiln tend saw to. why is why the build
// stopped, and note the text the tend session left for it.
func owedMessage(why, note string) string {
	msg := fmt.Sprintf(owed, strings.TrimSpace(why))
	if note = strings.TrimSpace(note); note != "" {
		msg += fmt.Sprintf(owedNote, note)
	}

	return msg + owedEnd
}

const (
	owed     = "kiln stopped the build: %s\n\nThe developer saw to it, in a session of their own."
	owedNote = " They left you this note:\n\n%s"
	owedEnd  = "\n\nGo on with your work where you stopped, and end your turn: kiln commits what you leave, and " +
		"gates it."
)

// TendLanded records the work of the stopped build of name as landed on main, by commits kiln did not make. It drops
// the build's checkouts and branches, and records the work on main at main's tip, as a landing does. It sends no
// notice, reads no gauge, and no audit reads the work. It refuses all but a spec's stopped build. It refuses while
// main changed no file under the spec's acceptance paths since the build was queued.
func (r *Repo) TendLanded(ctx context.Context, name Spec, out io.Writer) error {
	k, err := r.store.read()
	if err != nil {
		return err
	}

	rec := k.find(name)
	switch {
	case rec == nil:
		return fmt.Errorf("%w: %s: kiln holds no record of it, and kiln tend --landed takes a spec's stopped build",
			errRefused, name)
	case rec.Monitor, rec.State != stopped:
		return fmt.Errorf("%w: %s: it is %s, and kiln tend --landed takes a spec's stopped build", errRefused, name,
			shownState(rec))
	case rec.Accepted:
		return fmt.Errorf("%w: %s: it is %s, which kiln accept %s lands again, or kiln reject %s drops", errRefused,
			name, shownState(rec), name, name)
	}

	main, err := r.mainTip(ctx)
	if err != nil {
		return err
	}

	if err := r.workShown(ctx, *rec, main); err != nil {
		return err
	}

	if err := r.dropCheckouts(ctx, name); err != nil {
		return err
	}

	b := &build{r: r, rec: *rec, tree: r.tree(name), out: out}
	b.set(func(r *record) {
		r.stop, r.Owed, r.ForBuild, r.ForVerify, r.Asked, r.Running = stop{}, "", "", "", false, nil
		r.Before, r.Landing, r.Onto = "", "", ""
	})

	return b.onMain(main)
}

// workShown refuses the work of rec as landed on main, at main, while nothing shows it there. That is while main
// changed no file under the spec's acceptance paths since rec was queued. Those paths are kiln.json's then.
func (r *Repo) workShown(ctx context.Context, rec record, main git.Rev) error {
	c, err := r.config(ctx, rec.Approved)
	if err != nil {
		return err
	}

	changed, err := r.git.Changed(ctx, rec.Approved, main)
	if err != nil {
		return err
	}

	paths := c.acceptancePaths(rec.Name)
	if slices.ContainsFunc(changed, func(p string) bool { return underAny(p, paths) }) {
		return nil
	}

	return fmt.Errorf("%w: %s: main changed nothing under %s since %s, the commit its build was queued from, so "+
		"nothing on main shows its work: commit the work and its acceptance tests on main, then run kiln tend %s "+
		"--landed again; or leave the work to the build, which goes on once the session ends", errRefused,
		rec.Name, strings.Join(paths, ", "), short(rec.Approved), rec.Name)
}
