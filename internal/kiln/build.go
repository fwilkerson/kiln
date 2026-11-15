package kiln

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
)

// unknownReset is how long a usage limit that names no reset pauses a build.
const unknownReset = 5 * time.Minute

// Run builds each queued spec, one at a time, until none is queued. A build that was building when kiln last stopped
// goes on first, in the same session. Run refuses while another kiln run or kiln monitor run builds in the
// repository, and while kiln is being reinstalled. It skips a monitor's fix.
func (r *Repo) Run(ctx context.Context, out io.Writer) error {
	return r.run(ctx, out, "")
}

var errRunning = fmt.Errorf("%w: another kiln builds in this repository", errRefused)

// run builds the queued specs, or only the one named only, if it is not "".
func (r *Repo) run(ctx context.Context, out io.Writer, only Spec) error {
	unlock, err := r.lockRun(runHolder)
	if err != nil {
		return err
	}

	unlock = sync.OnceFunc(unlock)
	defer unlock()
	unshare, err := shareBuilds()
	if err != nil {
		return err
	}

	defer unshare()

	r.recount(ctx, out)
	if err := r.store.update(func(b *book) error { b.requeue(); return nil }); err != nil {
		return err
	}

	return r.buildEach(ctx, out, only, unlock)
}

// buildEach builds each spec take marks building, until none is left.
func (r *Repo) buildEach(ctx context.Context, out io.Writer, only Spec, unlock func()) error {
	for {
		rec, found, err := r.take(only, unlock)
		if err != nil || !found {
			return err
		}

		b := &build{r: r, rec: rec, tree: r.tree(rec.Name), out: out}
		if err := b.settle(ctx, b.run(ctx)); err != nil {
			return err
		}

		b.waits(ctx)
		if err := b.readGauges(ctx); err != nil {
			return err
		}
	}
}

// lockRun takes the run lock, which one kiln run or kiln monitor run in the repository holds at a time. holder
// names which, for a kiln accept that finds it held.
func (r *Repo) lockRun(holder string) (unlock func(), err error) {
	unlock, err = r.store.lock(runLock, syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return nil, errRunning
	}

	if err == nil {
		err = os.WriteFile(filepath.Join(r.store.dir, runLock), []byte(holder), 0o600)
	}

	return unlock, err
}

// The kilns that hold the run lock, as lockRun writes them into it.
const (
	runHolder     = "kiln run"
	monitorHolder = "kiln monitor run"
)

// runHeld is the kiln that holds the run lock, or last held it.
func (r *Repo) runHeld() string {
	held, _ := os.ReadFile(filepath.Join(r.store.dir, runLock))
	return string(held)
}

// take marks the next spec building, as book.next picks it. Once none is left, it unlocks the run.
func (r *Repo) take(only Spec, unlock func()) (rec record, found bool, err error) {
	err = r.store.update(func(b *book) error {
		rec, found = b.next(only, os.Getpid())
		// A landing queued after this update finds the run lock free, so no kiln accept counts on this run. kiln
		// monitor run never unlocks here: it lands queued landings only as it starts.
		if !found {
			unlock()
		}

		return nil
	})

	return rec, found, err
}

