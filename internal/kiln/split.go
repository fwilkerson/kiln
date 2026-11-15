package kiln

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
)

// A split build runs two sessions at once, each in a clone of main of its own:
//
//	main ──┬─▶ kiln/<name>.build    the build session writes the code
//	       └─▶ kiln/<name>.verify   the verify session writes the acceptance tests, from the spec alone
//	gate kiln/<name>.build alone ── fails ──▶ the build session
//	stack both on kiln/<name> ── conflicts ──▶ the verify session
//	gate the stack ── fails ──▶ the verify session, whose bugs line sends a bug to the build session
//	audit the stack ── findings ──▶ both sessions at once
//	land kiln/<name> as one commit, or wait there with amendments
//
// Each session's branch lives in its clone. The repository's kiln/<name>.build and kiln/<name>.verify are only
// fetches of them.

// clone is the clone in which role's session of a split build of name works. A spec's name never starts with a dot,
// so it is no build's tree.
func (r *Repo) clone(name Spec, role string) git.Worktree {
	return git.Worktree(sideClone(r.tree(name), name, role))
}

// sideBranch is the branch role's session of a split build of the spec works on.
func (s Spec) sideBranch(role string) git.Branch { return git.Branch("kiln/" + string(s) + "." + role) }

// other is the role of a split build's other session.
func other(role string) string {
	if role == buildRole {
		return verifyRole
	}

	return buildRole
}

// dropCheckouts removes every checkout and branch a build of name made: the stack, the audit's worktree, and each
// session's clone. The .main worktree stays, since every gate shares it.
func (r *Repo) dropCheckouts(ctx context.Context, name Spec) error {
	return errors.Join(
		r.git.Remove(ctx, r.tree(name), name.branch()),
		r.git.Remove(ctx, r.auditTree(name), ""),
		r.git.Remove(ctx, r.clone(name, buildRole), name.sideBranch(buildRole)),
		r.git.Remove(ctx, r.clone(name, verifyRole), name.sideBranch(verifyRole)),
	)
}

// turn is what each session of a split build is told next. A session whose prompt is "" does not run.
type turn struct {
	build, verify string
	why           reason
	// first starts each session, where every other turn resumes it.
	first bool
}

// none is whether t tells neither session anything, and gives no reason.
func (t turn) none() bool { return t.build == "" && t.verify == "" && t.why.Why == "" }

// splitRun runs a split build until it lands, waits on its branch, or stops.
func (b *build) splitRun(ctx context.Context) error {
	if err := b.splitGoOn(ctx); err != nil {
		return err
	}

	for {
		if err := b.passOn(ctx); err != nil {
			return err
		}

		t, err := b.splitStep(ctx)
		if err != nil || t.none() {
			return err
		}

		if err := b.both(ctx, t); err != nil {
			return err
		}
	}
}

// splitGoOn takes the split build up where kiln left it: it starts both sessions, or resumes each.
func (b *build) splitGoOn(ctx context.Context) error {
	b.ledger(ctx)
	if err := b.unland(ctx); err != nil {
		return err
	}

	rec := b.now()
	switch {
	case rec.Session == "":
		return b.splitStart(ctx)
	case rec.Accepted:
		return nil
	}

	// Only a session whose turn kiln cut short goes on. One that had ended its turn waits for the next gate.
	t := turn{why: reason{Why: whyInterrupted, Round: rec.Rounds}}
	if slices.Contains(rec.Running, buildRole) {
		t.build = interrupted
	}

	if slices.Contains(rec.Running, verifyRole) {
		t.verify = interrupted
	}

	return b.both(ctx, t)
}

// splitStart makes each session's clone from main's tip, and starts both sessions at once. A revised spec's old
// acceptance tests are on main, so kiln commits their removal on the build side first.
func (b *build) splitStart(ctx context.Context) error {
	name := b.rec.Name
	s, err := b.sides(ctx)
	if err != nil {
		return err
	}

	if err := b.r.dropCheckouts(ctx, name); err != nil {
		return err
	}

	if err := b.cloneSides(ctx); err != nil {
		return err
	}

	old, err := b.removeOldTests(ctx, s.acceptance)
	if err != nil {
		return err
	}

	main, err := b.r.git.Tip(ctx, mainBranch)
	if err != nil {
		return err
	}

	told, err := b.amendedSpec(ctx, main)
	if err != nil {
		return err
	}

	b.set(func(r *record) { r.Session, r.Verify, r.OldTests, r.Told = newSession(), newSession(), old, told })
	if err := b.fetchSides(ctx); err != nil {
		return err
	}

	b.say("building on %s, in %s, and verifying on %s, in %s", name.sideBranch(buildRole),
		b.r.clone(name, buildRole), name.sideBranch(verifyRole), b.r.clone(name, verifyRole))
	return b.both(ctx, turn{
		build:  fmt.Sprintf(startingSplit, name.file(), short(b.rec.Approved), name.sideBranch(buildRole)),
		verify: fmt.Sprintf(verifying, name.file(), short(b.rec.Approved), name.sideBranch(verifyRole)),
		why:    reason{Why: whyFirst},
		first:  true,
	})
}

