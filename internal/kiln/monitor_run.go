package kiln

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/fwilkerson/kiln/internal/git"
)

// MonitorRun runs, at main's tip, each monitor in .kiln/monitors/ that is due, one at a time. With a name, it runs that
// monitor alone, due or not, and held or not by its last finding. It never starts a second fix while one builds, waits
// or lands. It works in the monitors' lane, beside a kiln fire. It refuses while another kiln monitor run, or a kiln
// accept that lands a fix, holds that lane. It refuses while kiln is being reinstalled. First it drops each fix a kiln
// cut short, and lands each fix's accepted landing that is queued. It skips a monitor while a kiln tend is at work on
// it. A run that finds nothing to fix drops the monitor's stopped fix, unless the developer accepted it.
func (r *Repo) MonitorRun(ctx context.Context, name Spec, out io.Writer) error {
	unlock, err := r.lockLane(monitorsLane, monitorHolder)
	if err != nil {
		return err
	}

	defer unlock()
	return r.monitorRunHeld(ctx, name, out, "")
}

// monitorRunHeld is kiln monitor run, for every monitor or the one named name, once its caller holds the monitors'
// lane. tending names the monitor the caller's kiln tend is at work on, which the run does not skip.
func (r *Repo) monitorRunHeld(ctx context.Context, name Spec, out io.Writer, tending Spec) error {
	unshare, err := shareBuilds()
	if err != nil {
		return err
	}

	defer unshare()
	if err := r.recover(ctx, out); err != nil {
		return err
	}

	run, reads, err := r.monitorRun(ctx, name, out)
	if err != nil {
		return err
	}

	run.tending = tending
	for _, d := range reads {
		if err := run.read(ctx, d); err != nil {
			return err
		}
	}

	return nil
}

// monitorRun is one kiln monitor run, as main's tip says. It answers what it runs: each monitor, or the one named
// name. It refuses a name main's tip holds no monitor of, and one kiln refuses, with why.
func (r *Repo) monitorRun(ctx context.Context, name Spec, out io.Writer) (*monitorRunning, []monitorRead, error) {
	_, c, reads, err := r.mainMonitors(ctx)
	if err != nil {
		return nil, nil, err
	}

	run := &monitorRunning{r: r, out: out, c: c, named: name != ""}
	if name == "" {
		return run, reads, nil
	}

	m, found, err := monitorNamed(reads, name)
	switch {
	case err != nil:
		return nil, nil, err
	case !found:
		return nil, nil, fmt.Errorf("%w: %s at main's tip holds no monitor %s", errRefused, monitorsDir, name)
	}

	return run, []monitorRead{{name: name, monitor: m}}, nil
}

// read runs the monitor d holds, as monitor does, or says why kiln refuses it, as refused does. It skips a monitor
// while another kiln tend is at work on it. So no run drops or replaces a stopped fix that session reads.
func (run *monitorRunning) read(ctx context.Context, d monitorRead) error {
	switch {
	case d.name != run.tending && run.r.tended(d.name):
		fmt.Fprintf(run.out, "%s: skipped, since a kiln tend is at work on it\n", d.name)
		return nil
	case d.refused != nil:
		return run.refused(ctx, d)
	}

	return run.monitor(ctx, d.monitor)
}

// refused says why kiln refuses d, which it never runs. It writes d's monitor line, and notifies, unless the last run
// refused d's directory as it stands. Then it writes nothing.
func (run *monitorRunning) refused(ctx context.Context, d monitorRead) error {
	main, err := run.r.mainTip(ctx)
	if err != nil {
		return err
	}

	entries, err := run.r.store.entries()
	if err != nil {
		return err
	}

	line := entry{Kind: monitorKind, Monitor: d.name, At: run.r.now(), Main: main, Entry: d.tree, Found: foundRefused,
		Summary: d.refused.Error()}
	if h := histories(entries)[d.name]; h.lastResult() == foundRefused && h.last.Entry == d.tree {
		run.say(line)
		return nil
	}

	if err := run.write(line); err != nil {
		return err
	}

	run.r.notifyFix(ctx, d.name, "", run.out)
	return nil
}

// recover takes the monitors' lane up where kilns left it. It ends their lost runs of fixes, and drops each fix cut
// short that the developer hadn't accepted. Then it lands each fix's accepted landing that is queued. It leaves every
// spec's record as it stands.
func (r *Repo) recover(ctx context.Context, out io.Writer) error {
	lost, err := r.takeUp(ctx, out, monitorsLane)
	if err != nil {
		return err
	}

	for _, rec := range lost {
		if err := r.dropLost(ctx, rec, out); err != nil {
			return err
		}
	}

	return r.buildEach(ctx, out, inLane{lane: monitorsLane, only: landings, unlock: func() {}})
}