// settle stops the build for err, the error its run ended with. It answers the context's error once kiln is
// interrupted, which ends kiln run. A build out of rounds is stopped first, and answers nil even then.
func (b *build) settle(ctx context.Context, err error) error {
	if b.rec.Monitor {
		return b.fixSettle(ctx, err)
	}

	if s, ok := errors.AsType[*outOfRounds](err); ok {
		b.stop(s.why, s.failing)
		return nil
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err != nil {
		b.stop(couldNotGoOn+err.Error(), nil)
	}

	return nil
}

// couldNotGoOn starts the Why of a build stopped for an error of kiln's, not its gate's.
const couldNotGoOn = "kiln could not go on with the build: "

// waits tells the developer when the build ended waiting on them: stopped, waiting for UAT, or waiting on its branch.
// It prints an OSC 9 sequence, which a terminal such as Ghostty shows as a notification, then the build's summary.
// The sequence names the spec, and where it stands as kiln status shows it.
func (b *build) waits(ctx context.Context) {
	k, err := b.r.store.read()
	if err != nil {
		b.say("kiln could not read where it stands: %v", err)
		return
	}

	rec := k.find(b.rec.Name)
	if rec == nil || !slices.Contains([]state{stopped, landed, amended}, rec.State) {
		return
	}

	// The body starts with kiln:, so a spec named 4 can't read as OSC 9;4, a progress bar Ghostty draws.
	fmt.Fprintf(b.out, "\x1b]9;kiln: %s %s\a", rec.Name, shownState(rec))
	for _, line := range b.r.sumUp(ctx, *rec, b.tree, b.landed) {
		fmt.Fprintln(b.out, indent(line))
	}
}

// build is one build of a spec, or the landing of an accepted one.
type build struct {
	r    *Repo
	rec  record
	tree git.Worktree
	out  io.Writer
	// landed is the commit the build moved main to, once it has, and onto the commit main stood at before.
	landed, onto git.Rev
	// side is what each session may write, once read.
	side *sides
	// mu guards rec while a split build's two sessions run at once. set, now, record and say hold it.
	mu sync.Mutex
	// fix is a monitor's fix, or nil for a spec's build.
	fix *fixing
}

// now is the build's record, as its last set left it.
func (b *build) now() record {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rec
}

// outOfRounds is a build that its gate failed past its last round. failing names what failed.
type outOfRounds struct {
	why     string
	failing []string
}

func (s *outOfRounds) Error() string { return s.why }

func (b *build) run(ctx context.Context) error {
	if b.rec.Monitor {
		return b.landFix(ctx)
	}

	s, err := b.sides(ctx)
	if err != nil {
		return err
	}

	if s.split {
		return b.splitRun(ctx)
	}

	if err := b.goOn(ctx); err != nil {
		return err
	}

	return b.untilDone(ctx)
}

// untilDone gates the work, and sends each failure or finding back to the session, until nothing goes back.
func (b *build) untilDone(ctx context.Context) error {
	for {
		next, why, err := b.gateWork(ctx)
		if err != nil || next == "" {
			return err
		}

		l, err := b.launch(ctx, true, next)
		if err != nil {
			return err
		}

		if _, err := b.talk(ctx, l, why); err != nil {
			return err
		}
	}
}

// goOn takes the build up where kiln left it: it starts its session, or resumes one kiln stopped.
func (b *build) goOn(ctx context.Context) error {
	b.ledger(ctx)
	// A landing cut off left the branch where it moved it.
	if err := b.unland(ctx); err != nil {
		return err
	}

	switch {
	case b.rec.Session == "":
		return b.start(ctx)
	case !b.rec.Accepted:
		l, err := b.launch(ctx, true, interrupted)
		if err != nil {
			return err
		}

		_, err = b.talk(ctx, l, reason{Why: whyInterrupted, Round: b.rec.Rounds})
		return err
	}

	return nil
}

// ledger writes the build's first line, unless its session began. A spec approved before kiln kept a ledger has no
// build ID, and its build gets one where it stands.
func (b *build) ledger(ctx context.Context) {
	fresh := b.rec.Build == ""
	if fresh {
		b.set(func(r *record) { r.Build = newBuild() })
	}

	if fresh || b.rec.Session == "" {
		b.begin(ctx)
	}
}

// gateWork commits what the session left, gates it, and takes the build on from the verdict, as after does.
func (b *build) gateWork(ctx context.Context) (string, reason, error) {
	if _, err := b.r.git.Commit(ctx, b.tree, b.message("the session's work")); err != nil {
		return "", reason{}, err
	}

	v, err := b.gate(ctx, false, b.allowed()...)
	if err != nil {
		return "", reason{}, err
	}

	if b.rec.FailingAtMain == nil && len(v.atMain) > 0 {
		b.set(func(r *record) { r.FailingAtMain = v.atMain })
	}

	return b.after(ctx, v)
}

// after takes the build on from a gate's verdict. It answers what to tell the session next, and why. It answers "" once
// the build has landed, or waits on its branch.
func (b *build) after(ctx context.Context, v verdict) (string, reason, error) {
	if !v.passed {
		return b.round(setback{why: whyGate, report: v.report, failing: v.failing})
	}

	if findings, why, err := b.findings(ctx); err != nil || findings != "" {
		return fmt.Sprintf(audited, findings), why, err
	}

	head, err := b.r.git.Head(ctx, b.tree)
	if err != nil {
		return "", reason{}, err
	}

	if waits, err := b.waitsOnBranch(ctx, head); err != nil || waits {
		return "", reason{}, err
	}

	return b.landWork(ctx, head)
}

// findings runs the audit, unless it has answered. It runs it again when the work changed a path kiln.json's reaudit
// names otherwise than the audit last read. That audit counts as a round, and past the last one the build
// stops. It answers the findings, or "" when the audit found none or did not run.
func (b *build) findings(ctx context.Context) (string, reason, error) {
	head, err := b.r.git.Head(ctx, b.tree)
	if err != nil {
		return "", reason{}, err
	}

	why := reason{Why: whyFindings}
	if b.rec.Audited != "" {
		again, err := b.r.reaudits(ctx, b.rec.Audited, head)
		if err != nil || !again {
			return "", reason{}, err
		}

		if why, err = b.reauditRound(); err != nil {
			return "", reason{}, err
		}
	}

	findings, err := b.audit(ctx)
	if err != nil {
		return "", reason{}, err
	}

	b.set(func(r *record) { r.Audited = head })
	if findings == "" {
		return "", reason{}, nil
	}

	return findings, why, nil
}

// reauditRound counts an audit past the first as a round. Past the last round, it stops the build, so no change the
// audit has not read lands.
func (b *build) reauditRound() (reason, error) {
	rounds := b.limit()
	if b.rec.Rounds >= rounds {
		return reason{}, &outOfRounds{why: "the work changed a path the audit must read, after its last round"}
	}

	b.set(func(r *record) { r.Rounds++ })
	b.say("the work changed a path the audit must read; it reads the work again, round %d of %d", b.rec.Rounds,
		rounds)
	return reason{Why: whyFindings, Round: b.rec.Rounds, Rounds: rounds}, nil
}

// limit is the rounds kiln.json gave the build's last gate, or 0 before any gate ran.
func (b *build) limit() int {
	if b.rec.Limit == nil {
		return 0
	}

	return *b.rec.Limit
}

// waitsOnBranch leaves the build on its branch when head holds the spec's amendments, unless the developer accepted
// them. A failed read of the amendments leaves it there too, with the error. It answers whether it left it there.
func (b *build) waitsOnBranch(ctx context.Context, head git.Rev) (bool, error) {
	if b.rec.Accepted {
		return false, nil
	}

	amendments, found, err := b.r.git.Show(ctx, head, b.rec.Name.amendPath())
	if err == nil && !found {
		return false, nil
	}

	b.set(func(r *record) { r.State, r.Amendments = amended, string(amendments) })
	if err == nil {
		b.record(entry{Kind: outcomeKind, Outcome: outWaits, Amendments: string(amendments)})
	}

	b.say("waits on %s, with amendments for UAT", b.rec.Name.branch())
	return true, err
}

// landWork lands the work at head. An accepted spec is retired in the landing. A setback goes back to the session.
func (b *build) landWork(ctx context.Context, head git.Rev) (string, reason, error) {
	var retire func(context.Context, git.Rev) error
	if b.rec.Accepted {
		retire = b.retire
	}

	onMain, back, err := b.land(ctx, head, retire, b.allowed()...)
	switch {
	case err != nil:
		return "", reason{}, err
	case back.report != "":
		return b.round(back)
	}

	if err := b.r.dropCheckouts(ctx, b.rec.Name); err != nil {
		return "", reason{}, err
	}

	return "", reason{}, b.onMain(onMain)
}

// onMain records the build's work on main, at the commit at. An accepted spec is retired, and any other waits for UAT.
func (b *build) onMain(at git.Rev) error {
	if b.rec.Accepted {
		b.record(entry{Kind: outcomeKind, Outcome: outAccepted})
		b.landed = at
		b.say("accepted, and main is at %s", short(at))
		return b.r.store.update(func(k *book) error { k.retire(b.rec.Name); return nil })
	}

	b.record(entry{Kind: outcomeKind, Outcome: outLanded})
	b.set(func(r *record) { r.State = landed })
	b.landed = at
	b.say("landed on main at %s, and waits for UAT", short(at))
	return nil
}

// allowed are the paths under .kiln/ that an accepted spec's landing changes: it folds the amendments, and retires
// the spec. A fix changes none.
func (b *build) allowed() []string {
	if b.rec.Accepted && !b.rec.Monitor {
		return []string{b.rec.Name.file(), b.rec.Name.amendPath()}
	}

	return nil
}

// setback is what kept the work from passing, for the session. It is a failed gate, or a conflict with main. It is
// work that changes nothing there, or a failed gate on the work rebased onto it. why names which, as a start line
// does. failing names what failed: the gate's failures, or the files in conflict.
type setback struct {
	why, report string
	failing     []string
}

// round sends a setback's report back to the session, or stops the build past its last round. It answers the prompt,
// and the round it starts. The rounds it allows are those kiln.json gave the build's last gate.
func (b *build) round(back setback) (string, reason, error) {
	rounds := b.limit()
	if b.rec.Rounds >= rounds {
		return "", reason{}, &outOfRounds{why: "the gate still failed after its last round:\n" + back.report,
			failing: back.failing}
	}

	b.set(func(r *record) { r.Rounds++ })
	b.say("the gate failed; it goes back to the session, round %d of %d", b.rec.Rounds, rounds)
	why := reason{Why: back.why, Round: b.rec.Rounds, Rounds: rounds}
	return fmt.Sprintf(gateFailed, back.report), why, nil
}

func (b *build) start(ctx context.Context) error {
	main, err := b.r.git.Tip(ctx, mainBranch)
	if err != nil {
		return err
	}

	// A branch an earlier build of the spec left would hold work from a spec as it was approved before.
	if err := b.r.git.Remove(ctx, b.tree, b.rec.Name.branch()); err != nil {
		return err
	}

	if err := b.r.git.Checkout(ctx, b.tree, b.rec.Name.branch(), main); err != nil {
		return err
	}

	session := newSession()
	b.set(func(r *record) { r.Session = session })
	b.say("building on %s, in %s", b.rec.Name.branch(), b.tree)
	prompt := fmt.Sprintf(starting, b.rec.Name.file(), short(b.rec.Approved), b.rec.Name.branch())
	l, err := b.launch(ctx, false, prompt)
	if err != nil {
		return err
	}

	_, err = b.talk(ctx, l, reason{Why: whyFirst})
	return err
}

// launch is a run of the build agent's session, which resume says resumes it.
func (b *build) launch(ctx context.Context, resume bool, prompt string) (claude.Launch, error) {
	return b.agentLaunch(ctx, buildRole, claude.Launch{
		Session: b.rec.Session,
		Resume:  resume,
		Prompt:  prompt,
		Dir:     string(b.tree),
		Hook:    b.treeGuard().hookCommand(b.r.exe),
	})
}

// talk runs the session l launches, and answers how it ended. A usage limit pauses the build until it resets, and the
// same session resumes. The ledger gets a line before each run of the session, with why it started, and one after.
//
// The audit changes nothing, so kiln checks. It runs in a worktree of its own. Each audit run's start line names the
// commit it read, and its end line each path the run changed. Then kiln puts that worktree back at the commit read.
func (b *build) talk(ctx context.Context, l claude.Launch, why reason) (claude.Ended, error) {
	for {
		ended, err := b.runOnce(ctx, l, why)
		if err != nil {
			return ended, err
		}

		if ended.Limit == nil {
			return ended, b.heard(ctx, l, ended)
		}

		if b.fix != nil {
			return ended, errLimitEnded
		}

		if err := b.pause(ctx, ended.Limit.Resets); err != nil {
			return ended, err
		}

		l.Resume, l.Prompt, why.Why = true, limitReset, whyLimit
	}
}

// runOnce runs the session l launches once, between its start line and its end line. Once kiln is interrupted, it
// answers the context's error. A session kiln's interrupt ended has not ended on its own, so how it ended says nothing.
func (b *build) runOnce(ctx context.Context, l claude.Launch, why reason) (claude.Ended, error) {
	streamed, err := b.r.claude.Streamed(l.Session)
	if err != nil {
		b.say("kiln could not measure session %s's stream: %v", l.Session, err)
	}

	read, err := b.r.auditRead(ctx, l)
	if err != nil {
		return claude.Ended{}, err
	}

	b.record(entry{Kind: startKind, Role: l.Role, Session: l.Session, Streamed: streamed, PID: os.Getpid(),
		reason: why, Read: read})
	ended, err := b.r.claude.Run(ctx, l)
	end := entry{Kind: endKind, Session: l.Session, Ended: how(ended), Totals: &ended.Totals}
	end.Streamed, _ = b.r.claude.Streamed(l.Session)
	var undone error
	if l.Role == auditRole {
		// An interrupted audit run may have changed its worktree too, and its end line names what it changed.
		end.Changed, undone = b.r.undo(context.WithoutCancel(ctx), git.Worktree(l.Dir), read)
	}

	b.record(end)
	if ctx.Err() != nil {
		return ended, ctx.Err()
	}

	return ended, errors.Join(err, undone)
}

// auditRead is the commit an audit run that l launches reads. It is "" for any other run.
func (r *Repo) auditRead(ctx context.Context, l claude.Launch) (git.Rev, error) {
	if l.Role != auditRole {
		return "", nil
	}

	return r.git.Head(ctx, git.Worktree(l.Dir))
}

// pause pauses the build for a usage limit, until it resets. A limit with no reset pauses it for unknownReset.
func (b *build) pause(ctx context.Context, until time.Time) error {
	if until.IsZero() {
		until = time.Now().Add(unknownReset)
	}

	b.set(func(r *record) { r.Paused = until })
	b.say("paused for a usage limit until %s", until.Local().Format(time.Kitchen))
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Until(until)):
	}

	b.set(func(r *record) { r.Paused = time.Time{} })
	return nil
}

