package kiln

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
)

// A verify-first fix runs two sessions in turn, as a monitor.json of {"workflow": "verify-first"} asks:
//
//	main ──▶ kiln/<name>.verify   verify.md's session writes test files alone, from the check's finding, in a clone
//	gate, without the check ── passes, or fails on more than tests ──▶ the verify session, as a round
//	tests ──▶ kiln/<name>         agent.md's session, a new one, writes the fix, and no test file
//	audit ── findings ──▶ the verify session, its gate again, a merge into kiln/<name>, then the fix session
//
// So the session that writes the fix never decides what proves it, and the tests failed at main's code first. The
// verify session never reads the fix's worktree, nor the fix session's transcripts. Once the developer accepted the
// fix, its findings go to the fix session alone.

//go:embed contract/monitor-verify-first.md
var monitorVerifyContract string

// verifying is whether f's verify session works now.
func (f *fixing) verifying() bool {
	return f.side.role == verifyRole
}

// writeTests runs the verify session in a clone of main until the gate fails on its tests alone. It answers the
// commit of those tests, or "" once the session ended the fix.
func (b *build) writeTests(ctx context.Context) (git.Rev, error) {
	name := b.rec.Name
	clone := b.r.clone(name, verifyRole)
	if err := b.r.dropCheckouts(ctx, name); err != nil {
		return "", err
	}

	if err := b.r.git.Clone(ctx, clone, mainBranch, name.sideBranch(verifyRole)); err != nil {
		return "", err
	}

	base, err := b.r.git.Head(ctx, clone)
	if err != nil {
		return "", err
	}

	b.set(func(r *record) { r.VerifyFirst = claude.NewID() })
	b.say("writing the tests on %s, in %s", name.sideBranch(verifyRole), clone)
	tested, report, err := b.verifyTurns(ctx, false, b.fix.verifyPrompt(), reason{Why: whyFirst})
	if err != nil || tested == "" {
		return "", err
	}

	tests, err := b.r.git.Changed(ctx, base, tested)
	b.fix.tested = fmt.Sprintf(testFailed, strings.Join(tests, ", "), report)
	b.set(func(r *record) { r.Rounds = 0 })
	b.say("the test fails at main's code; a session of its own fixes it")
	return tested, err
}

// verifyTurns tells the verify session prompt, for why, in its clone, and resumes it when resume is true. kiln commits
// each turn's work, and gates it, until the gate fails on tests alone. It answers that commit, and the gate's report.
// It answers "" once the session ended the fix. Work that leaves the tests kiln proved needs no gate.
func (b *build) verifyTurns(ctx context.Context, resume bool, prompt string, why reason) (git.Rev, string, error) {
	b.fix.side.role = verifyRole
	for ; ; resume = true {
		l, err := b.verifyLaunch(ctx, resume, prompt)
		if err != nil {
			return "", "", err
		}

		ended, err := b.talk(ctx, l, why)
		if err != nil || b.fix.heard(ended, b.r.claude.Stderr(l.Session)) {
			return "", "", err
		}

		head, report, back, err := b.verifyGate(ctx)
		switch {
		case err != nil:
			return "", "", err
		case back.why == "":
			b.fix.side.role = fixRole
			b.set(func(r *record) { r.Tested = head })
			return head, report, nil
		}

		if prompt, why, err = b.round(back); err != nil {
			return "", "", err
		}
	}
}

// verifyGate commits what the verify session left in its clone, fetches its branch, and gates it there without the
// check. It answers the commit, and the gate's report once the gate failed on tests alone. Otherwise it answers what
// goes back to the session. Work that leaves the tests kiln proved as they were needs no gate.
func (b *build) verifyGate(ctx context.Context) (git.Rev, string, setback, error) {
	g := b.verifyGuard()
	head, err := b.r.git.Commit(ctx, g.place, b.message("the verify session's work"))
	if err == nil {
		err = b.r.git.Fetch(ctx, g.place, b.rec.Name.sideBranch(verifyRole))
	}

	if err != nil || head == b.now().Tested {
		return head, "", setback{}, err
	}

	g.head = head
	v, err := b.judgeWork(ctx, nil, g)
	switch {
	case err != nil:
		return "", "", setback{}, err
	case v.passed:
		return head, "", setback{why: whyNoTest, report: testPassed}, nil
	case !v.failsOnTests():
		return head, "", setback{why: whyNoTest, report: fmt.Sprintf(testBroke, v.report), failing: v.failing}, nil
	}

	return head, v.report, setback{}, nil
}