// cloneSides makes each session's clone from main's tip, on its side's branch.
func (b *build) cloneSides(ctx context.Context) error {
	name := b.rec.Name
	for _, role := range []string{buildRole, verifyRole} {
		if err := b.r.git.Clone(ctx, b.r.clone(name, role), mainBranch, name.sideBranch(role)); err != nil {
			return err
		}
	}

	return nil
}

// removeOldTests commits, on the build side, the removal of the spec's acceptance tests main holds. It answers
// whether main held any.
func (b *build) removeOldTests(ctx context.Context, acceptance string) (bool, error) {
	clone := b.r.clone(b.rec.Name, buildRole)
	at := filepath.Join(string(clone), acceptance)
	if _, err := os.Lstat(at); err != nil {
		return false, nil
	}

	if err := os.RemoveAll(at); err != nil {
		return false, err
	}

	message := fmt.Sprintf("chore(%s): remove the old acceptance tests, which the verify session writes anew\n",
		b.rec.Name)
	_, err := b.r.git.Commit(ctx, clone, message)
	return true, err
}

// fetchSides commits what each session left in its clone, and fetches each side's branch into the repository.
func (b *build) fetchSides(ctx context.Context) error {
	name := b.rec.Name
	for _, role := range []string{buildRole, verifyRole} {
		if _, err := b.r.git.Commit(ctx, b.r.clone(name, role), b.message("the "+role+" session's work")); err != nil {
			return err
		}

		if err := b.r.git.Fetch(ctx, b.r.clone(name, role), name.sideBranch(role)); err != nil {
			return err
		}
	}

	return nil
}

// passOn passes on each reply kiln read but has not passed on. A bug the verify session found goes to the build
// session. Then a test the build session can't meet goes to the verify session. Each reply stays in the record until
// its session's turn ends. The verify session answers the build session with commits alone, so its reply goes nowhere.
func (b *build) passOn(ctx context.Context) error {
	bug := b.now().ForBuild
	why := reason{Why: whyBug, Round: b.now().Rounds, Rounds: b.limit()}
	if err := b.pass(ctx, bug, turn{build: bug, why: why}, func(r *record) { r.ForBuild = "" }); err != nil {
		return err
	}

	reply := b.now().ForVerify
	t := turn{verify: fmt.Sprintf(cannotHold, reply), why: reason{Why: whyCannotHold}}
	return b.pass(ctx, reply, t, func(r *record) { r.ForVerify = "" })
}

// pass runs t when reply is not "", and then clears the reply from the record as passed.
func (b *build) pass(ctx context.Context, reply string, t turn, passed func(*record)) error {
	if reply == "" {
		return nil
	}

	if err := b.both(ctx, t); err != nil {
		return err
	}

	b.set(passed)
	return nil
}

// splitStep gates the work, and answers what each session is told next. Once the build has landed, or waits on its
// branch, it answers the zero turn.
func (b *build) splitStep(ctx context.Context) (turn, error) {
	if err := b.fetchSides(ctx); err != nil {
		return turn{}, err
	}

	v, err := b.gateBuild(ctx)
	switch {
	case err != nil:
		return turn{}, err
	case !v.passed:
		_, why, err := b.round(setback{why: whyGate, report: v.report, failing: v.failing})
		return turn{build: fmt.Sprintf(gateFailed, v.report), why: why}, err
	}

	back, err := b.stack(ctx)
	switch {
	case err != nil:
		return turn{}, err
	case back.why != "":
		_, why, err := b.round(back)
		return turn{verify: back.report, why: why}, err
	}

	if t, err := b.gateStack(ctx); err != nil || t.why.Why != "" {
		return t, err
	}

	return b.splitAfter(ctx)
}

