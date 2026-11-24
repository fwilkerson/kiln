package kiln

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/practices/claim"
)

// TestDueSeats holds which sessions run next, by the build's order, what kiln owes each, and whether a proof stood. The
// verify session runs whenever kiln owes it. Once a proof stands, the code session waits while kiln owes the verify
// session. In turn, it waits for the first proof too.
func TestDueSeats(t *testing.T) {
	for name, tc := range map[string]struct {
		order               order
		code, verify, proof bool
		want                []seatName
	}{
		"alone, owed":                          {order: alone, code: true, want: []seatName{codeSeat}},
		"alone, owed nothing":                  {order: alone},
		"side by side, at the start":           {order: sideBySide, code: true, verify: true, want: []seatName{verifySeat, codeSeat}},
		"side by side, before a proof":         {order: sideBySide, code: true, want: []seatName{codeSeat}},
		"side by side, the verify session's":   {order: sideBySide, code: true, verify: true, proof: true, want: []seatName{verifySeat}},
		"side by side, once the verify answer": {order: sideBySide, code: true, proof: true, want: []seatName{codeSeat}},
		"in turn, at the start":                {order: inTurn, code: true, verify: true, want: []seatName{verifySeat}},
		"in turn, the code waits for a proof":  {order: inTurn, code: true},
		"in turn, once proved":                 {order: inTurn, code: true, proof: true, want: []seatName{codeSeat}},
		"in turn, the verify session first":    {order: inTurn, code: true, verify: true, proof: true, want: []seatName{verifySeat}},
	} {
		t.Run(name, func(t *testing.T) {
			b := &build{order: tc.order}
			for s, owed := range map[seatName]bool{codeSeat: tc.code, verifySeat: tc.verify} {
				if owed {
					owe(&b.rec, s, message{Text: "go on"})
				}
			}

			if tc.proof {
				b.rec.Tested = "proved"
			}

			if got := b.due(); !slices.Equal(got, tc.want) {
				t.Errorf("due names %q, want %q", got, tc.want)
			}
		})
	}
}

// TestProvedThenCode holds what kiln does with a verify session's later commit that changes a path it may not write.
// The commit never joins the work. It goes back to the verify session as a round, with the refusal. A first proof
// does the same. This holds whether or not the commit changes a test, for a spec's verify session and a fix's.
func TestProvedThenCode(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		fix bool
		// test is a test the commit changes beside the code, or "".
		test string
	}{
		"a spec's verify session changes code alone":    {},
		"a spec's verify session changes code and test": {test: "uat/x/x_test.go"},
		"a fix's verify session changes code alone":     {fix: true},
		"a fix's verify session changes code and test":  {fix: true, test: "x_test.go"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b, _ := splitBuild(t, false)
			b.side.acceptance = []string{"uat/x/"}
			if tc.fix {
				b.fix = &fixing{side: side{role: fixRole, tests: []string{"*_test.go"}}}
				b.set(func(r *record) { r.Monitor = true })
			}

			proved := b.now().Tested
			clone := string(b.r.clone("x", verifyRole))
			script := "echo 'package x' >doc.go"
			if tc.test != "" {
				script += " && mkdir -p " + filepath.Dir(tc.test) + " && echo 'package x' >" + tc.test
			}

			shell(t, clone, script)
			stands, err := b.prove(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			refusal := b.verifyGuard().guarded("doc.go")
			owed := b.now().Verify.Owed
			told := len(owed) == 1 && strings.Contains(owed[0].Text, refusal)
			_, joinErr := os.Stat(b.path("doc.go"))
			if !claim.Holds(!stands, b.now().Tested == proved, joinErr != nil, told, refusal != "") {
				t.Errorf("prove answered %v; the proved tests are %s, want %s; the work holds doc.go: %v. The verify "+
					"session is told %+v, want one message with %q.", stands, b.now().Tested, proved, joinErr == nil,
					owed, refusal)
			}
		})
	}
}

// TestRefusedCodeTakenOut holds that a verify session answers a refusal by taking out the refused code. Its commit
// keeps the test it changed beside that code. kiln then proves the commit again, and never stops the build as though
// the session changed nothing.
func TestRefusedCodeTakenOut(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		fix  bool
		test string
	}{
		"a spec's verify session": {test: "uat/x/x_test.go"},
		"a fix's verify session":  {fix: true, test: "x_test.go"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b, repo := splitBuild(t, false)
			shell(t, repo, `echo '{"gate": ["true"], "rounds": 3}' >.kiln/kiln.json && git commit --quiet -am rounds`)
			b.side.acceptance = []string{"uat/x/"}
			if tc.fix {
				b.fix = &fixing{side: side{role: fixRole, tests: []string{"*_test.go"}}}
				b.set(func(r *record) { r.Monitor = true })
			}

			clone := string(b.r.clone("x", verifyRole))
			shell(t, clone, "echo 'package x' >doc.go && mkdir -p "+filepath.Dir(tc.test)+" && echo 'package x' >"+
				tc.test)
			if _, err := b.prove(t.Context()); err != nil {
				t.Fatal(err)
			}

			b.set(func(r *record) { paid(r, verifySeat, len(r.Verify.Owed)) })
			shell(t, clone, "rm doc.go")
			_, err := b.prove(t.Context())
			if rounds := b.now().Verify.Rounds; !claim.Holds(err == nil, rounds == 2) {
				t.Errorf("once the session took out doc.go, prove answered %v, and the verify session had %d "+
					"rounds; want no error, and a second round", err, rounds)
			}
		})
	}
}