// verifyGuard is the guard on the verify session's work, in its clone. It keeps the session to test files.
func (b *build) verifyGuard() guard {
	return guard{spec: b.rec.Name, place: b.r.clone(b.rec.Name, verifyRole), side: b.fix.side}
}

// verifyLaunch is a run of the verify session, in its clone. Its sandbox denies it reads of the fix's worktree, and of
// the fix session's transcripts.
func (b *build) verifyLaunch(ctx context.Context, resume bool, prompt string) (claude.Launch, error) {
	g := b.verifyGuard()
	l := claude.Launch{Session: b.now().VerifyFirst, Resume: resume, Prompt: prompt,
		System: fmt.Sprintf(monitorVerifyContract, b.rec.Name, b.fix.side.testWords())}
	l.DenyRead, l.AllowRead, l.Deny = b.r.blinds(b.rec.Name, g.place, b.tree)
	return b.monitorLaunch(ctx, verifyAgent, g, l)
}

// sendFindings answers what the fix session is told of the audit's findings, and why. In a verify-first fix the
// developer has not accepted, the verify session answers them first. kiln gates its tests again, and merges them into
// the fix's branch. A landing's findings go to the fix session alone, since the developer accepted its tests.
func (b *build) sendFindings(ctx context.Context, findings string, why reason) (string, reason, error) {
	if !b.fix.verifierAnswers(b.rec.Accepted) {
		return fmt.Sprintf(fixAudited, findings), why, nil
	}

	tested, _, err := b.verifyTurns(ctx, true, fmt.Sprintf(testsAudited, findings), why)
	if err != nil || tested == "" {
		return "", reason{}, err
	}

	if err := b.mergeTests(ctx); err != nil {
		return "", reason{}, err
	}

	return fmt.Sprintf(fixAuditedTests, findings), why, nil
}

// mergeTests merges the verify session's branch into the fix's. A conflict stops the fix, since neither session may
// write what the other wrote.
func (b *build) mergeTests(ctx context.Context) error {
	branch := b.rec.Name.sideBranch(verifyRole)
	conflict, err := b.r.git.Merge(ctx, b.tree, branch, b.message("merge the verify session's answer to the audit"))
	if err != nil || conflict == nil {
		return err
	}

	return conflictStop(testsConflict, conflict.Files)
}

// verifyPrompt is the verify session's first message. It holds what the monitor's check printed, and nothing of a
// fix.
func (f *fixing) verifyPrompt() string {
	m := f.monitor
	found := fmt.Sprintf(foundBy[checkScript], m.path(checkScript), f.checked.summary, cmpOr(f.checked.found,
		"(nothing)"))
	return found + "\n\n" + fmt.Sprintf(verifyWhat, m.Name)
}

// verifierAnswers is whether the verify session of f, a fix or nil, answers the audit. It does in a verify-first
// fix, until the developer accepts the fix and its tests.
func (f *fixing) verifierAnswers(accepted bool) bool {
	return f != nil && !accepted && f.monitor.Workflow == verifyFirst
}

// auditNote is what the audit is told of f's tests, where f's verify session answers the audit, or "".
func (f *fixing) auditNote(accepted bool) string {
	if !f.verifierAnswers(accepted) {
		return ""
	}

	return fmt.Sprintf(auditingTests, strings.TrimSpace(f.checked.summary+"\n\n"+f.checked.found))
}

const (
	verifyWhat = "Write the tests that fail for what the monitor %s found, as your instructions say, and end your " +
		"turn. A session of its own then writes the fix."
	testPassed = "The gate passed with your work, so no test fails on what the monitor found. Write a test that " +
		"fails on main's code for that reason, and end your turn."
	testBroke = "The gate failed, but not on tests alone: a command failed with no test named, or a path is refused. " +
		"Make it fail only on a test that shows what the monitor found, and end your turn.\n\n%s"
	testsConflict = "the verify session's tests conflict with the fix, in these files:\n"
	testFailed    = "A session before yours wrote tests of it, in %s, and your branch starts from them. The gate " +
		"fails on them at main, as it must until the fix:\n\n%s"
	testsAudited = "The audit read the work: your tests, and the fix another session wrote. Answer each finding " +
		"on your tests with commits to the tests alone, and leave each finding on the code to the fix session. " +
		"Then end your turn: kiln gates your tests again.\n\n%s"
	fixAuditedTests = "The audit read the directories the fix and the tests changed. The verify session answered " +
		"its findings on the tests, and kiln merged its answer here. Fix each finding on the code, or say in your " +
		"reply why it is wrong. Then end your turn.\n\n%s"
	auditingTests = " A session of its own wrote the tests from what the monitor's check found, and kiln proved " +
		"they fail at main. Read each test against that finding, which follows.\n\n%s"
)
