package kiln

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/fwilkerson/kiln/internal/git"
)

// MonitorRun runs, at main's tip, each monitor in .kiln/monitors/ that is due, one at a time. With a name, it runs that
// monitor alone, due or not, and held or not by its last fix. It never starts a second fix while one builds, waits or
// lands. It works in the monitors' lane, beside a kiln fire. It refuses while another kiln monitor run, or a kiln
// accept that lands a fix, holds that lane. It refuses while kiln is being reinstalled. First it drops each fix a kiln
// cut short, and lands each fix's accepted landing that is queued.
func (r *Repo) MonitorRun(ctx context.Context, name Spec, out io.Writer) error {
	unlock, err := r.lockLane(monitorsLane, monitorHolder)
	if err != nil {
		return err
	}

	defer unlock()
	unshare, err := shareBuilds()
	if err != nil {
		return err
	}

	defer unshare()
	if err := r.recover(ctx, out); err != nil {
		return err
	}

	run, monitors, err := r.monitorRun(ctx, name, out)
	if err != nil {
		return err
	}

	for _, m := range monitors {
		if err := run.monitor(ctx, m); err != nil {
			return err
		}
	}

	return nil
}

// monitorRun is one kiln monitor run, as main's tip says. It answers the monitors it runs: each, or the one named
// name.
func (r *Repo) monitorRun(ctx context.Context, name Spec, out io.Writer) (*monitorRunning, []monitor, error) {
	main, err := r.mainTip(ctx)
	if err != nil {
		return nil, nil, err
	}

	c, monitors, err := r.monitors(ctx, main)
	if err != nil {
		return nil, nil, err
	}

	run := &monitorRunning{r: r, out: out, c: c, named: name != ""}
	if name == "" {
		return run, monitors, nil
	}

	m, ok := monitorNamed(monitors, name)
	if !ok {
		return nil, nil, fmt.Errorf("%w: %s at main's tip holds no monitor %s", errRefused, monitorsDir, name)
	}

	return run, []monitor{m}, nil
}

// recover takes the monitors' lane up where kilns left it. It ends their lost runs of fixes, and drops each fix cut
// short that the developer hadn't accepted. Then it lands each fix's accepted landing that is queued. It leaves every
// spec's record as it stands.
func (r *Repo) recover(ctx context.Context, out io.Writer) error {
	r.recount(ctx, out, monitorsLane)
	var lost []record
	err := r.store.update(func(b *book) error {
		b.requeue(monitorsLane)
		lost = b.dropLost(monitorsLane)
		return nil
	})
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

// dropLost ends the build of rec, a fix a kiln cut short, dropped, and removes its branch and checkouts. A fix
// whose landing main holds ends landed instead, opens a tab, and the gauges read it.
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

		r.notify(ctx, rec.Name, out)
		return b.readGauges(ctx)
	}

	r.ended(rec.Name, rec.Build, outDropped, out)
	if err := r.dropCheckouts(ctx, rec.Name); err != nil {
		return err
	}

	fmt.Fprintf(out, "%s: dropped its fix, which a kiln cut short\n", rec.Name)
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

	return run.check(ctx, m, h, k.find(m.Name))
}

// check runs m's check, or reads its gauge, at main's tip, and writes its monitor line. A finding starts a fix,
// unless the monitor is held. h is m's history, and rec its fix's record, if it has one. It answers whether to run m
// again at once: the fix landed, and the check asked to drain.
func (run *monitorRunning) check(ctx context.Context, m monitor, h *history, rec *record) (again bool, err error) {
	main, found, err := run.checkMain(ctx, m)
	if err != nil {
		return false, err
	}

	line := entry{Kind: monitorKind, Monitor: m.Name, At: run.r.now(), Main: main, Entry: m.tree,
		Printed: hashOf(found.printed), Summary: found.summary, Found: foundOf(found), Gauge: found.reading.Gauge,
		Value: found.reading.Value, gaugeLine: found.reading.gaugeLine}
	if line.Found == foundFix && run.held(m, h, rec, line) {
		line.Found = foundHeld
	}

	if line.Found != foundFix {
		return false, run.write(line)
	}

	line.Build = newBuild()
	if err := run.write(line); err != nil {
		return false, err
	}

	landed, err := run.fix(ctx, newFixing(run.c, m, found), line, h)
	return landed && found.drain, err
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

	found, err := held.firstCheck(ctx, m, main)
	return main, found, err
}

// fix runs the fix f that the monitor line started. It opens a tab when the developer should see how it ended. h
// is the monitor's history before this run. It answers whether the fix landed.
func (run *monitorRunning) fix(ctx context.Context, f *fixing, line entry, h *history) (landed bool, err error) {
	b, err := run.r.startFix(ctx, run.out, f, line)
	if b == nil || ctx.Err() != nil {
		return false, err
	}

	if notifies(f.outcome, h) {
		run.r.notify(ctx, line.Monitor, run.out)
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

	said := e.Found
	if e.Found == foundFix {
		said = "found a fix to make"
	}

	if e.Summary != "" {
		said += ": " + e.Summary
	}

	fmt.Fprintf(run.out, "%s: %s\n", e.Monitor, said)
	return nil
}

// notified are the outcomes that open a tab.
var notified = []string{outLanded, outStopped, outWaits, outNeedsYou, outFailed}

// notifies is whether a fix that ended with outcome opens a tab. h is its monitor's history before this run. A
// session that failed as the monitor's last run did opens none. An expired sign-in then opens one tab per monitor.
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

// notify opens a tab that shows how the last run of the monitor name ended. An opener that fails changes no result,
// and kiln says so on out.
func (r *Repo) notify(ctx context.Context, name Spec, out io.Writer) {
	if err := r.open(ctx, name); err != nil {
		fmt.Fprintf(out, "%s: kiln could not open a tab: %v\n", name, err)
	}
}

// shownTime shows t as kiln status does.
func shownTime(t time.Time) string {
	return t.Format("2006-01-02 15:04")
}