// TestFailedVerifyRun holds that only a verify session's run that ended with a result can answer by changing nothing.
// After a proof that did not stand, a run that failed gets the proof's report again, as its next round. Past the last
// round, the build stops as any setback there does. A run that answered with no change stops it as unproved.
func TestFailedVerifyRun(t *testing.T) {
	t.Parallel()
	const failed = `{"type":"result","is_error":true,"result":"API Error: 500 Internal server error"}`
	for name, tc := range map[string]struct {
		reply  string
		rounds int
		// stops is whether the build stops, and kind the kind of its stop.
		stops bool
		kind  stopKind
	}{
		"a run that failed is told the report again": {reply: failed, rounds: 3},
		"a run that failed in the last round stops":  {reply: failed, rounds: 1, stops: true},
		"a run that answered stops the build": {rounds: 3, stops: true, kind: stopUnproved,
			reply: `{"type":"result","is_error":false,"result":"Main already does it."}`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b, report := disprovedBuild(t, tc.rounds)
			replies(t, b, tc.reply)
			if _, err := b.tell(t.Context(), []seatName{verifySeat}); err != nil {
				t.Fatal(err)
			}

			_, err := b.prove(t.Context())
			stopped, _ := errors.AsType[*outOfRounds](err)
			var kind stopKind
			if stopped != nil {
				kind = stopped.Kind
			}

			want := message{Text: report.Text, Why: report.Why, Round: 2, Rounds: tc.rounds}
			owed, rounds := b.now().Verify.Owed, b.now().Verify.Rounds
			told := slices.Contains(owed, want) && rounds == 2
			if !claim.Holds((stopped != nil) == tc.stops, kind == tc.kind, told != tc.stops) {
				t.Errorf("after the verify session's run, prove answered %v, and kiln owes the session %+v, with %d "+
					"rounds used. Want a stop: %v, of kind %q; else %+v owed, with 2 rounds used.", err, owed,
					rounds, tc.stops, tc.kind, want)
			}
		})
	}
}

// TestDisprovedAtLastRound holds that a proof that did not stand past the last round still records its commit as
// unproved. A verify session that then changes nothing stops the build as unproved, and its proof's gate never runs
// again.
func TestDisprovedAtLastRound(t *testing.T) {
	t.Parallel()
	b, _ := disprovedBuild(t, 1)
	b.set(func(r *record) { paid(r, verifySeat, len(r.Verify.Owed)) })
	clone := string(b.r.clone("x", verifyRole))
	shell(t, clone, "echo 'package x' >uat/x/y_test.go")
	_, err := b.prove(t.Context())
	if stopped, _ := errors.AsType[*outOfRounds](err); stopped == nil || stopped.Kind == stopUnproved {
		t.Fatalf("a second proof that did not stand, past the last round, answered %v; want a stop of its round", err)
	}

	_, err = b.prove(t.Context())
	if stopped, _ := errors.AsType[*outOfRounds](err); stopped == nil || stopped.Kind != stopUnproved {
		t.Errorf("once the verify session changed nothing after that stop, prove answered %v, and kiln owes it %+v; "+
			"want the build stopped as unproved", err, b.now().Verify.Owed)
	}
}

// TestPassedProofWayOut holds what a proof whose gate passed tells the verify session, where main's code already does
// the work. A spec's verify session changes nothing, which stops the build. A fix's asks for the developer, as its
// contract says, so the developer reads why no test can fail.
func TestPassedProofWayOut(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		fix         bool
		want, never string
	}{
		"a spec's verify session changes nothing":       {want: "change nothing", never: "ask for the developer"},
		"a fix's verify session asks for the developer": {fix: true, want: "ask for the developer", never: "change nothing"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b, _ := disprovedBuild(t, 3)
			if tc.fix {
				b.fix = &fixing{side: side{role: fixRole, tests: []string{"*_test.go"}}}
				b.job = b.fix
				b.set(func(r *record) { r.Monitor = true })
			}

			b.set(func(r *record) { paid(r, verifySeat, len(r.Verify.Owed)) })
			shell(t, string(b.r.clone("x", verifyRole)), "echo 'package x' >uat/x/y_test.go")
			if _, err := b.prove(t.Context()); err != nil {
				t.Fatal(err)
			}

			owed := b.now().Verify.Owed
			text := ""
			if len(owed) == 1 {
				text = owed[0].Text
			}

			if !claim.Holds(strings.Contains(text, tc.want), !strings.Contains(text, tc.never)) {
				t.Errorf("a proof whose gate passed owes the verify session %+v; want one message that says %q, and "+
					"never %q", owed, tc.want, tc.never)
			}
		})
	}
}

