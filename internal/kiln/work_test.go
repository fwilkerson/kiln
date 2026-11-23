package kiln

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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
