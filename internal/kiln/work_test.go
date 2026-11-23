package kiln

import (
	"slices"
	"testing"
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
