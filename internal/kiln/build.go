package kiln

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fwilkerson/kiln/guidance"
	"github.com/fwilkerson/kiln/internal/claude"
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
func (r *Repo) run(ctx context.Context, out io.Writer, only string) error {
	unlock, err := r.store.lock(runLock, syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return errRunning
	}

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
	err = r.store.update(func(b *book) error {
		for _, s := range b.Specs {
			if s.State == building {
				s.State = queued
			}
		}

		slices.SortStableFunc(b.Specs, func(x, y *record) int { return goesOn(y) - goesOn(x) })
		return nil
	})
	if err != nil {
		return err
	}

	for {
		var rec record
		var found bool
		err := r.store.update(func(b *book) error {
			rec, found = b.next(only, os.Getpid())
			// A landing queued after this update finds the run lock free, so no kiln accept counts on this run.
			if !found {
				unlock()
			}

			return nil
		})
		if err != nil || !found {
			return err
		}

		b := &build{r: r, rec: rec, tree: r.tree(rec.Name), out: out}
		err = b.run(ctx)
		if s, ok := errors.AsType[*outOfRounds](err); ok {
			b.stop(s.why)
		} else if ctx.Err() != nil {
			return ctx.Err()
		} else if err != nil {
			b.stop(fmt.Sprintf("kiln could not go on with the build: %v", err))
		}
	}
}

func goesOn(r *record) int {
	if r.State == queued && r.Session != "" {
		return 1
	}

	return 0
}

// build is one build of a spec, or the landing of an accepted one.
type build struct {
	r    *Repo
	rec  record
	tree string
	out  io.Writer
}

// outOfRounds is a build that its gate failed past its last round.
type outOfRounds struct{ why string }

func (s *outOfRounds) Error() string { return s.why }

func (b *build) run(ctx context.Context) error {
	// A spec approved before kiln kept a ledger has no build ID, and its build gets one where it stands.
	fresh := b.rec.Build == ""
	if fresh {
		b.set(func(r *record) { r.Build = newBuild() })
	}

	if fresh || b.rec.Session == "" {
		b.begin(ctx)
	}

	// A landing cut off left the branch where it moved it.
	if b.rec.Before != "" {
		if err := b.unland(ctx); err != nil {
			return err
		}
	}

	switch {
	case b.rec.Session == "":
		if err := b.start(ctx); err != nil {
			return err
		}
	case !b.rec.Accepted:
		why := reason{Why: whyInterrupted, Round: b.rec.Rounds}
		if _, err := b.talk(ctx, b.launch(true, interrupted), why); err != nil {
			return err
		}
	}

	for {
		if _, err := b.r.git.Commit(ctx, b.tree, b.message("the session's work")); err != nil {
			return err
		}

		v, err := b.gate(ctx, false, b.allowed()...)
		if err != nil {
			return err
		}

		if b.rec.FailingAtMain == nil && len(v.atMain) > 0 {
			b.set(func(r *record) { r.FailingAtMain = v.atMain })
		}

		next, why, err := b.after(ctx, v)
		if err != nil || next == "" {
			return err
		}

		if _, err := b.talk(ctx, b.launch(true, next), why); err != nil {
			return err
		}
	}
}

// after takes the build on from a gate's verdict. It answers what to tell the session next, and why. It answers "" once
// the build has landed, or waits on its branch.
func (b *build) after(ctx context.Context, v verdict) (string, reason, error) {
	if !v.passed {
		return b.round(ctx, setback{why: whyGate, report: v.report})
	}

	if !b.rec.Audited {
		findings, err := b.audit(ctx)
		if err != nil {
			return "", reason{}, err
		}

		b.set(func(r *record) { r.Audited = true })
		if findings != "" {
			return fmt.Sprintf(audited, findings), reason{Why: whyFindings}, nil
		}
	}

	head, err := b.r.git.Head(ctx, b.tree)
	if err != nil {
		return "", reason{}, err
	}

	var retire func(context.Context, string) error
	if b.rec.Accepted {
		retire = b.retire
	} else if amendments, found, err := b.r.git.Show(ctx, head, amendPath(b.rec.Name)); err != nil || found {
		b.set(func(r *record) { r.State, r.Amendments = amended, string(amendments) })
		if err == nil {
			b.record(entry{Kind: outcomeKind, Outcome: outWaits, Amendments: string(amendments)})
		}

		b.say("waits on %s, with amendments for UAT", branchOf(b.rec.Name))
		return "", reason{}, err
	}

	onMain, back, err := b.land(ctx, head, retire, b.allowed()...)
	if err != nil || back.report != "" {
		if err == nil {
			return b.round(ctx, back)
		}

		return "", reason{}, err
	}

	if err := b.r.git.Remove(ctx, b.tree, branchOf(b.rec.Name)); err != nil {
		return "", reason{}, err
	}

	if b.rec.Accepted {
		b.record(entry{Kind: outcomeKind, Outcome: outAccepted})
		b.say("accepted, and main is at %s", short(onMain))
		return "", reason{}, b.r.store.update(func(k *book) error { k.retire(b.rec.Name); return nil })
	}

	b.record(entry{Kind: outcomeKind, Outcome: outLanded})
	b.set(func(r *record) { r.State = landed })
	b.say("landed on main at %s, and waits for UAT", short(onMain))
	return "", reason{}, nil
}