// undo answers each path an audit run changed in its worktree at dir, committed or not, since the commit read. Then
// it puts the worktree back at read, so the next run of the audit sees only the build's work.
func (r *Repo) undo(ctx context.Context, dir git.Worktree, read git.Rev) ([]string, error) {
	head, err := r.git.Head(ctx, dir)
	if err != nil {
		return nil, err
	}

	changed, err := r.git.Status(ctx, dir)
	if err != nil {
		return nil, err
	}

	if head != read {
		committed, err := r.git.Changed(ctx, read, head)
		if err != nil {
			return nil, err
		}

		changed = append(changed, committed...)
	}

	slices.Sort(changed)
	return slices.Compact(changed), r.git.Detached(ctx, dir, read)
}

// set applies change once, to the stored record, and takes that record as the build's copy.
func (b *build) set(change func(*record)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	err := b.r.store.update(func(k *book) error {
		if r := k.find(b.rec.Name); r != nil {
			change(r)
			b.rec = *r
		}

		return nil
	})
	if err != nil {
		change(&b.rec)
		fmt.Fprintf(b.out, "%s: kiln could not record it: %v\n", b.rec.Name, err)
	}
}

// stop records the build as stopped, why, and what failed. It leaves the worktree and the session in place.
func (b *build) stop(why string, failing []string) {
	b.set(func(r *record) { r.State, r.Why, r.Failing, r.Paused = stopped, why, failing, time.Time{} })
	b.record(entry{Kind: outcomeKind, Outcome: outStopped})
	b.say("stopped: %s", strings.SplitN(why, "\n", 2)[0])
}

