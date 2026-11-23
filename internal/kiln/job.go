package kiln

import (
	"context"
	"fmt"
	"strings"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
)

// job is what a build works for: a spec, or a monitor's fix. Each supplies what differs between them, and how its
// work ends. specJob is a spec's, and fixing a fix's.
type job interface {
	// launch is a run of the session at the seat s, told prompt. The job's agent, guard and contract shape it.
	launch(ctx context.Context, b *build, s seatName, prompt string) (claude.Launch, error)
	// heard reads how a run of the session at s ended, and answers what its reply asks.
	heard(b *build, s seatName, ended claude.Ended) answer
	// asks says what the verify session's tests show, for a proof that did not stand.
	asks() string
	// gates is whether the work at head needs the gate and the audit before it passes.
	gates(ctx context.Context, b *build, head git.Rev) (bool, error)
	// audited runs the audit, and owes its findings to the sessions. It answers whether it owed any.
	audited(ctx context.Context, b *build) (bool, error)
	// passed takes the work at head to its end: it lands or waits. Or it answers what kept it back.
	passed(ctx context.Context, b *build, head git.Rev) (setback, error)
	// goneBack sends back what kept the work at its end back, as a round of the code session's.
	goneBack(ctx context.Context, b *build, back setback) error
}

// specJob is a spec's build.
type specJob struct{}

// start makes a spec's checkouts from main's tip, and owes each session its first message. Its sessions, given IDs in
// the write that owes them, then show that it started. A kill before leaves no session, and the next kiln starts again.
func (b *build) start(ctx context.Context, s sides) error {
	name := b.rec.Name
	if err := b.r.dropCheckouts(ctx, name); err != nil {
		return err
	}

	if err := b.checkoutMain(ctx); err != nil {
		return err
	}

	isolated := b.order.isolated()
	if err := b.cloneVerify(ctx); err != nil {
		return err
	}

	old, err := b.sideBySideStart(ctx, s.acceptance)
	if err != nil {
		return err
	}

	first, verify := b.firstPrompts()
	b.set(func(r *record) {
		r.Code, r.OldTests = seat{Session: claude.NewID()}, old
		owe(r, codeSeat, message{Text: first, Why: whyFirst})
		if isolated {
			r.Verify = seat{Session: claude.NewID()}
			owe(r, verifySeat, message{Text: verify, Why: whyFirst})
		}
	})

	b.saysWhere()
	return nil
}

// cloneVerify makes the verify session's clone from main's tip, where the order runs a verify session.
func (b *build) cloneVerify(ctx context.Context) error {
	if !b.order.isolated() {
		return nil
	}

	name := b.rec.Name
	return b.r.git.Clone(ctx, b.r.clone(name, verifyRole), mainBranch, name.sideBranch(verifyRole))
}

// saysWhere says where each session of the build works.
func (b *build) saysWhere() {
	name := b.rec.Name
	where := fmt.Sprintf("on %s, in %s", name.branch(), b.tree)
	if b.order.isolated() {
		where += fmt.Sprintf(", and verifying on %s, in %s", name.sideBranch(verifyRole), b.r.clone(name, verifyRole))
	}

	if b.fix != nil {
		b.say("fixing %s", where)
		return
	}

	b.say("building %s", where)
}

// firstPrompts are the first messages of a spec's code session and verify session.
func (b *build) firstPrompts() (code, verify string) {
	name, approved := b.rec.Name, short(b.rec.Approved)
	code = fmt.Sprintf(starting, name.file(), approved, name.branch())
	if b.order.isolated() {
		code = fmt.Sprintf(startingSplit, name.file(), approved, name.branch())
	}

	return code, fmt.Sprintf(verifying, name.file(), approved, name.sideBranch(verifyRole))
}