// disprovedBuild is splitBuild's build of x after a proof that did not stand. kiln.json gives it rounds rounds, and
// the verify session's test passes on main's code. It answers the proof's report, which kiln owes the session.
func disprovedBuild(t *testing.T, rounds int) (*build, message) {
	b, repo := splitBuild(t, false)
	shell(t, repo, fmt.Sprintf(`echo '{"gate": ["true"], "rounds": %d}' >.kiln/kiln.json && `, rounds)+
		`mkdir -p .kiln/agents && echo 'Write the tests.' >.kiln/agents/verify.md && git add -A && `+
		`git commit --quiet -m rounds`)
	b.set(func(r *record) {
		r.Approved, r.Verify.Session = git.Rev(shell(t, repo, "git rev-parse HEAD")), claude.NewID()
	})

	b.side.acceptance = []string{"uat/x/"}
	shell(t, string(b.r.clone("x", verifyRole)), "mkdir -p uat/x && echo 'package x' >uat/x/x_test.go")
	if _, err := b.prove(t.Context()); err != nil {
		t.Fatal(err)
	}

	owed := b.now().Verify.Owed
	if len(owed) != 1 || owed[0].Round != 1 {
		t.Fatalf("the first proof owes the verify session %+v, want its report as round 1", owed)
	}

	return b, owed[0]
}

// replies makes b's claude a program that prints reply, a stream line, to each run.
func replies(t *testing.T, b *build, reply string) {
	program := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(program, []byte("#!/bin/sh\ncat >/dev/null\necho '"+reply+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	b.r.claude = claude.New(claude.Config{Program: program, Sessions: filepath.Join(t.TempDir(), "sessions")})
}

// TestRunToldTwoRounds holds a run told two messages: the audit's findings, then a conflict of round 1. The build
// line of kiln ledger keeps both: the round kiln fire printed, and the findings the audit sent back. kiln status says
// so too.
func TestRunToldTwoRounds(t *testing.T) {
	t.Parallel()
	b, repo := splitBuild(t, false)
	shell(t, repo, `mkdir -p .kiln/agents && echo 'Build it.' >.kiln/agents/build.md && git add -A && `+
		`git commit --quiet -m agent`)
	b.set(func(r *record) {
		r.Approved, r.Code.Session = git.Rev(shell(t, repo, "git rev-parse HEAD")), claude.NewID()
		r.Told = r.Tested
		owe(r, codeSeat, message{Text: "the findings", Why: whyFindings})
		owe(r, codeSeat, message{Text: "the conflict", Why: whyConflict, Round: 1, Rounds: 3})
	})

	b.set(nil, entry{Kind: builtKind, Spec: "x"})
	b.set(nil, entry{Kind: startKind, Role: auditRole, Session: claude.NewID(), Why: whyAudit, Try: 1})
	replies(t, b, `{"type":"result","is_error":false,"result":"done"}`)
	if _, err := b.tell(t.Context(), []seatName{codeSeat}); err != nil {
		t.Fatal(err)
	}

	builds := collect(ledgerLines(t, b.r.store))
	if len(builds) != 1 {
		t.Fatalf("the ledger holds %d builds, want 1", len(builds))
	}

	runs := builds[0].runs
	told := slices.DeleteFunc(slices.Clone(runs), func(r run) bool { return r.role != buildRole })
	line, did := builds[0].printedBuild(), auditDid(true, runs, true)
	if !claim.Holds(len(told) == 1, line.Rounds != nil && *line.Rounds == 1, line.Audits == 1, line.Findings == 1,
		did == "the audit sent findings back") {
		t.Errorf("a run told the findings, then a conflict of round 1 of 3, left %d build runs, a build line of %v "+
			"rounds, %d audits and %d findings, and kiln status says %q; want 1 run, 1 round, 1 audit and 1 finding, "+
			"and that the audit sent findings back", len(told), deref(line.Rounds), line.Audits, line.Findings, did)
	}
}

// deref is what p points to, or nil.
func deref(p *int) any {
	if p == nil {
		return nil
	}

	return *p
}