// dropLost ends the build of rec, a fix a kiln cut short, dropped, as end ends it. A fix whose landing main holds
// ends landed instead, notifies, and the gauges read it.
func (r *Repo) dropLost(ctx context.Context, rec record, out io.Writer) error {
	b := &build{r: r, rec: rec, tree: r.tree(rec.Name), out: out}
	held, err := b.landedBefore(ctx)
	if err != nil {
		return err
	}

	if held {
		if err := b.fixEnded(ctx); err != nil {
			return err
		}

		r.notifyFix(ctx, rec.Name, b.landed, out)
		return b.readGauges(ctx)
	}

	if err := r.end(ctx, rec, entry{Outcome: outDropped}); err != nil {
		return err
	}

	fmt.Fprintf(out, "%s: dropped its fix, which a kiln cut short\n", rec.Name)
	return nil
}

// dropStopped drops rec, the monitor's stopped fix the developer had not accepted, with its branch and checkouts. A
// run that found nothing to fix calls it, while it holds the monitors' lane.
func (r *Repo) dropStopped(ctx context.Context, rec record, out io.Writer) error {
	if err := r.end(ctx, rec, entry{Outcome: outDropped}); err != nil {
		return err
	}

	fmt.Fprintf(out, "%s: dropped its stopped fix, since the monitor finds nothing to fix\n", rec.Name)
	return nil
}

// monitorRunning is what one kiln monitor run runs its monitors with.
type monitorRunning struct {
	r   *Repo
	out io.Writer
	// c is kiln.json at main's tip when the run started.
	c config
	// named is a run of one monitor by its name, which runs due or not.
	named bool
	// tending is the monitor the kiln tend that started the run is at work on, or "".
	tending Spec
}

// monitor runs m, if it is due or named. It runs again, due or not, after each fix that landed for a check that asked
// to drain. So one run empties a queue that each fix takes one item from.
func (run *monitorRunning) monitor(ctx context.Context, m monitor) error {
	again, err := run.pass(ctx, m, run.named)
	for again {
		again, err = run.pass(ctx, m, true)
	}

	return err
}

// pass runs m's check once, if m is due or dueOrNot, and records what it found. A finding starts a fix, unless the
// monitor is held. It answers whether to run m again at once, which it never does when err is not nil.
func (run *monitorRunning) pass(ctx context.Context, m monitor, dueOrNot bool) (again bool, err error) {
	r := run.r
	k, err := r.store.read()
	if err != nil {
		return false, err
	}

	if rec := k.find(m.Name); rec != nil && !rec.Monitor {
		fmt.Fprintf(run.out, "%s: refused: kiln holds a spec of that name, and one name is never both\n", m.Name)
		return false, nil
	}

	entries, err := r.store.entries()
	if err != nil {
		return false, err
	}

	h := histories(entries)[m.Name]
	if !dueOrNot && !h.due(m, r.now()) {
		fmt.Fprintf(run.out, "%s: not due until %s\n", m.Name, shownTime(h.next(m)))
		return false, nil
	}

	if held, err := run.heldStuck(ctx, m, h, dueOrNot); err != nil || held {
		return false, err
	}

	if skipped, err := run.skipped(ctx, m, dueOrNot); err != nil || skipped {
		return false, err
	}

	return run.check(ctx, m, h, k.find(m.Name))
}

// heldStuck holds m while main's tip and m's directory stay as they were when m's check or gauge timed out. It
// writes m's monitor line then. A run due or not, as one by name, holds none. It answers whether it held m.
func (run *monitorRunning) heldStuck(ctx context.Context, m monitor, h *history, dueOrNot bool) (bool, error) {
	if dueOrNot {
		return false, nil
	}

	main, err := run.r.mainTip(ctx)
	if err != nil {
		return false, err
	}

	line := entry{Kind: monitorKind, Monitor: m.Name, At: run.r.now(), Main: main, Entry: m.tree, Found: foundHeld,
		Past: true}
	if !h.stuckAt(line) {
		return false, nil
	}

	line.Summary = fmt.Sprintf("its check or gauge ran past %s at main %s; it runs once main's tip, or any file "+
		"in the monitor's directory, changes", timeoutKey, short(main))
	return true, run.write(line)
}

// skipped skips m while kiln has no connection to Claude's API, and writes m's monitor line then. kiln asks only for
// a monitor with an agent, in a run that is due. A run due or not, as one by name, never asks. It answers whether it
// skipped m.
func (run *monitorRunning) skipped(ctx context.Context, m monitor, dueOrNot bool) (bool, error) {
	if dueOrNot || !m.agent {
		return false, nil
	}

	at := run.r.now()
	online, host := run.r.online(ctx)
	switch {
	case ctx.Err() != nil:
		return false, ctx.Err()
	case online:
		return false, nil
	}

	main, err := run.r.mainTip(ctx)
	if err != nil {
		return false, err
	}

	return true, run.write(entry{Kind: monitorKind, Monitor: m.Name, At: at, Main: main, Found: foundSkipped,
		Summary: "no connection to " + host})
}