func (specJob) launch(ctx context.Context, b *build, s seatName, prompt string) (claude.Launch, error) {
	rec := b.now()
	g, rival := b.treeGuard(), b.r.clone(rec.Name, verifyRole)
	if s == verifySeat {
		g, rival = b.verifyGuard(), b.tree
	}

	l := claude.Launch{Session: rec.seat(s).Session, Prompt: prompt, Dir: string(g.place),
		Hook: g.hookCommand(b.r.exe)}
	if b.order.isolated() {
		l.DenyRead, l.AllowRead, l.Deny = b.r.blinds(rec.Name, g.place, rival)
	}

	return b.agentLaunch(ctx, b.roleOf(s), l)
}

// heard keeps the code session's reply for the verify session, when its last line says a test cannot hold.
func (specJob) heard(b *build, s seatName, ended claude.Ended) answer {
	if s == verifySeat || !b.order.isolated() {
		return answer{}
	}

	if _, found := lastLine(ended.Text, "cannot hold:"); !found || ended.Failed {
		return answer{}
	}

	text := fmt.Sprintf(cannotHold, strings.TrimSpace(ended.Text))
	return answer{owed: &message{Text: text, Why: whyCannotHold}}
}

func (specJob) asks() string { return "what the spec asks" }

func (specJob) gates(context.Context, *build, git.Rev) (bool, error) { return true, nil }

// audited owes the audit's findings to the build session. Beside a verify session, it owes them to that session
// first, and due runs it first.
func (specJob) audited(ctx context.Context, b *build) (bool, error) {
	isolated := b.order.isolated()
	return b.findings(ctx, func(r *record, findings string, why reason) {
		if !isolated {
			owe(r, codeSeat, message{Text: fmt.Sprintf(audited, findings), reason: why})
			return
		}

		owe(r, verifySeat, message{Text: fmt.Sprintf(auditedVerify, findings), reason: why})
		owe(r, codeSeat, message{Text: fmt.Sprintf(auditedBuild, findings), reason: why})
	})
}

func (specJob) passed(ctx context.Context, b *build, _ git.Rev) (setback, error) {
	return b.waitOrLand(ctx)
}

// goneBack sends a landing's setback back to the build session, as sendBack says.
func (specJob) goneBack(ctx context.Context, b *build, back setback) error {
	return b.sendBack(ctx, back)
}

// sideOf is what the session at the seat s may write. A spec's build with one session has the zero side.
func (b *build) sideOf(s seatName) side {
	if b.fix != nil {
		return b.fix.sideOf(s)
	}

	if b.side == nil || !b.side.order.isolated() {
		return side{}
	}

	role := map[seatName]string{codeSeat: buildRole, verifySeat: verifyRole}[s]
	return side{role: role, acceptance: b.side.acceptance, helpers: b.side.helpers}
}

// Why a session run started, beside the reasons every build shares.
const whyCannotHold = "cannot hold"

const (
	startingSplit = "Build %s, approved at %s. Your worktree is on its branch, %s, made from main. A verify session " +
		"writes the acceptance tests apart, and kiln joins them into your branch once it proves they fail on " +
		"main. When the build is done, end your turn: kiln commits what you leave, and gates it."
	verifying = "Write the acceptance tests of %s, approved at %s. Your checkout is on its branch, %s, made from " +
		"main. A build session writes the code apart. When the tests are written, end your turn: kiln commits " +
		"what you leave, and proves the gate fails on them at main's code."
	cannotHold = "The build session says these acceptance tests can't hold. Answer with commits alone: an " +
		"amendment, or fixed tests. Then end your turn.\n\n%s"
	auditedBuild = "The audit read the work of both sessions. The verify session answered its findings on the " +
		"tests first, and kiln joined its answer here. Fix each finding in your code, or say in your reply why it " +
		"is wrong. Then end your turn.\n\n%s"
	auditedVerify = "The audit read the work of both sessions, your tests beside their Given, When, Then. Answer " +
		"each finding on your tests with commits alone: fixed tests, or an amendment. Leave each finding on the " +
		"code to the build session. Then end your turn.\n\n%s"
	amendedNote = "The spec's acceptance tests changed since you were last told: the verify session amended them, or " +
		"withdrew its amendments. The spec now reads:\n\n%s"
)