// splitAfter takes a split build on once its stack passed its gate. It goes to the audit, its branch, or main.
func (b *build) splitAfter(ctx context.Context) (turn, error) {
	findings, why, err := b.findings(ctx)
	if err != nil || findings != "" {
		return turn{build: fmt.Sprintf(auditedBuild, findings), verify: fmt.Sprintf(auditedVerify, findings),
			why: why}, err
	}

	head, err := b.r.git.Head(ctx, b.tree)
	if err != nil {
		return turn{}, err
	}

	if waits, err := b.waitsOnBranch(ctx, head); err != nil || waits {
		return turn{}, err
	}

	return b.splitLand(ctx, head)
}

// sideGuard is the guard on role's session's own work, in its clone.
func (b *build) sideGuard(ctx context.Context, role string) (guard, error) {
	s, err := b.sides(ctx)
	own := side{role: role, acceptance: s.acceptance, harness: s.harness}
	return guard{spec: b.rec.Name, place: b.r.clone(b.rec.Name, role), allowed: b.allowed(), side: own}, err
}

// gateBuild gates the build session's work alone. It holds none of the spec's acceptance tests. A revised spec's
// old ones, which kiln removed, are gated at main without them too.
func (b *build) gateBuild(ctx context.Context) (verdict, error) {
	g, err := b.sideGuard(ctx, buildRole)
	if err != nil {
		return verdict{}, err
	}

	without := ""
	if b.now().OldTests {
		without = g.side.acceptance
	}

	v, err := b.gated(ctx, false, without, g)
	if err == nil {
		b.keepFailingAtMain(v.atMain)
	}

	return v, err
}

// keepFailingAtMain records the tests that fail at main, the first time a gate of the build side names any.
func (b *build) keepFailingAtMain(atMain []string) {
	if b.now().FailingAtMain == nil && len(atMain) > 0 {
		b.set(func(r *record) { r.FailingAtMain = atMain })
	}
}

// gateStack gates the stack, and checks the verify session's own work stays on its side. A failure goes to the
// verify session. Its answer must end with its bugs line.
func (b *build) gateStack(ctx context.Context) (turn, error) {
	g, err := b.sideGuard(ctx, verifyRole)
	if err != nil {
		return turn{}, err
	}

	v, err := b.gated(ctx, false, "", b.treeGuard(b.allowed()...), g)
	if err != nil || v.passed {
		return turn{}, err
	}

	_, why, err := b.round(setback{why: whyStack, report: v.report, failing: v.failing})
	if err != nil {
		return turn{}, err
	}

	b.set(func(r *record) { r.Asked = true })
	return turn{verify: fmt.Sprintf(stackFailed, v.report), why: why}, nil
}

// stack makes kiln/<name> anew, in the build's worktree: the build side's tip, then the verify side's work merged
// onto it. Only kiln writes there. It answers a conflict as a setback for the verify session, with both sides of
// each file.
func (b *build) stack(ctx context.Context) (setback, error) {
	name := b.rec.Name
	tips := map[string]git.Rev{}
	for _, role := range []string{buildRole, verifyRole} {
		tip, err := b.r.git.Tip(ctx, name.sideBranch(role))
		if err != nil {
			return setback{}, err
		}

		tips[role] = tip
	}

	if err := b.r.git.Remove(ctx, b.tree, name.branch()); err != nil {
		return setback{}, err
	}

	if err := b.r.git.Checkout(ctx, b.tree, name.branch(), tips[buildRole]); err != nil {
		return setback{}, err
	}

	if err := b.putBack(ctx, tips[buildRole], tips[verifyRole]); err != nil {
		return setback{}, err
	}

	message := fmt.Sprintf("chore(%s): stack the verify session's tests on the build session's code\n", name)
	conflict, err := b.r.git.Merge(ctx, b.tree, name.sideBranch(verifyRole), message)
	if err != nil || conflict == nil {
		return setback{}, err
	}

	report, err := b.bothSides(ctx, conflict.Files, tips)
	return setback{why: whyConflict, report: report, failing: conflict.Files}, err
}