// allowed are the paths under specs/ that an accepted spec's landing changes: it folds the amendments, and retires
// the spec.
func (b *build) allowed() []string {
	if b.rec.Accepted {
		return []string{specPath(b.rec.Name), amendPath(b.rec.Name)}
	}

	return nil
}

// setback is what kept the work from passing, for the session. It is a failed gate, a conflict with main, or a failed
// gate on the work rebased onto main. why names which, as a start line does.
type setback struct {
	why, report string
}

// round sends a setback's report back to the session, or stops the build past its last round. It answers the prompt,
// and the round it starts.
func (b *build) round(ctx context.Context, back setback) (string, reason, error) {
	main, err := b.r.git.Tip(ctx, mainBranch)
	if err != nil {
		return "", reason{}, err
	}

	c, err := b.r.config(ctx, main)
	if err != nil {
		return "", reason{}, err
	}

	if b.rec.Rounds >= c.Rounds {
		return "", reason{}, &outOfRounds{why: "the gate still failed after its last round:\n" + back.report}
	}

	b.set(func(r *record) { r.Rounds++ })
	b.say("the gate failed; it goes back to the session, round %d of %d", b.rec.Rounds, c.Rounds)
	why := reason{Why: back.why, Round: b.rec.Rounds, Rounds: c.Rounds}
	return fmt.Sprintf(gateFailed, b.rec.Rounds, c.Rounds, back.report), why, nil
}

func (b *build) start(ctx context.Context) error {
	main, err := b.r.git.Tip(ctx, mainBranch)
	if err != nil {
		return err
	}

	// A branch an earlier build of the spec left would hold work from a spec as it was approved before.
	if err := b.r.git.Remove(ctx, b.tree, branchOf(b.rec.Name)); err != nil {
		return err
	}

	if err := b.r.git.Checkout(ctx, b.tree, branchOf(b.rec.Name), main); err != nil {
		return err
	}

	session := newSession()
	b.set(func(r *record) { r.Session = session })
	b.say("building on %s, in %s", branchOf(b.rec.Name), b.tree)
	prompt := fmt.Sprintf(starting, specPath(b.rec.Name), short(b.rec.Approved), branchOf(b.rec.Name))
	_, err = b.talk(ctx, b.launch(false, prompt), reason{Why: whyFirst})
	return err
}

func (b *build) launch(resume bool, prompt string) claude.Launch {
	name := b.rec.Name
	hook := fmt.Sprintf("%s hook --spec %s --place %s", shellQuote(b.r.exe), name, shellQuote(b.tree))
	return b.sandboxed(claude.Launch{
		Session: b.rec.Session,
		Resume:  resume,
		Role:    "build",
		System:  fmt.Sprintf(buildRole, name),
		Prompt:  prompt,
		Dir:     b.tree,
		Hook:    hook,
	})
}

// sandboxed adds to l what every session of a build holds to.
func (b *build) sandboxed(l claude.Launch) claude.Launch {
	l.Deny = []string{"Bash(git push)", "Bash(git push *)", "Bash(git update-ref *)", "Bash(git branch -f *)",
		"Bash(git branch -D *)", "Bash(git checkout main)", "Bash(git switch main)", "Bash(git reset * main)"}
	l.Forbidden = []string{
		"Pushing to any remote, in any form.",
		"Writing, moving, resetting or deleting any ref but the branch checked out in the working directory, " +
			"and above all main.",
		fmt.Sprintf("Changing anything under specs/ but %s, by a file tool or a shell command.", amendPath(b.rec.Name)),
		"Changing .claude/settings.json or .claude/settings.local.json.",
	}
	l.Writable = b.r.goCaches(context.Background())
	l.Domains = []string{"proxy.golang.org", "sum.golang.org", "storage.googleapis.com"}
	return l
}