// check runs m's check, or reads its gauge, at main's tip, and writes its monitor line. A finding starts a fix,
// unless the monitor is held. In a monitor that fixes nothing, it needs the developer, and notifies. h is m's history,
// and rec its fix's record, if it has one. It answers whether to run m again at once: the fix landed, and the check
// asked to drain.
func (run *monitorRunning) check(ctx context.Context, m monitor, h *history, rec *record) (again bool, err error) {
	main, found, err := run.checkMain(ctx, m)
	if err != nil {
		return false, err
	}

	line := entry{Kind: monitorKind, Monitor: m.Name, At: run.r.now(), Main: main, Entry: m.tree,
		Printed: hashOf(found.printed), Summary: found.summary, Found: foundOf(found), Gauge: found.reading.Gauge,
		Value: found.reading.Value, gaugeLine: found.reading.gaugeLine, Past: found.past}
	switch {
	case line.Found != foundFix:
	case run.held(m, h, rec, line):
		line.Found = foundHeld
	case !m.fixes():
		line.Found = outNeedsYou
	}

	if line.Found != foundFix {
		return false, run.noFix(ctx, line, rec)
	}

	line.Build = newBuild()
	landed, err := run.fix(ctx, newFixing(run.c, m, found), line, h)
	return landed && found.drain, err
}

// noFix writes the monitor line of a run that starts no fix. A watching monitor that needs the developer notifies. A
// healthy run drops the monitor's stopped fix, rec, unless the developer accepted it.
func (run *monitorRunning) noFix(ctx context.Context, line entry, rec *record) error {
	// kiln status shows the healthy line over the fix, so a kiln that dies mid-drop must not have written it.
	if line.Found == foundHealthy && rec.unacceptedStop() {
		if err := run.r.dropStopped(ctx, *rec, run.out); err != nil {
			return err
		}
	}

	if err := run.write(line); err != nil {
		return err
	}

	if line.Found == outNeedsYou {
		run.r.notifyFix(ctx, line.Monitor, "", run.out)
	}

	return nil
}

// checkMain runs m's check, or reads its gauge, at main's tip, and answers that tip and what it found. It holds
// commandsLock from before it reads the tip, so the other lane moves no main under the check.
func (run *monitorRunning) checkMain(ctx context.Context, m monitor) (git.Rev, checked, error) {
	held, err := run.r.lockCommands(ctx, m.Name, run.out)
	if err != nil {
		return "", checked{}, err
	}

	defer held.release()
	main, err := run.r.mainTip(ctx)
	if err != nil {
		return "", checked{}, err
	}

	found, err := held.firstCheck(ctx, run.c, m, main)
	return main, found, err
}

// fix runs the fix f that the monitor line started. It notifies when the developer should see how it ended. h
// is the monitor's history before this run. It answers whether the fix landed.
func (run *monitorRunning) fix(ctx context.Context, f *fixing, line entry, h *history) (landed bool, err error) {
	b, err := run.r.startFix(run.out, f, line)
	if err != nil {
		return false, err
	}

	run.say(line)
	err = b.fixRun(ctx)
	if ctx.Err() != nil {
		return false, err
	}

	if notifies(f.outcome, h) {
		run.r.notifyFix(ctx, line.Monitor, b.landed, run.out)
	}

	err = errors.Join(err, b.readGauges(ctx))
	return err == nil && f.outcome == outLanded, err
}

// foundOf is what a check found: couldn't run, healthy, or a fix to make.
func foundOf(c checked) string {
	switch {
	case !c.ran:
		return foundCouldNot
	case !c.fix:
		return foundHealthy
	}

	return foundFix
}

// held is whether m, whose check asks for a fix, is held. h is its history, rec its fix's record, and line its
// monitor line. A run by name ignores the hold, but never starts a second fix.
func (run *monitorRunning) held(m monitor, h *history, rec *record, line entry) bool {
	return fixHeld(rec) || !run.named && h.holds(m, line) != ""
}

// fixHeld is whether rec, the record of a monitor's fix, holds the monitor. One fix of a monitor builds, waits or
// lands at a time. Only a stopped fix gives way to a new one.
func fixHeld(rec *record) bool {
	if rec == nil {
		return false
	}

	return rec.State != stopped || rec.Accepted
}

// write writes the monitor line e to the ledger, and says what the run found.
func (run *monitorRunning) write(e entry) error {
	if err := run.r.store.write(e); err != nil {
		return err
	}

	run.say(e)
	return nil
}

// say says what the run of the monitor line e found.
func (run *monitorRunning) say(e entry) {
	said := e.Found
	if e.Found == foundFix {
		said = "found a fix to make"
	}

	if e.Summary != "" {
		said += ": " + e.Summary
	}

	fmt.Fprintf(run.out, "%s: %s\n", e.Monitor, said)
}

// shownTime shows t as kiln status does.
func shownTime(t time.Time) string {
	return t.Format("2006-01-02 15:04")
}
