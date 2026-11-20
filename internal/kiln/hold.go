package kiln

import (
	"time"
)

// history is a monitor's runs, as the ledger holds them.
type history struct {
	// last is its last monitor line, and lastFix the last that started a fix. Each is the zero entry while none is.
	last, lastFix entry
	// outcomes holds the last outcome line of each of its fixes' builds.
	outcomes map[string]entry
	// running holds the start line of each of its fixes' session runs that has no end line yet, by build.
	running map[string]entry
	// stuck is its last monitor line whose check or gauge ran past kiln.json's timeout, while each line since is Past.
	stuck entry
}

// histories reads each monitor's history from the ledger, by its name.
func histories(entries []entry) map[Spec]*history {
	all := map[Spec]*history{}
	of := func(name Spec) *history {
		if all[name] == nil {
			all[name] = &history{outcomes: map[string]entry{}, running: map[string]entry{}}
		}

		return all[name]
	}

	fixes := map[string]Spec{}
	for _, e := range entries {
		switch {
		case e.Kind == monitorKind:
			h := of(e.Monitor)
			h.last = e
			h.stuck = stuckAfter(h.stuck, e)
			if e.Found == foundFix {
				h.lastFix = e
				fixes[e.Build] = e.Monitor
			}
		case e.Kind == outcomeKind && fixes[e.Build] != "":
			of(fixes[e.Build]).outcomes[e.Build] = e
		case e.Kind == startKind && fixes[e.Build] != "":
			of(fixes[e.Build]).running[e.Build] = e
		case e.Kind == endKind && fixes[e.Build] != "":
			h := of(fixes[e.Build])
			if h.running[e.Build].Session == e.Session {
				delete(h.running, e.Build)
			}
		}
	}

	return all
}

// result is what the run of the monitor line e came to: what it found, or its fix's outcome. A fix with no outcome
// line yet is building, or was cut short.
func (h *history) result(e entry) string {
	if e.Found != foundFix {
		return e.Found
	}

	return h.outcomes[e.Build].Outcome
}

// fixSummary is what the fix that the monitor line e started says of itself: its outcome's summary, or its check's.
func (h *history) fixSummary(e entry) string {
	return cmpOr(h.outcomes[e.Build].Summary, e.Summary)
}

// slack is how much short of its interval a monitor's last run may have started, with the monitor still due. It takes
// in launchd's hourly tick, which lands a few seconds either side of the hour. It takes in a run a long fix held up.
const slack = 30 * time.Minute

// due is whether m is due at now. It is due when it never ran, or its last run couldn't run its check. It is due when
// that run met a usage limit, or its fix was cut short. Otherwise, it is due once its interval less slack has passed.
func (h *history) due(m monitor, now time.Time) bool {
	if h == nil || h.last.Kind == "" {
		return true
	}

	switch h.result(h.last) {
	case outLimit, outDropped:
		return true
	case foundCouldNot:
		// One that ran past the timeout would hold commands.lock as long again, so it waits for its interval.
		return !h.last.Past || !now.Before(h.next(m))
	}

	return !now.Before(h.next(m))
}

// stuckAfter is the stuck line of a history whose stuck line was stuck, once the monitor line e follows it.
func stuckAfter(stuck, e entry) entry {
	switch {
	case !e.Past:
		return entry{}
	case e.Found == foundCouldNot:
		return e
	}

	return stuck
}

// stuckAt is whether the monitor's check or gauge ran past kiln.json's timeout at the main and entry now holds. Its
// run would hang again on the same input, so the monitor is held until one of them changes.
func (h *history) stuckAt(now entry) bool {
	if h == nil || h.stuck.Kind == "" {
		return false
	}

	return h.stuck.Main == now.Main && h.stuck.Entry == now.Entry
}

// next is when m is due again, once its last run's interval less slack has passed.
func (h *history) next(m monitor) time.Time {
	return h.last.At.Add(m.interval() - slack)
}

// holds says what keeps m from starting a fix, run as the monitor line now says. That is the outcome of its last fix.
// It answers "" when nothing holds it. A change since that fix started releases it, as holding says.
func (h *history) holds(m monitor, now entry) string {
	if h == nil || h.lastFix.Kind == "" {
		return ""
	}

	was := h.lastFix
	outcome := h.result(was)
	held := holding[outcome]
	c := change{main: was.Main != now.Main, entry: was.Entry != now.Entry, printed: was.Printed != now.Printed}
	if held == nil || !held(m, c) {
		return ""
	}

	return outcome
}

// change is what changed since a monitor's last fix started: main's tip, its entry, or what it printed.
type change struct {
	main, entry, printed bool
}

// finding is whether the monitor's finding is the same: its entry, and what its check or gauge printed.
func (c change) finding() bool {
	return !c.entry && !c.printed
}

// holding says, by the outcome of a monitor's last fix, whether that fix holds the monitor after c.
var holding = map[string]func(m monitor, c change) bool{
	outStopped:  stillStopped,
	outRejected: stillStopped,
	// An hourly monitor that finds the same errors asks once. With no finding, main's tip is all that can change.
	outNeedsYou: func(m monitor, c change) bool {
		if !m.finds() {
			return c.finding() && !c.main
		}

		return c.finding()
	},
	// An agent that judged the finding noise runs once for it.
	outUnchanged: func(m monitor, c change) bool {
		return m.finds() && c.finding()
	},
	// A check out of the gate can't see a fix until it is deployed. A gauge in the gate read only the fix's change.
	// So a fix that left the repository's reading as it was runs once.
	outLanded: func(m monitor, c change) bool {
		switch {
		case m.Gauge != "":
			return c.finding()
		case m.Check == "" || m.gated():
			return false
		}

		return !c.printed
	},
}

// stillStopped holds a monitor whose fix stopped or was rejected, until main's tip or its finding changes.
func stillStopped(_ monitor, c change) bool {
	return c.finding() && !c.main
}