// talk runs the session l launches, and answers how it ended. A usage limit pauses the build until it resets, and the
// same session resumes. The ledger gets a line before each run of the session, with why it started, and one after.
//
// The audit changes nothing, so kiln checks. It runs in a worktree of its own. Each audit run's start line names the
// commit it read, and its end line each path the run changed. Then kiln puts that worktree back at the commit read.
func (b *build) talk(ctx context.Context, l claude.Launch, why reason) (claude.Ended, error) {
	for {
		streamed, err := b.r.claude.Streamed(l.Session)
		if err != nil {
			b.say("kiln could not measure session %s's stream: %v", l.Session, err)
		}

		var read string
		if l.Role == auditRole {
			if read, err = b.r.git.Head(ctx, l.Dir); err != nil {
				return claude.Ended{}, err
			}
		}

		b.record(entry{Kind: startKind, Role: l.Role, Session: l.Session, Streamed: streamed, PID: os.Getpid(),
			reason: why, Read: read})
		ended, err := b.r.claude.Run(ctx, l)
		end := entry{Kind: endKind, Session: l.Session, Ended: how(ended), Totals: &ended.Totals}
		end.Streamed, _ = b.r.claude.Streamed(l.Session)
		var undone error
		if l.Role == auditRole {
			// An interrupted audit run may have changed its worktree too, and its end line names what it changed.
			end.Changed, undone = b.r.undo(context.WithoutCancel(ctx), l.Dir, read)
		}

		b.record(end)
		if err := errors.Join(err, undone); err != nil && ctx.Err() == nil {
			return ended, err
		}

		if ctx.Err() != nil {
			// A session kiln's interrupt ended has not ended on its own, so how it ended says nothing.
			return ended, ctx.Err()
		}

		if ended.Limit == nil {
			return ended, nil
		}

		until := ended.Limit.Resets
		if until.IsZero() {
			until = time.Now().Add(unknownReset)
		}

		b.set(func(r *record) { r.Paused = until })
		b.say("paused for a usage limit until %s", until.Local().Format(time.Kitchen))
		select {
		case <-ctx.Done():
			return ended, ctx.Err()
		case <-time.After(time.Until(until)):
		}

		b.set(func(r *record) { r.Paused = time.Time{} })
		l.Resume, l.Prompt, why.Why = true, limitReset, whyLimit
	}
}

const auditRole = "audit"