func (b *build) say(format string, args ...any) {
	fmt.Fprintf(b.out, "%s: %s\n", b.now().Name, fmt.Sprintf(format, args...))
}

func (b *build) message(what string) string {
	if b.rec.Monitor {
		return fmt.Sprintf("fix(%s): %s\n\nkiln fixed it for the monitor %s, from main at %s.\n", b.rec.Name, what,
			b.rec.Name, short(b.rec.Approved))
	}

	return fmt.Sprintf("feat(%s): %s\n\nkiln built %s, approved at %s, in session %s.\n", b.rec.Name, what,
		b.rec.Name.file(), short(b.rec.Approved), b.rec.Session)
}

// landing is the message of the commit that lands the build. It holds the build's time, cost, turns and failed calls,
// so git log shows what each landed build cost.
func (b *build) landing() string {
	what := "land the approved spec"
	if b.rec.Monitor {
		what = "land the monitor's fix"
	}

	return b.message(what) + "\nBuild: " + b.r.summary(b.rec.Build) + "\n"
}

// gate runs the gate on the build's worktree, and writes it to the ledger. landing is the gate on the work rebased
// onto main. A fix's gate keeps it to its side.
func (b *build) gate(ctx context.Context, landing bool, allowed ...string) (verdict, error) {
	return b.gated(ctx, landing, "", b.treeGuard(allowed...))
}