// putBack puts the acceptance path back on the build side of the stack, in a commit of kiln's. It puts it as it
// stands at the two tips' merge base. Then the verify side's changes there merge cleanly. Merged straight, a
// revised spec's removed tests conflict with each test the verify session changed. Each it left alone is deleted.
func (b *build) putBack(ctx context.Context, buildTip, verifyTip git.Rev) error {
	s, err := b.sides(ctx)
	if err != nil {
		return err
	}

	base, err := b.r.git.MergeBase(ctx, buildTip, verifyTip)
	if err != nil {
		return err
	}

	changed, err := b.r.git.Changed(ctx, base, buildTip)
	if err != nil {
		return err
	}

	var removed []string
	for _, p := range changed {
		if within(fold(p), s.acceptance) {
			removed = append(removed, p)
		}
	}

	if len(removed) == 0 {
		return nil
	}

	if err := b.r.git.Restore(ctx, b.tree, base, removed...); err != nil {
		return err
	}

	message := fmt.Sprintf("chore(%s): put the old acceptance tests back, for the verify session's to merge onto\n",
		b.rec.Name)
	_, err = b.r.git.Commit(ctx, b.tree, message)
	return err
}

// bothSides is the report of a stack's conflict: each file, as each side holds it.
func (b *build) bothSides(ctx context.Context, files []string, tips map[string]git.Rev) (string, error) {
	var report strings.Builder
	fmt.Fprintf(&report, stackConflict, strings.Join(files, "\n"))
	for _, f := range files {
		for _, role := range []string{buildRole, verifyRole} {
			text, found, err := b.r.git.Show(ctx, tips[role], f)
			if err != nil {
				return "", err
			}

			held := "```\n" + string(text) + "\n```"
			if !found {
				held = "(no such file)"
			}

			fmt.Fprintf(&report, "\n\n%s, as the %s session's side holds it:\n\n%s", f, role, held)
		}
	}

	return report.String(), nil
}

// splitLand lands the stack at head as one commit, and removes every checkout the build made. A setback goes back
// as a round: work that changes nothing goes to the build session. Otherwise main moved, so kiln fetches it into
// both clones and merges it where the merge is clean. A conflict goes to the session whose side holds the file.
func (b *build) splitLand(ctx context.Context, head git.Rev) (turn, error) {
	var retire func(context.Context, git.Rev) error
	if b.rec.Accepted {
		retire = b.retire
	}

	onMain, back, err := b.land(ctx, head, retire, b.allowed()...)
	switch {
	case err != nil:
		return turn{}, err
	case back.report == "":
		if err := b.r.dropCheckouts(ctx, b.rec.Name); err != nil {
			return turn{}, err
		}

		return turn{}, b.onMain(onMain)
	}

	_, why, err := b.round(back)
	if err != nil || back.why == whyUnchanged {
		return turn{build: unchanged, why: why}, err
	}

	return b.mergeMains(ctx, why)
}

// mergeMains merges main into both clones, and answers the turn that has each session merge what conflicted.
func (b *build) mergeMains(ctx context.Context, why reason) (turn, error) {
	build, err := b.mergeMain(ctx, buildRole)
	if err != nil {
		return turn{}, err
	}

	verify, err := b.mergeMain(ctx, verifyRole)
	if err != nil {
		return turn{}, err
	}

	return turn{build: build, verify: verify, why: why}, nil
}

// mergeMain fetches main into role's clone, and merges it in a commit of kiln's when the merge is clean. On a
// conflict, kiln makes no commit: it answers the prompt that has the session merge. Then the gate reads what the
// session staged as its own write.
func (b *build) mergeMain(ctx context.Context, role string) (string, error) {
	clone := b.r.clone(b.rec.Name, role)
	if err := b.r.git.Pull(ctx, clone, mainBranch); err != nil {
		return "", err
	}

	message := fmt.Sprintf("chore(%s): merge main\n", b.rec.Name)
	conflict, err := b.r.git.Merge(ctx, clone, mainBranch, message)
	if err != nil || conflict == nil {
		return "", err
	}

	return fmt.Sprintf(conflicts, strings.Join(conflict.Files, "\n")), nil
}

// both runs each session that t tells something, at once, and waits for both to end.
func (b *build) both(ctx context.Context, t turn) error {
	launches, err := b.launches(ctx, t)
	if err != nil {
		return err
	}

	roles := make([]string, len(launches))
	for i, l := range launches {
		roles[i] = l.Role
	}

	b.set(func(r *record) { r.Running = roles })
	errs := make([]error, len(launches))
	var wg sync.WaitGroup
	for i, l := range launches {
		wg.Go(func() {
			// A turn kiln cut short stays running, so the next kiln run resumes it.
			if errs[i] = b.converse(ctx, l, t.why); errs[i] == nil {
				b.set(func(r *record) {
					r.Running = slices.DeleteFunc(r.Running, func(s string) bool { return s == l.Role })
				})
			}
		})
	}

	wg.Wait()
	return errors.Join(errs...)
}