// undo answers each path an audit run changed in its worktree at dir, committed or not, since the commit read. Then
// it puts the worktree back at read, so the next run of the audit sees only the build's work.
func (r *Repo) undo(ctx context.Context, dir, read string) ([]string, error) {
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

// audit runs the audit agent on the packages the build changed, and its acceptance tests. It answers the findings, or
// "" for none. An audit that names no finding count is all findings. An audit that ends with no result runs again, in a
// new session, up to auditTries times.
func (b *build) audit(ctx context.Context) (string, error) {
	main, err := b.r.git.Tip(ctx, mainBranch)
	if err != nil {
		return "", err
	}

	head, err := b.r.git.Head(ctx, b.tree)
	if err != nil {
		return "", err
	}

	base, err := b.r.git.MergeBase(ctx, main, head)
	if err != nil {
		return "", err
	}

	changed, err := b.r.git.Changed(ctx, base, head)
	if err != nil {
		return "", err
	}

	var packages []string
	for _, p := range changed {
		if strings.HasSuffix(p, ".go") && !slices.Contains(packages, "./"+path.Dir(p)) {
			packages = append(packages, "./"+path.Dir(p))
		}
	}

	if len(packages) == 0 {
		return "", nil
	}

	b.say("the audit reads %s", strings.Join(packages, " "))
	// The audit's own worktree keeps what it writes out of the build's, even when kiln stops mid-audit.
	l := b.sandboxed(claude.Launch{
		Role:   auditRole,
		System: guidance.Audit(),
		Prompt: fmt.Sprintf(auditing, strings.Join(packages, " "), specPath(b.rec.Name), b.rec.Name),
		Dir:    b.r.auditTree(b.rec.Name),
		Tools:  []string{"Read", "Grep", "Glob", "Bash"},
	})
	for try := 1; ; try++ {
		if err := b.r.git.Detached(ctx, l.Dir, head); err != nil {
			return "", err
		}

		l.Session = newSession()
		ended, err := b.talk(ctx, l, reason{Why: whyAudit, Try: try})
		switch {
		case err != nil:
			return "", err
		case ended.Failed && try < auditTries:
			b.say("the audit ended with no result; it runs again")
			continue
		case ended.Failed:
			return "", fmt.Errorf("the audit ended with no result %d times", auditTries)
		}

		text := strings.TrimSpace(ended.Text)
		if m := tally.FindStringSubmatch(text); m != nil && m[1] == "0" {
			return "", nil
		}

		return text, nil
	}
}

const auditTries = 3

// land rebases the build's work onto main as one commit, links its guidance in, and gates it. Then it moves main
// there, only if main has not moved since the rebase; if it has, land starts again. from is the commit to rebase,
// or "" to start from main's tip. Before the gate, retire may add a commit. A path in allowed may change, though
// guarded names it.
//
// It answers the commit main moved to. Or it answers what kept the work from landing, a conflict or the gate, for the
// session. A landing that doesn't move main puts the branch back where it stood, even after an error or an interrupt.
// Should kiln die mid-landing, the record keeps that commit, and the next kiln run puts the branch back.
func (b *build) land(ctx context.Context, from string, retire func(ctx context.Context, held string) error,
	allowed ...string) (landed string, back setback, err error) {
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
		onto, err := b.r.git.Tip(ctx, mainBranch)
		if err != nil {
			return "", setback{}, err
		}

		start := cmpOr(from, onto)
		if err := b.r.git.Reset(ctx, b.tree, start); err != nil {
			return "", setback{}, err
		}

		if start != onto {
			_, conflict, err := b.r.git.Rebase(ctx, b.tree, onto, b.landing())
			if err != nil {
				return "", setback{}, err
			}

			if conflict != nil {
				return "", setback{whyConflict, fmt.Sprintf(conflicts, strings.Join(conflict.Files, "\n"))}, nil
			}
		}

		// Main already holds start when no rebase ran, so the links need a commit of their own.
		held, err := b.linkGuidance(ctx, start != onto)
		if err != nil {
			return "", setback{}, err
		}

		if retire != nil {
			if err := retire(ctx, held); err != nil {
				return "", setback{}, err
			}
		}

		v, err := b.gate(ctx, true, allowed...)
		if err != nil || !v.passed {
			return "", setback{whyLanding, v.report}, err
		}

		head, err := b.r.git.Head(ctx, b.tree)
		if err != nil {
			return "", setback{}, err
		}

		way, err := b.r.git.Move(ctx, mainBranch, head, onto)
		switch {
		case err != nil:
			return "", setback{}, err
		case way == nil:
			return head, setback{}, nil
		case way.Tip != "":
			b.say("main moved while the gate ran; the work is rebased onto it again")
			continue
		}

		return "", setback{}, fmt.Errorf(
			"main's checkout is in the way of the landing: busy in %q, or with changes to %s", way.Busy,
			strings.Join(way.Files, ", "))
	}
}

// unland puts the branch back at the commit it stood at before its landing began.
func (b *build) unland(ctx context.Context) error {
	if err := b.r.git.Reset(ctx, b.tree, b.rec.Before); err != nil {
		return err
	}

	b.set(func(r *record) { r.Before = "" })
	return nil
}

// linkGuidance links .claude/agents to guidance/agents/, and .claude/skills to guidance/skills/, for each that
// guidance/ holds and .claude/ does not. A .claude/ directory the developer made stays theirs. With amend, the links go
// into the commit at HEAD; without it, into a new commit, if they changed anything. It answers the commit at HEAD.
func (b *build) linkGuidance(ctx context.Context, amend bool) (string, error) {
	for _, dir := range []string{"agents", "skills"} {
		held, err := isDir(filepath.Join(b.tree, "guidance", dir))
		if err != nil {
			return "", err
		}

		link := ".claude/" + dir
		_, err = os.Lstat(filepath.Join(b.tree, link))
		switch {
		case !held || err == nil:
			continue
		case !errors.Is(err, fs.ErrNotExist):
			return "", err
		}

		if err := b.r.git.Link(ctx, b.tree, link, "../guidance/"+dir); err != nil {
			return "", err
		}
	}

	if amend {
		return b.r.git.Amend(ctx, b.tree)
	}

	return b.r.git.Commit(ctx, b.tree, "chore: link guidance into .claude\n")
}

func isDir(p string) (bool, error) {
	info, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}

	return err == nil && info.IsDir(), err
}

