package kiln

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/proc"
)

// unknownReset is how long a usage limit that names no reset pauses a build.
const unknownReset = 5 * time.Minute

// Run builds each queued spec, one at a time, until none is queued. A build that was building when kiln last stopped
// goes on first, in the same session. Run refuses while another kiln run builds in the repository, and while kiln
// is being reinstalled.
func (r *Repo) Run(ctx context.Context, out io.Writer) error {
	return r.run(ctx, out, "")
}

var errRunning = fmt.Errorf("%w: another kiln run is building in this repository", errRefused)

// run builds the queued specs, or only the one named only, if it is not "".
func (r *Repo) run(ctx context.Context, out io.Writer, only Spec) error {
	unlock, err := r.lockRun()
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
	}
}

// lockRun takes the run lock, which one kiln run in the repository holds at a time.
func (r *Repo) lockRun() (unlock func(), err error) {
	unlock, err = r.store.lock(runLock, syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return nil, errRunning
	}

	return unlock, err
}

// take marks the next spec building, as book.next picks it. Once none is left, it unlocks the run.
func (r *Repo) take(only Spec, unlock func()) (rec record, found bool, err error) {
	err = r.store.update(func(b *book) error {
		rec, found = b.next(only, os.Getpid())
		// A landing queued after this update finds the run lock free, so no kiln accept counts on this run.
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
	// landed is the commit the build moved main to, once it has.
	landed git.Rev
}

// outOfRounds is a build that its gate failed past its last round. failing names what failed.
type outOfRounds struct {
	why     string
	failing []string
}

func (s *outOfRounds) Error() string { return s.why }

func (b *build) run(ctx context.Context) error {
	if err := b.goOn(ctx); err != nil {
		return err
	}

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

	if prompt, why, err := b.findings(ctx); err != nil || prompt != "" {
		return prompt, why, err
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

// findings runs the audit, unless it has answered. It answers the prompt that sends its findings to the session, or ""
// when it found none.
func (b *build) findings(ctx context.Context) (string, reason, error) {
	if b.rec.Audited {
		return "", reason{}, nil
	}

	findings, err := b.audit(ctx)
	if err != nil {
		return "", reason{}, err
	}

	b.set(func(r *record) { r.Audited = true })
	if findings == "" {
		return "", reason{}, nil
	}

	return fmt.Sprintf(audited, findings), reason{Why: whyFindings}, nil
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

	if err := b.r.git.Remove(ctx, b.tree, b.rec.Name.branch()); err != nil {
		return "", reason{}, err
	}

	return "", reason{}, b.onMain(onMain)
}

// onMain records the build's work on main, at the commit at. An accepted spec is retired, and any other waits for UAT.
func (b *build) onMain(at git.Rev) error {
	if b.rec.Accepted {
		b.record(entry{Kind: outcomeKind, Outcome: outAccepted})
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
// the spec.
func (b *build) allowed() []string {
	if b.rec.Accepted {
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
	rounds := 0
	if b.rec.Limit != nil {
		rounds = *b.rec.Limit
	}

	if b.rec.Rounds >= rounds {
		return "", reason{}, &outOfRounds{why: "the gate still failed after its last round:\n" + back.report,
			failing: back.failing}
	}

	b.set(func(r *record) { r.Rounds++ })
	b.say("the gate failed; it goes back to the session, round %d of %d", b.rec.Rounds, rounds)
	why := reason{Why: back.why, Round: b.rec.Rounds, Rounds: rounds}
	return fmt.Sprintf(gateFailed, b.rec.Rounds, rounds, back.report), why, nil
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
	name := b.rec.Name
	hook := fmt.Sprintf("%s hook --spec %s --place %s", shellQuote(b.r.exe), name, shellQuote(string(b.tree)))
	return b.agentLaunch(ctx, buildRole, claude.Launch{
		Session: b.rec.Session,
		Resume:  resume,
		Prompt:  prompt,
		Dir:     string(b.tree),
		Hook:    hook,
	})
}

// sandboxed adds to l what every session of a build holds to. No agent's file changes the denied commands and
// forbidden actions. The hosts and writable paths are kiln.json's, at main's tip.
func (b *build) sandboxed(ctx context.Context, l claude.Launch) (claude.Launch, error) {
	l.Deny = []string{"Bash(git push)", "Bash(git push *)", "Bash(git update-ref *)", "Bash(git branch -f *)",
		"Bash(git branch -D *)", "Bash(git checkout main)", "Bash(git switch main)", "Bash(git reset * main)"}
	l.Forbidden = []string{
		"Pushing to any remote, in any form.",
		"Writing, moving, resetting or deleting any ref but the branch checked out in the working directory, " +
			"and above all main.",
		fmt.Sprintf("Changing anything under .kiln/ but %s, by a file tool or a shell command.",
			b.rec.Name.amendPath()),
		"Changing .claude/settings.json or .claude/settings.local.json.",
	}
	var err error
	l.Domains, l.Writable, err = b.r.reach(ctx)
	return l, err
}

// reach answers the hosts a session's commands reach, and the paths beside its worktree they write. kiln.json at
// main's tip names them.
func (r *Repo) reach(ctx context.Context) (hosts, writable []string, err error) {
	main, err := r.git.Tip(ctx, mainBranch)
	if err != nil {
		return nil, nil, err
	}

	c, err := r.config(ctx, main)
	if err != nil {
		return nil, nil, err
	}

	for _, command := range c.Writable {
		paths, err := r.printed(ctx, command)
		if err != nil {
			return nil, nil, err
		}

		writable = append(writable, paths...)
	}

	return c.Hosts, writable, nil
}

// printed answers the paths command prints, a field each. It runs command once per kiln command, in the checkout
// kiln runs in, with sh -c. A command that fails is an error.
func (r *Repo) printed(ctx context.Context, command string) ([]string, error) {
	r.writableMu.Lock()
	defer r.writableMu.Unlock()
	if paths, ok := r.writable[command]; ok {
		return paths, nil
	}

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Dir, cmd.Env = r.top, proc.Shell.Env(r.env)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s's writable command %q: %w", configFile, command, err)
	}

	if r.writable == nil {
		r.writable = map[string][]string{}
	}

	r.writable[command] = strings.Fields(string(out))
	return r.writable[command], nil
}

// talk runs the session l launches, and answers how it ended. A usage limit pauses the build until it resets, and the
// same session resumes. The ledger gets a line before each run of the session, with why it started, and one after.
//
// The audit changes nothing, so kiln checks. It runs in a worktree of its own. Each audit run's start line names the
// commit it read, and its end line each path the run changed. Then kiln puts that worktree back at the commit read.
func (b *build) talk(ctx context.Context, l claude.Launch, why reason) (claude.Ended, error) {
	for {
		ended, err := b.runOnce(ctx, l, why)
		if err != nil || ended.Limit == nil {
			return ended, err
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

var tally = regexp.MustCompile(`found (\d+)\s*$`)

// audit runs the audit agent on the directories the build changed, and its acceptance tests. It answers the findings,
// or "" for none. An audit that names no finding count is all findings. An audit that ends with no result runs again,
// in a new session, up to auditTries times.
func (b *build) audit(ctx context.Context) (string, error) {
	head, dirs, files, err := b.changedFiles(ctx)
	if err != nil || len(dirs) == 0 {
		return "", err
	}

	b.say("the audit reads %s", strings.Join(dirs, " "))
	// The audit's own worktree keeps what it writes out of the build's, even when kiln stops mid-audit.
	l, err := b.agentLaunch(ctx, auditRole, claude.Launch{
		Prompt: fmt.Sprintf(auditing, "`"+strings.Join(dirs, "`, `")+"`", b.rec.Name.file(), strings.Join(files, ", ")),
		Dir:    string(b.r.auditTree(b.rec.Name)),
	})
	if err != nil {
		return "", err
	}

	for try := 1; ; try++ {
		findings, err := b.auditTry(ctx, l, head, try)
		if !errors.Is(err, errNoResult) {
			return findings, err
		}

		b.say("the audit ended with no result; it runs again")
	}
}

// errNoResult is an audit run that ended with no result, with tries left.
var errNoResult = errors.New("the audit ended with no result")

// auditTry runs the audit l launches, in a new session, on the build's commit head. It answers its findings, or
// errNoResult for a try that ended with no result before the last.
func (b *build) auditTry(ctx context.Context, l claude.Launch, head git.Rev, try int) (string, error) {
	if err := b.r.git.Detached(ctx, git.Worktree(l.Dir), head); err != nil {
		return "", err
	}

	l.Session = newSession()
	ended, err := b.talk(ctx, l, reason{Why: whyAudit, Try: try})
	switch {
	case err != nil:
		return "", err
	case !ended.Failed:
		return findingsOf(ended.Text), nil
	case try < auditTries:
		return "", errNoResult
	}

	return "", fmt.Errorf("the audit ended with no result %d times", auditTries)
}

// findingsOf is the findings an audit's text names, or "" for none. A text that names no finding count is all
// findings.
func findingsOf(text string) string {
	text = strings.TrimSpace(text)
	if m := tally.FindStringSubmatch(text); m != nil && m[1] == "0" {
		return ""
	}

	return text
}

// changedFiles answers the build's commit, and the files its work changed since it left main, outside .kiln/. It
// answers their directories too, as changedDirs names them.
func (b *build) changedFiles(ctx context.Context) (head git.Rev, dirs, files []string, err error) {
	main, err := b.r.git.Tip(ctx, mainBranch)
	if err != nil {
		return "", nil, nil, err
	}

	head, err = b.r.git.Head(ctx, b.tree)
	if err != nil {
		return "", nil, nil, err
	}

	base, err := b.r.git.MergeBase(ctx, main, head)
	if err != nil {
		return "", nil, nil, err
	}

	changed, err := b.r.git.Changed(ctx, base, head)
	if err != nil {
		return "", nil, nil, err
	}

	files = slices.DeleteFunc(changed, func(p string) bool { return strings.HasPrefix(p, kilnDir+"/") })
	dirs, err = b.changedDirs(ctx, head, files)
	return head, dirs, files, err
}

// changedDirs are the directories at head that hold paths, sorted, each once: . for the top, or the directory's path.
// A directory the work emptied gives way to the nearest one above it that head holds. A deletion is audited too.
// It names no file extension, so it serves a project in any language.
func (b *build) changedDirs(ctx context.Context, head git.Rev, paths []string) ([]string, error) {
	var dirs []string
	for _, p := range paths {
		dir, err := b.heldDir(ctx, head, path.Dir(p))
		if err != nil {
			return nil, err
		}

		if !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}

	slices.Sort(dirs)
	return dirs, nil
}

// heldDir is dir, or the nearest directory above it, that holds a file at head. The top always counts.
func (b *build) heldDir(ctx context.Context, head git.Rev, dir string) (string, error) {
	for ; dir != "."; dir = path.Dir(dir) {
		files, err := b.r.git.Files(ctx, head, dir)
		if err != nil || len(files) > 0 {
			return dir, err
		}
	}

	return ".", nil
}

const auditTries = 3

// land rebases the build's work onto main as one commit, and gates it. Then it moves main
// there, only if main has not moved since the rebase; if it has, land starts again. from is the commit to rebase,
// or "" to start from main's tip. Before the gate, retire may add a commit. A path in allowed may change, though
// guarded names it.
//
// It answers the commit main moved to. Or it answers what kept the work from landing, for the session. That is a
// conflict, work that changes nothing on main, or the gate. A landing that doesn't move main puts the branch back
// where it stood, even after an error or an interrupt. Should kiln die mid-landing, the record keeps that commit,
// and the next kiln run puts the branch back.
func (b *build) land(ctx context.Context, from git.Rev, retire func(ctx context.Context, held git.Rev) error,
	allowed ...string) (landed git.Rev, back setback, err error) {
	before, err := b.r.git.Head(ctx, b.tree)
	if err != nil {
		return "", setback{}, err
	}

	b.set(func(r *record) { r.Before = before })
	defer func() {
		if landed == "" {
			err = errors.Join(err, b.unland(context.WithoutCancel(ctx)))
		} else {
			b.set(func(r *record) { r.Before = "" })
		}
	}()

	for {
		landed, back, err = b.landOnce(ctx, from, retire, allowed)
		if !errors.Is(err, errMainMoved) {
			return landed, back, err
		}

		b.say("main moved while the gate ran; the work is rebased onto it again")
	}
}

// errMainMoved is a landing that main's move refused, since main moved while the gate ran.
var errMainMoved = errors.New("main moved while the gate ran")

// landOnce tries the landing once, onto main's tip. It answers errMainMoved when main moved meanwhile.
func (b *build) landOnce(ctx context.Context, from git.Rev, retire func(ctx context.Context, held git.Rev) error,
	allowed []string) (git.Rev, setback, error) {
	onto, err := b.r.git.Tip(ctx, mainBranch)
	if err != nil {
		return "", setback{}, err
	}

	head, back, err := b.prepare(ctx, from, onto, retire)
	if err != nil || back.why != "" {
		return "", back, err
	}

	v, err := b.gate(ctx, true, allowed...)
	if err != nil || !v.passed {
		return "", setback{whyLanding, v.report, v.failing}, err
	}

	return b.move(ctx, head, onto)
}

// prepare puts the work from from onto onto, and retires the spec if retire is set. It
// answers the commit that would land. Or it answers what kept the work from landing: a conflict, or no change.
func (b *build) prepare(ctx context.Context, from, onto git.Rev, retire func(ctx context.Context, held git.Rev) error,
) (git.Rev, setback, error) {
	start := cmpOr(from, onto)
	if err := b.r.git.Reset(ctx, b.tree, start); err != nil {
		return "", setback{}, err
	}

	if back, err := b.rebase(ctx, start, onto); err != nil || back.why != "" {
		return "", back, err
	}

	if retire == nil {
		return b.changes(ctx, onto)
	}

	held, err := b.r.git.Head(ctx, b.tree)
	if err != nil {
		return "", setback{}, err
	}

	if err := retire(ctx, held); err != nil {
		return "", setback{}, err
	}

	return b.changes(ctx, onto)
}

// rebase rebases the work at start onto onto, as the one commit that lands, unless start is onto already.
func (b *build) rebase(ctx context.Context, start, onto git.Rev) (setback, error) {
	if start == onto {
		return setback{}, nil
	}

	_, conflict, err := b.r.git.Rebase(ctx, b.tree, onto, b.landing())
	if err != nil || conflict == nil {
		return setback{}, err
	}

	report := fmt.Sprintf(conflicts, strings.Join(conflict.Files, "\n"))
	return setback{whyConflict, report, conflict.Files}, nil
}

// changes answers the commit at HEAD, unless it changes nothing since onto. A session that ended with no result may
// leave nothing, and main would "move" to the commit it holds.
func (b *build) changes(ctx context.Context, onto git.Rev) (git.Rev, setback, error) {
	head, err := b.r.git.Head(ctx, b.tree)
	if err != nil {
		return "", setback{}, err
	}

	changed, err := b.r.git.Changed(ctx, onto, head)
	switch {
	case err != nil:
		return "", setback{}, err
	case len(changed) == 0:
		return "", setback{why: whyUnchanged, report: unchanged}, nil
	}

	return head, setback{}, nil
}

// move moves main to head, only if main is still at onto. It answers errMainMoved if main moved.
func (b *build) move(ctx context.Context, head, onto git.Rev) (git.Rev, setback, error) {
	way, err := b.r.git.Move(ctx, mainBranch, head, onto)
	switch {
	case err != nil:
		return "", setback{}, err
	case way == nil:
		return head, setback{}, nil
	case way.Tip != "":
		return "", setback{}, errMainMoved
	}

	return "", setback{}, fmt.Errorf("main's checkout is in the way of the landing: busy in %q, or with changes to %s",
		way.Busy, strings.Join(way.Files, ", "))
}

// unland puts the branch back at the commit it stood at before its landing began, if a landing began.
func (b *build) unland(ctx context.Context) error {
	if b.rec.Before == "" {
		return nil
	}

	if err := b.r.git.Reset(ctx, b.tree, b.rec.Before); err != nil {
		return err
	}

	b.set(func(r *record) { r.Before = "" })
	return nil
}

// foldAmendments commits, on the spec's branch, the spec with its amendments folded in, and the amendment file
// removed. It answers that commit.
func (b *build) foldAmendments(ctx context.Context) (git.Rev, error) {
	name := b.rec.Name
	head, err := b.r.git.Head(ctx, b.tree)
	if err != nil {
		return "", err
	}

	spec, _, err := b.r.git.Show(ctx, head, name.file())
	if err != nil {
		return "", err
	}

	amendments, _, err := b.r.git.Show(ctx, head, name.amendPath())
	if err != nil {
		return "", err
	}

	folded, err := foldAmendments(string(spec), string(amendments))
	if err != nil {
		return "", fmt.Errorf("%w: %s cannot fold into %s: %w", errRefused, name.amendPath(), name.file(), err)
	}

	if err := os.WriteFile(b.path(name.file()), []byte(folded), 0o644); err != nil {
		return "", err
	}

	if err := os.Remove(b.path(name.amendPath())); err != nil {
		return "", err
	}

	return b.r.git.Commit(ctx, b.tree, fmt.Sprintf("docs(%s): fold the amendments into the spec\n", name))
}

func (b *build) checkoutMain(ctx context.Context) error {
	main, err := b.r.git.Tip(ctx, mainBranch)
	if err != nil {
		return err
	}

	if err := b.r.git.Remove(ctx, b.tree, b.rec.Name.branch()); err != nil {
		return err
	}

	return b.r.git.Checkout(ctx, b.tree, b.rec.Name.branch(), main)
}

// retire deletes the spec and its amendments, and adds its line to PRODUCT.md. The line holds the command that shows
// the spec at held, the commit that last held it.
func (b *build) retire(ctx context.Context, held git.Rev) error {
	name := b.rec.Name
	title, err := b.title()
	if err != nil {
		return err
	}

	if err := b.addProduct(fmt.Sprintf("- %s: `git show %s:%s`\n", title, short(held), name.file())); err != nil {
		return err
	}

	for _, p := range []string{name.file(), name.amendPath()} {
		if err := os.Remove(b.path(p)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}

	_, err = b.r.git.Commit(ctx, b.tree, fmt.Sprintf("docs(%s): retire the accepted spec\n", name))
	return err
}

// title is the spec's title, from its first line, or its name if that line is no title.
func (b *build) title() (string, error) {
	spec, err := os.ReadFile(b.path(b.rec.Name.file()))
	if err != nil {
		return "", err
	}

	line, _, _ := strings.Cut(string(spec), "\n")
	if title, ok := strings.CutPrefix(line, "# "); ok {
		return title, nil
	}

	return string(b.rec.Name), nil
}

// addProduct adds line to PRODUCT.md, which it starts if the worktree holds none.
func (b *build) addProduct(line string) error {
	productPath := b.path(product)
	text, err := os.ReadFile(productPath)
	if errors.Is(err, fs.ErrNotExist) {
		text, err = []byte(productHead), nil
	}

	if err != nil {
		return err
	}

	return os.WriteFile(productPath, append(text, line...), 0o644)
}

const productHead = "# Product\n\nEach spec the developer accepted, and the command that shows it in the commit " +
	"that last held it.\n\n"

// set applies change once, to the stored record, and takes that record as the build's copy.
func (b *build) set(change func(*record)) {
	err := b.r.store.update(func(k *book) error {
		if r := k.find(b.rec.Name); r != nil {
			change(r)
			b.rec = *r
		}

		return nil
	})
	if err != nil {
		change(&b.rec)
		b.say("kiln could not record it: %v", err)
	}
}

// stop records the build as stopped, why, and what failed. It leaves the worktree and the session in place.
func (b *build) stop(why string, failing []string) {
	b.set(func(r *record) { r.State, r.Why, r.Failing, r.Paused = stopped, why, failing, time.Time{} })
	b.record(entry{Kind: outcomeKind, Outcome: outStopped})
	b.say("stopped: %s", strings.SplitN(why, "\n", 2)[0])
}

func (b *build) say(format string, args ...any) {
	fmt.Fprintf(b.out, "%s: %s\n", b.rec.Name, fmt.Sprintf(format, args...))
}

func (b *build) message(what string) string {
	return fmt.Sprintf("feat(%s): %s\n\nkiln built %s, approved at %s, in session %s.\n", b.rec.Name, what,
		b.rec.Name.file(), short(b.rec.Approved), b.rec.Session)
}

// landing is the message of the commit that lands the build. It holds the build's time, cost, turns and failed calls,
// so git log shows what each landed build cost.
func (b *build) landing() string {
	return b.message("land the approved spec") + "\nBuild: " + b.r.summary(b.rec.Build) + "\n"
}

// gate runs the gate on the build's worktree, and writes it to the ledger. landing is the gate on the work rebased
// onto main.
func (b *build) gate(ctx context.Context, landing bool, allowed ...string) (verdict, error) {
	start := time.Now()
	v, err := b.r.gate(ctx, guard{spec: b.rec.Name, place: b.tree, allowed: allowed})
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
	gateFailed = "The gate failed, so it comes back to you: round %d of %d. Fix what it found, and end your " +
		"turn.\n\n%s"
	audited = "The audit read the directories you changed, and your acceptance tests' code beside their Given, " +
		"When, Then. Fix each finding, or say in your reply why it is wrong. Then end your turn.\n\n%s"
	auditing  = "Audit these directories: %s. The spec is %s. The build changed these files: %s."
	conflicts = "Main moved, and your work conflicts with it in these files:\n%s\n\nMerge main into your " +
		"branch with git merge main, resolve each conflict, commit, and end your turn."
	unchanged   = "Your work changes nothing on main, so it can't land. Build the spec, and end your turn."
	limitReset  = "The usage limit has reset. Go on where you stopped."
	interrupted = "kiln stopped while you worked. Go on where you stopped."
)