// treeGuard is the guard on the build's worktree, which lets the build change allowed. A fix's keeps it to its side.
// The hook of a session in that worktree and the gate there both take it from here.
func (b *build) treeGuard(allowed ...string) guard {
	g := guard{spec: b.rec.Name, place: b.tree, allowed: allowed}
	if b.fix != nil {
		g.side = b.fix.side
	}

	return g
}

// gated runs the gate as Repo.gate does, and writes it to the ledger. landing is the gate on the work rebased onto
// main.
func (b *build) gated(ctx context.Context, landing bool, without string, guards ...guard) (verdict, error) {
	start := time.Now()
	v, err := b.r.gate(ctx, without, guards...)
	if err == nil {
		v, err = b.gatedCheck(ctx, v)
	}

	b.record(entry{Kind: gateKind, At: start, End: time.Now(), Passed: v.passed, Landing: landing})
	if err == nil {
		b.set(func(r *record) { r.Limit = &v.rounds })
	}

	return v, err
}

// path is the path of elem in the build's worktree.
func (b *build) path(elem ...string) string {
	return filepath.Join(append([]string{string(b.tree)}, elem...)...)
}

func cmpOr[T comparable](a, b T) T {
	var zero T
	if a != zero {
		return a
	}

	return b
}

const (
	starting = "Build %s, approved at %s. Your worktree is on its branch, %s, made from main. When the build " +
		"is done, end your turn: kiln commits what you leave, and gates it."
	gateFailed = "The gate failed, so it comes back to you. Fix what it found, and end your turn.\n\n%s"
	audited    = "The audit read the directories you changed, and your acceptance tests' code beside their Given, " +
		"When, Then. Fix each finding, or say in your reply why it is wrong. Then end your turn.\n\n%s"
	auditing        = "Audit these directories: %s. The spec is %s. The build changed these files: %s."
	auditingHarness = " The build session changed these files of the test harness, which the tests run through: " +
		"%s. Read each for a check it weakened."
	conflicts = "Main moved, and your work conflicts with it in these files:\n%s\n\nMerge main into your " +
		"branch with git merge main, resolve each conflict, commit, and end your turn."
	unchanged   = "Your work changes nothing on main, so it can't land. Build the spec, and end your turn."
	limitReset  = "The usage limit has reset. Go on where you stopped."
	interrupted = "kiln stopped while you worked. Go on where you stopped."
)