// foldAmendments commits, on the spec's branch, the spec with its amendments folded in, and the amendment file
// removed. It answers that commit.
func (b *build) foldAmendments(ctx context.Context) (string, error) {
	name := b.rec.Name
	head, err := b.r.git.Head(ctx, b.tree)
	if err != nil {
		return "", err
	}

	spec, _, err := b.r.git.Show(ctx, head, specPath(name))
	if err != nil {
		return "", err
	}

	amendments, _, err := b.r.git.Show(ctx, head, amendPath(name))
	if err != nil {
		return "", err
	}

	folded, err := foldAmendments(string(spec), string(amendments))
	if err != nil {
		return "", fmt.Errorf("%w: %s cannot fold into %s: %w", errRefused, amendPath(name), specPath(name), err)
	}

	if err := os.WriteFile(filepath.Join(b.tree, specPath(name)), []byte(folded), 0o644); err != nil {
		return "", err
	}

	if err := os.Remove(filepath.Join(b.tree, amendPath(name))); err != nil {
		return "", err
	}

	return b.r.git.Commit(ctx, b.tree, fmt.Sprintf("docs(%s): fold the amendments into the spec\n", name))
}

func (b *build) checkoutMain(ctx context.Context) error {
	main, err := b.r.git.Tip(ctx, mainBranch)
	if err != nil {
		return err
	}

	if err := b.r.git.Remove(ctx, b.tree, branchOf(b.rec.Name)); err != nil {
		return err
	}

	return b.r.git.Checkout(ctx, b.tree, branchOf(b.rec.Name), main)
}

// retire deletes the spec and its amendments, and adds its line to PRODUCT.md. The line holds the command that shows
// the spec at held, the commit that last held it.
func (b *build) retire(ctx context.Context, held string) error {
	name := b.rec.Name
	spec, err := os.ReadFile(filepath.Join(b.tree, specPath(name)))
	if err != nil {
		return err
	}

	title := name
	if line, _, _ := strings.Cut(string(spec), "\n"); strings.HasPrefix(line, "# ") {
		title = strings.TrimPrefix(line, "# ")
	}

	productPath := filepath.Join(b.tree, product)
	text, err := os.ReadFile(productPath)
	if errors.Is(err, fs.ErrNotExist) {
		text, err = []byte(productHead), nil
	}

	if err != nil {
		return err
	}

	line := fmt.Sprintf("- %s: `git show %s:%s`\n", title, short(held), specPath(name))
	if err := os.WriteFile(productPath, append(text, line...), 0o644); err != nil {
		return err
	}

	for _, p := range []string{specPath(name), amendPath(name)} {
		if err := os.Remove(filepath.Join(b.tree, p)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}

	_, err = b.r.git.Commit(ctx, b.tree, fmt.Sprintf("docs(%s): retire the accepted spec\n", name))
	return err
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

// stop records the build as stopped, and why.
func (b *build) stop(why string) {
	b.set(func(r *record) { r.State, r.Why, r.Paused = stopped, why, time.Time{} })
	b.record(entry{Kind: outcomeKind, Outcome: outStopped})
	b.say("stopped: %s", strings.SplitN(why, "\n", 2)[0])
}

func (b *build) say(format string, args ...any) {
	fmt.Fprintf(b.out, "%s: %s\n", b.rec.Name, fmt.Sprintf(format, args...))
}

func (b *build) message(what string) string {
	return fmt.Sprintf("feat(%s): %s\n\nkiln built %s, approved at %s, in session %s.\n", b.rec.Name, what,
		specPath(b.rec.Name), short(b.rec.Approved), b.rec.Session)
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
	v, err := b.r.gate(ctx, b.tree, b.rec.Name, allowed...)
	b.record(entry{Kind: gateKind, At: start, End: time.Now(), Passed: v.passed, Landing: landing})
	return v, err
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}

	return b
}

func jsonIndent(v any) ([]byte, error) {
	raw, err := json.MarshalIndent(v, "", "  ")
	return append(raw, '\n'), err
}

//go:embed build.md
var buildRole string

const (
	starting = "Build %s, approved at %s. Your worktree is on its branch, %s, made from main. When the build " +
		"is done, end your turn: kiln commits what you leave, and gates it."
	gateFailed = "The gate failed, so it comes back to you: round %d of %d. Fix what it found, and end your " +
		"turn.\n\n%s"
	audited = "The audit read the packages you changed, and your acceptance tests' code beside their Given, " +
		"When, Then. Fix each finding, or say in your reply why it is wrong. Then end your turn.\n\n%s"
	auditing  = "Audit these packages: %s. The spec is %s, and its acceptance tests' code is in acceptance/%s/."
	conflicts = "Main moved, and your work conflicts with it in these files:\n%s\n\nMerge main into your " +
		"branch with git merge main, resolve each conflict, commit, and end your turn."
	limitReset  = "The usage limit has reset. Go on where you stopped."
	interrupted = "kiln stopped while you worked. Go on where you stopped."
)