// launches is a run of each session that t tells something.
func (b *build) launches(ctx context.Context, t turn) ([]claude.Launch, error) {
	var launches []claude.Launch
	for _, role := range []string{buildRole, verifyRole} {
		prompt, err := b.promptOf(ctx, t, role)
		if err != nil {
			return nil, err
		}

		if prompt == "" {
			continue
		}

		l, err := b.sessionLaunch(ctx, role, !t.first, prompt)
		if err != nil {
			return nil, err
		}

		launches = append(launches, l)
	}

	return launches, nil
}

// promptOf is what t tells role's session, or "" for nothing. The build session's prompt adds what told adds.
func (b *build) promptOf(ctx context.Context, t turn, role string) (string, error) {
	prompt := map[string]string{buildRole: t.build, verifyRole: t.verify}[role]
	if prompt == "" || role != buildRole {
		return prompt, nil
	}

	return b.told(ctx, prompt)
}

// told adds to prompt, for the build session, the verify session's amendments, when they changed since kiln last
// told it.
func (b *build) told(ctx context.Context, prompt string) (string, error) {
	tip, err := b.r.git.Tip(ctx, b.rec.Name.sideBranch(verifyRole))
	if err != nil || tip == "" {
		return prompt, err
	}

	spec, err := b.amendedSpec(ctx, tip)
	if err != nil || spec == b.now().Told {
		return prompt, err
	}

	b.set(func(r *record) { r.Told = spec })
	return prompt + "\n\n" + fmt.Sprintf(amendedNote, spec), nil
}

// amendedSpec is the spec as the commit at holds it, with any amendments there folded in. A fold the developer
// accepted then reads the same as the amendments it folded.
func (b *build) amendedSpec(ctx context.Context, at git.Rev) (string, error) {
	spec, _, err := b.r.git.Show(ctx, at, b.rec.Name.file())
	if err != nil {
		return "", err
	}

	amendments, found, err := b.r.git.Show(ctx, at, b.rec.Name.amendPath())
	if err != nil || !found {
		return string(spec), err
	}

	folded, err := foldAmendments(string(spec), string(amendments))
	if err != nil {
		return string(spec) + "\n\n" + string(amendments), nil
	}

	return folded, nil
}

// sessionLaunch is a run of role's session of the split build, in its own clone. Its hook keeps it to its side. Its
// settings deny it reads of every checkout but its own, and of the other session's transcripts.
func (b *build) sessionLaunch(ctx context.Context, role string, resume bool, prompt string) (claude.Launch, error) {
	g, err := b.sideGuard(ctx, role)
	if err != nil {
		return claude.Launch{}, err
	}

	rec := b.now()
	session := map[string]string{buildRole: rec.Session, verifyRole: rec.Verify}[role]
	l := claude.Launch{Session: session, Resume: resume, Prompt: prompt, Dir: string(g.place),
		Hook: g.hookCommand(b.r.exe)}
	l.DenyRead, l.AllowRead, l.Deny = b.r.blinds(b.rec.Name, role)
	return b.agentLaunch(ctx, role, l)
}

// blinds are what role's session of a split build may not read. The sandbox denies a command the repository's main
// checkout and its git directory, which hold both sides' branches. It denies the whole of kiln's checkouts too, and
// allowRead lets its own clone back in. The Read tool's rules deny each other checkout by name, and the other session's
// transcripts.
func (r *Repo) blinds(name Spec, role string) (denyRead, allowRead, deny []string) {
	others := []string{string(r.clone(name, other(role))), string(r.tree(name)), string(r.auditTree(name)),
		filepath.Join(r.trees, mainTree), r.top, r.common}
	denyRead = []string{r.trees, r.top, r.common}
	if home, err := os.UserHomeDir(); err == nil {
		transcripts := filepath.Join(home, ".claude", "projects", projectDir(string(r.clone(name, other(role)))))
		others = append(others, transcripts)
		denyRead = append(denyRead, transcripts)
	}

	for _, p := range others {
		deny = append(deny, "Read(/"+p+"/**)")
	}

	return denyRead, []string{string(r.clone(name, role))}, deny
}

var notNamed = regexp.MustCompile(`[^A-Za-z0-9]`)

// projectDir names the directory under ~/.claude/projects/ that holds the transcripts of a session in dir.
func projectDir(dir string) string {
	return notNamed.ReplaceAllString(dir, "-")
}

// converse runs the session l launches, as talk does. The verify session's answer to a failing stack ends with its
// bugs line. A run that ended with no result runs again, up to auditTries times. A reply without the line
// resumes the session once to ask for it, and a second miss stops the build.
func (b *build) converse(ctx context.Context, l claude.Launch, why reason) error {
	ended, err := b.talk(ctx, l, why)
	if err != nil || l.Role != verifyRole {
		return err
	}

	l.Resume = true
	if err := b.rerun(ctx, l, ended); err != nil {
		return err
	}

	return b.askBugsLine(ctx, l)
}

// rerun runs the verify session again while it owes a bugs line and its last run ended with no result. It stops
// after auditTries runs in all.
func (b *build) rerun(ctx context.Context, l claude.Launch, ended claude.Ended) error {
	l.Prompt = interrupted
	for try := 2; b.now().Asked && ended.Failed && try <= auditTries; try++ {
		var err error
		if ended, err = b.talk(ctx, l, reason{Why: whyNoResult, Try: try}); err != nil {
			return err
		}
	}

	return nil
}

// askBugsLine resumes the verify session once to ask for the bugs line it still owes. A second miss stops the build.
func (b *build) askBugsLine(ctx context.Context, l claude.Launch) error {
	if !b.now().Asked {
		return nil
	}

	l.Prompt = askBugs
	if _, err := b.talk(ctx, l, reason{Why: whyBugsLine}); err != nil {
		return err
	}

	if b.now().Asked {
		return errors.New("the verify session's answer to the failing gate named no bugs line, twice")
	}

	return nil
}

// heard reads the reply of a split build's session, where every run of its role ends. The build session's
// cannot-hold line keeps its reply for the verify session. The verify session answers a failing stack with its bugs
// line. That keeps its report for the build session, with the Given, When, Then of each test it names.
func (b *build) heard(ctx context.Context, l claude.Launch, ended claude.Ended) error {
	rec := b.now()
	if rec.Verify == "" || ended.Failed {
		return nil
	}

	switch {
	case l.Role == buildRole:
		if _, found := lastLine(ended.Text, "cannot hold:"); found {
			b.set(func(r *record) { r.ForVerify = strings.TrimSpace(ended.Text) })
		}
	case l.Role == verifyRole && rec.Asked:
		return b.heardBugs(ctx, ended.Text)
	}

	return nil
}

// heardBugs reads the verify session's reply to a failing stack. A bugs line kiln reads keeps its report for the
// build session, and stops kiln asking for the line.
func (b *build) heardBugs(ctx context.Context, reply string) error {
	line, found := lastLine(reply, "bugs:")
	if !found {
		return nil
	}

	report, read, err := b.bugReport(ctx, reply, line)
	if err != nil || !read {
		return err
	}

	b.set(func(r *record) { r.Asked, r.ForBuild = false, report })
	return nil
}

// lastLine is what follows prefix on the reply's last line that starts with it.
func lastLine(text, prefix string) (string, bool) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for _, line := range slices.Backward(lines) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), prefix); ok {
			return strings.TrimSpace(rest), true
		}
	}

	return "", false
}

// bugReport is the prompt that sends the verify session's reply to the build session. The Given, When, Then of
// each test its bugs line names follow it. It is "" for a line that says none. A line kiln can't read, read is
// false, and kiln counts it as missing.
func (b *build) bugReport(ctx context.Context, reply, line string) (report string, read bool, err error) {
	spec, _, err := b.r.specAt(ctx, b.rec.Approved, b.rec.Name)
	if err != nil {
		return "", false, err
	}

	tests := testsOf(string(spec))
	numbers, read := bugsLine(line, tests)
	if !read || len(numbers) == 0 {
		return "", read, nil
	}

	named := make([]string, len(numbers))
	for i, n := range numbers {
		named[i] = tests[n]
	}

	return fmt.Sprintf(bugFound, strings.TrimSpace(reply), strings.Join(named, "\n\n")), true, nil
}

// bugsLine reads what follows a bugs line's "bugs:". It is "none", or the numbers of the spec's tests, separated by
// commas or spaces. It answers each number once, in order. Any other line is unread, so it never passes as none.
func bugsLine(line string, tests map[int]string) (numbers []int, read bool) {
	if line == "none" {
		return nil, true
	}

	for field := range strings.FieldsFuncSeq(line, func(r rune) bool { return r == ',' || r == ' ' }) {
		n, err := strconv.Atoi(field)
		if _, ok := tests[n]; err != nil || !ok {
			return nil, false
		}

		if !slices.Contains(numbers, n) {
			numbers = append(numbers, n)
		}
	}

	return numbers, len(numbers) > 0
}

var numbered = regexp.MustCompile(`(?m)^###\s+(\d+)\.`)

// testsOf is the text of each of a spec's acceptance tests, by its number.
func testsOf(spec string) map[int]string {
	_, tests, _ := strings.Cut(spec, "\n## Acceptance tests")
	tests, _, _ = strings.Cut(tests, "\n## ")
	found := map[int]string{}
	at := numbered.FindAllStringSubmatchIndex(tests, -1)
	for i, m := range at {
		end := len(tests)
		if i+1 < len(at) {
			end = at[i+1][0]
		}

		n, _ := strconv.Atoi(tests[m[2]:m[3]])
		found[n] = strings.TrimSpace(tests[m[0]:end])
	}

	return found
}

// harnessChanged lists each harness file the build session changed. The audit reads each for a weakened check. It
// lists none outside a split build.
func (b *build) harnessChanged(ctx context.Context) ([]string, error) {
	s, err := b.sides(ctx)
	if err != nil || !s.split {
		return nil, err
	}

	tip, err := b.r.git.Tip(ctx, b.rec.Name.sideBranch(buildRole))
	if err != nil {
		return nil, err
	}

	base, err := b.r.git.MergeBase(ctx, mainBranch, tip)
	if err != nil {
		return nil, err
	}

	changed, err := b.r.git.Changed(ctx, base, tip)
	return slices.DeleteFunc(changed, func(p string) bool {
		return !slices.ContainsFunc(s.harness, func(h string) bool { return within(fold(p), fold(h)) })
	}), err
}

// Why a split build's session run started, beside the whys every build shares.
const (
	whyStack      = "stack"
	whyBug        = "bug"
	whyCannotHold = "cannot hold"
	whyNoResult   = "no result"
	whyBugsLine   = "bugs line"
)

const (
	startingSplit = "Build %s, approved at %s. Your checkout is on its branch, %s, made from main. A verify session " +
		"writes the acceptance tests apart. When the build is done, end your turn: kiln commits what you leave, " +
		"and gates it."
	verifying = "Write the acceptance tests of %s, approved at %s. Your checkout is on its branch, %s, made from " +
		"main. A build session writes the code apart. When the tests are written, end your turn: kiln commits " +
		"what you leave, and gates it with the build's code."
	stackFailed = "The gate failed on your tests stacked on the build session's code, so it comes back to you. Fix " +
		"each test at fault, then end your reply with its bugs line: `bugs: none`, or `bugs: ` and the numbers " +
		"of the tests whose failure is the code's fault. Say above it, for each, what the spec expects and what " +
		"the program did.\n\n%s"
	askBugs = "Your reply named no bugs line. End your reply with one: `bugs: none`, or `bugs: ` and the numbers " +
		"of the failing tests whose failure is the code's fault."
	bugFound = "The verify session found a bug in your code. Its report follows, then the Given, When, Then of " +
		"each test it fails. Fix the code, and end your turn.\n\n%s\n\n%s"
	cannotHold = "The build session says these acceptance tests can't hold. Answer with commits alone: an " +
		"amendment, or fixed tests. Then end your turn.\n\n%s"
	stackConflict = "Your tests conflict with the build session's work when stacked, in these files:\n%s\n\nChange " +
		"your side so it merges, commit, and end your turn."
	auditedBuild = "The audit read the work of both sessions. Fix each finding in your code, or say in your reply " +
		"why it is wrong. Then end your turn.\n\n%s"
	auditedVerify = "The audit read the work of both sessions, your tests beside their Given, When, Then. Answer " +
		"each finding on your tests with commits alone: fixed tests, or an amendment. Then end your turn.\n\n%s"
	amendedNote = "The spec's acceptance tests changed since you were last told: the verify session amended them, or " +
		"withdrew its amendments. The spec now reads:\n\n%s"
)
