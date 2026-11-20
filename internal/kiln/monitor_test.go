package kiln

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/tui"
)

// TestMonitorConfig holds which monitors kiln.json may name.
func TestMonitorConfig(t *testing.T) {
	for name, tc := range map[string]struct {
		monitors, acceptance string
		refused              bool
	}{
		"a command":                    {monitors: `{"name": "deps", "every": "24h", "command": "true"}`},
		"an agent, by its file":        {monitors: `{"name": "deps", "every": "1h", "agent": "triage.md"}`},
		"an agent, by its path":        {monitors: `{"name": "deps", "every": "1h", "agent": ".kiln/agents/triage.md"}`},
		"no name":                      {monitors: `{"every": "24h", "command": "true"}`, refused: true},
		"a name no spec could have":    {monitors: `{"name": "Deps", "every": "24h", "command": "true"}`, refused: true},
		"a name another monitor has":   {monitors: `{"name": "a", "every": "1h", "command": "x"}, {"name": "a", "every": "2h", "command": "y"}`, refused: true},
		"under an hour":                {monitors: `{"name": "deps", "every": "30m", "command": "true"}`, refused: true},
		"not whole hours":              {monitors: `{"name": "deps", "every": "90m", "command": "true"}`, refused: true},
		"no duration":                  {monitors: `{"name": "deps", "every": "daily", "command": "true"}`, refused: true},
		"a check alone":                {monitors: `{"name": "deps", "every": "24h", "check": "true"}`, refused: true},
		"an agent outside .kiln":       {monitors: `{"name": "deps", "every": "24h", "agent": "../x.md"}`, refused: true},
		"the audit as its agent":       {monitors: `{"name": "deps", "every": "24h", "agent": "audit.md"}`, refused: true},
		"tests kiln could not guard":   {monitors: `{"name": "deps", "every": "24h", "command": "true"}`, acceptance: `"{spec}_test/"`, refused: true},
		"tests the monitor may write":  {monitors: `{"name": "deps", "every": "24h", "command": "true", "tests": true}`, acceptance: `"{spec}_test/"`},
		"tests under no path to guard": {monitors: `{"name": "deps", "every": "24h", "command": "true"}`, acceptance: `[]`, refused: true},
		"tests under one of two paths": {monitors: `{"name": "deps", "every": "24h", "command": "true"}`, acceptance: `["uat/{spec}/", "{spec}_test/"]`, refused: true},
		"a gauge":                      {monitors: `{"name": "deps", "every": "24h", "gauge": "health", "agent": "triage.md"}`},
		"a gauge gauges lacks":         {monitors: `{"name": "deps", "every": "24h", "gauge": "size", "agent": "triage.md"}`, refused: true},
		"a check and a gauge":          {monitors: `{"name": "deps", "every": "24h", "check": "true", "gauge": "health", "agent": "triage.md"}`, refused: true},
		"gates beside a gauge":         {monitors: `{"name": "deps", "every": "24h", "gauge": "health", "gates": true, "agent": "triage.md"}`, refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			acceptance := cmpOr(tc.acceptance, `"acceptance/{spec}/"`)
			raw := `{"gate": ["true"], "rounds": 1, "acceptance": ` + acceptance + `, "gauges": [{"name": ` +
				`"health", "command": "true", "min": 10}], "monitors": [` + tc.monitors + `]}`
			if _, err := parseConfig([]byte(raw)); (err != nil) != tc.refused {
				t.Errorf("refused %v, want %v", err, tc.refused)
			}
		})
	}
}

// TestDue holds when a monitor is due. It is when it never ran, its check couldn't run, or a usage limit ended its
// fix. It is when its fix was cut short. Otherwise it is due once its interval, less half an hour, has passed.
func TestDue(t *testing.T) {
	ran := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	daily := monitor{Name: "deps", Every: "24h"}
	for name, tc := range map[string]struct {
		found, outcome string
		later          time.Duration
		due, past      bool
	}{
		"healthy a moment ago":              {found: foundHealthy, later: time.Minute},
		"its check ran past the timeout":    {found: foundCouldNot, past: true, later: time.Minute},
		"that, its interval later":          {found: foundCouldNot, past: true, later: 24 * time.Hour, due: true},
		"healthy, its interval less slack":  {found: foundHealthy, later: 23*time.Hour + 30*time.Minute, due: true},
		"healthy, just short of that":       {found: foundHealthy, later: 23*time.Hour + 29*time.Minute},
		"its check couldn't run":            {found: foundCouldNot, later: time.Minute, due: true},
		"held a moment ago":                 {found: foundHeld, later: time.Minute},
		"a fix a usage limit ended":         {found: foundFix, outcome: outLimit, later: time.Minute, due: true},
		"a fix cut short":                   {found: foundFix, outcome: outDropped, later: time.Minute, due: true},
		"a gated check couldn't run":        {found: foundFix, outcome: outCouldNot, later: time.Minute, due: true},
		"a session that failed":             {found: foundFix, outcome: outFailed, later: time.Minute},
		"a session that failed, a day ago":  {found: foundFix, outcome: outFailed, later: 24 * time.Hour, due: true},
		"a fix that landed a moment ago":    {found: foundFix, outcome: outLanded, later: time.Minute},
		"a fix that stopped, its interval":  {found: foundFix, outcome: outStopped, later: 24 * time.Hour, due: true},
		"a fix waiting on the developer":    {found: foundFix, outcome: outWaits, later: time.Minute},
		"never run":                         {due: true},
		"a fix that changed nothing, today": {found: foundFix, outcome: outUnchanged, later: time.Hour},
	} {
		t.Run(name, func(t *testing.T) {
			var entries []entry
			if tc.found != "" {
				line := entry{Kind: monitorKind, Monitor: "deps", At: ran, Found: tc.found, Build: "b", Past: tc.past}
				entries = append(entries, line)
			}

			if tc.outcome != "" {
				entries = append(entries, entry{Kind: outcomeKind, Build: "b", Outcome: tc.outcome})
			}

			if due := histories(entries)["deps"].due(daily, ran.Add(tc.later)); due != tc.due {
				t.Errorf("due %v, want %v", due, tc.due)
			}
		})
	}
}

// TestStuck holds that a check or gauge past the timeout holds its monitor until main or its entry changes. A run
// held for it keeps it held, and any other run since releases it.
func TestStuck(t *testing.T) {
	stuck := entry{Kind: monitorKind, Monitor: "m", Main: "a", Entry: "e", Found: foundCouldNot, Past: true}
	heldFor := entry{Kind: monitorKind, Monitor: "m", Main: "a", Entry: "e", Found: foundHeld, Past: true}
	healthy := entry{Kind: monitorKind, Monitor: "m", Main: "a", Entry: "e", Found: foundHealthy}
	for name, tc := range map[string]struct {
		lines     []entry
		main, ent string
		held      bool
	}{
		"the same main and entry":     {lines: []entry{stuck}, main: "a", ent: "e", held: true},
		"main moved":                  {lines: []entry{stuck}, main: "b", ent: "e"},
		"its entry changed":           {lines: []entry{stuck}, main: "a", ent: "f"},
		"a run held for it since":     {lines: []entry{stuck, heldFor}, main: "a", ent: "e", held: true},
		"a run that finished since":   {lines: []entry{stuck, healthy}, main: "a", ent: "e"},
		"a check that never ran past": {lines: []entry{healthy}, main: "a", ent: "e"},
	} {
		now := entry{Main: git.Rev(tc.main), Entry: tc.ent}
		if held := histories(tc.lines)["m"].stuckAt(now); held != tc.held {
			t.Errorf("%s: held %v, want %v", name, held, tc.held)
		}
	}
}

// TestHold holds what releases a monitor its last fix holds. That is a change to main's tip, its entry, or what its
// check or gauge printed. The fix's outcome and the monitor's check or gauge say which.
func TestHold(t *testing.T) {
	gated := monitor{Name: "m", Every: "1h", Check: "c", Command: "x"}
	outside := monitor{Name: "m", Every: "1h", Check: "c", Agent: "a.md", Gates: new(false)}
	unchecked := monitor{Name: "m", Every: "1h", Agent: "a.md"}
	gauged := monitor{Name: "m", Every: "1h", Gauge: "g", Agent: "a.md"}
	for name, tc := range map[string]struct {
		m                    monitor
		outcome              string
		main, entry, printed bool
		held                 bool
	}{
		"stopped, nothing changed":           {m: gated, outcome: outStopped, held: true},
		"stopped, main moved":                {m: gated, outcome: outStopped, main: true},
		"stopped, its entry changed":         {m: gated, outcome: outStopped, entry: true},
		"stopped, its check printed another": {m: gated, outcome: outStopped, printed: true},
		"rejected, nothing changed":          {m: gated, outcome: outRejected, held: true},
		"rejected, main moved":               {m: gated, outcome: outRejected, main: true},
		"asked, main moved":                  {m: outside, outcome: outNeedsYou, main: true, held: true},
		"asked, its check printed another":   {m: outside, outcome: outNeedsYou, printed: true},
		"asked, its entry changed":           {m: outside, outcome: outNeedsYou, entry: true},
		"asked with no check, main moved":    {m: unchecked, outcome: outNeedsYou, main: true},
		"asked with no check, nothing moved": {m: unchecked, outcome: outNeedsYou, held: true},
		"unchanged, main moved":              {m: outside, outcome: outUnchanged, main: true, held: true},
		"unchanged, its check printed other": {m: outside, outcome: outUnchanged, printed: true},
		"unchanged with no check":            {m: unchecked, outcome: outUnchanged},
		"landed, its check out of the gate":  {m: outside, outcome: outLanded, main: true, held: true},
		"landed, and its check printed more": {m: outside, outcome: outLanded, printed: true},
		"landed, its check in the gate":      {m: gated, outcome: outLanded},
		"its session failed":                 {m: gated, outcome: outFailed},
		"a usage limit":                      {m: gated, outcome: outLimit},
		"cut short":                          {m: gated, outcome: outDropped},
		"its gated check couldn't run":       {m: gated, outcome: outCouldNot},
		"landed, its gauge printed the same": {m: gauged, outcome: outLanded, main: true, held: true},
		"landed, its gauge printed another":  {m: gauged, outcome: outLanded, main: true, printed: true},
		"landed, its gauge's entry changed":  {m: gauged, outcome: outLanded, main: true, entry: true},
		"unchanged, its gauge the same":      {m: gauged, outcome: outUnchanged, main: true, held: true},
		"asked, its gauge the same":          {m: gauged, outcome: outNeedsYou, main: true, held: true},
	} {
		t.Run(name, func(t *testing.T) {
			was := entry{Kind: monitorKind, Monitor: "m", Found: foundFix, Build: "b", Main: "a", Entry: "e",
				Printed: "p"}
			now := entry{Kind: monitorKind, Monitor: "m", Main: "a", Entry: "e", Printed: "p"}
			for _, change := range []struct {
				changed bool
				field   *string
			}{{tc.main, (*string)(&now.Main)}, {tc.entry, &now.Entry}, {tc.printed, &now.Printed}} {
				if change.changed {
					*change.field += "'"
				}
			}

			h := histories([]entry{was, {Kind: outcomeKind, Build: "b", Outcome: tc.outcome}})["m"]
			if held := h.holds(tc.m, now) != ""; held != tc.held {
				t.Errorf("held %v, want %v", held, tc.held)
			}
		})
	}
}

// TestCheck holds how kiln reads a check: its last line of stdout says whether to fix. A non-zero exit, or a last
// line that is no such object, couldn't run. kiln then keeps the last line it printed on either stream.
func TestCheck(t *testing.T) {
	for name, tc := range map[string]struct {
		command        string
		ran, fix       bool
		summary, found string
		limit          time.Duration
	}{
		"healthy":                {command: `echo '{"fix": false, "summary": "fine"}'`, ran: true, summary: "fine"},
		"a finding above it":     {command: `echo CVE-1; echo '{"fix": true, "summary": "one"}'`, ran: true, fix: true, summary: "one", found: "CVE-1"},
		"progress on stderr":     {command: `echo working >&2; echo '{"fix": true, "summary": "s"}'`, ran: true, fix: true, summary: "s"},
		"a non-zero exit":        {command: `echo '{"fix": true, "summary": "s"}'; sleep 0.2; echo dial tcp: refused >&2; exit 1`, summary: "dial tcp: refused"},
		"a last line of text":    {command: `echo all good`, summary: "all good"},
		"an object with no fix":  {command: `echo '{"summary": "s"}'`, summary: `{"summary": "s"}`},
		"nothing at all":         {command: `true`, summary: "the check printed nothing"},
		"an exit with no output": {command: `exit 3`, summary: "the check failed: exit status 3"},
		"one that runs past":     {command: `echo '{"fix": true, "summary": "s"}'; sleep 30`, limit: 500 * time.Millisecond, summary: "the check ran past kiln.json's timeout of 500ms, so kiln killed it"},
	} {
		t.Run(name, func(t *testing.T) {
			s := config{Timeout: timeout(tc.limit)}.shell(os.Environ())
			c, err := s.runMonitorCheck(t.Context(), git.Worktree(t.TempDir()), tc.command)
			if err != nil {
				t.Fatal(err)
			}

			type found struct {
				ran, fix       bool
				summary, found string
			}
			got := found{ran: c.ran, fix: c.fix, summary: c.summary, found: c.found}
			want := found{ran: tc.ran, fix: tc.fix, summary: tc.summary, found: tc.found}
			if got != want {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

// TestAsk holds how kiln reads an agent's reply that asks for the developer. It holds how a session's end ends a fix.
func TestAsk(t *testing.T) {
	for name, tc := range map[string]struct {
		text    string
		failed  bool
		outcome string
		summary string
	}{
		"a reply that asks":         {text: "It needs a person.\n{\"ask\": true, \"summary\": \"look\"}", outcome: outNeedsYou, summary: "look"},
		"a reply that fixed it":     {text: "Done."},
		"an ask that is false":      {text: `{"ask": false, "summary": "no"}`},
		"a session that failed":     {text: "OAuth token has expired", failed: true, outcome: outFailed, summary: "OAuth token has expired"},
		"a failure with no result":  {failed: true, outcome: outFailed, summary: "kiln: not signed in"},
		"an ask inside other lines": {text: "{\"ask\": true, \"summary\": \"x\"}\nThen I fixed it."},
	} {
		t.Run(name, func(t *testing.T) {
			f := &fixing{}
			ended := f.heard(claudeEnded(tc.text, tc.failed), "line 1\nkiln: not signed in\n")
			type ending struct {
				ended            bool
				outcome, summary string
			}
			got, want := ending{ended, f.outcome, f.summary}, ending{tc.outcome != "", tc.outcome, tc.summary}
			if got != want {
				t.Errorf("got %+v, want %+v", got, want)
			}

			if tc.outcome == outNeedsYou && !strings.Contains(f.report, "needs a person") {
				t.Errorf("the report is %q, want the reply above its last line", f.report)
			}
		})
	}
}

// TestTitle holds which titles an agent's reply gives the commit that lands its fix.
func TestTitle(t *testing.T) {
	for name, tc := range map[string]struct{ text, want string }{
		"a title on the last line": {text: "Fixed it.\n{\"title\": \"fix(tui): keep each row linear\"}",
			want: "fix(tui): keep each row linear"},
		"one with no scope":         {text: `{"title": "perf: cache the gauge rows"}`, want: "perf: cache the gauge rows"},
		"a reply with no title":     {text: "Fixed it."},
		"a title above other lines": {text: "{\"title\": \"fix(tui): x\"}\nThen I checked it."},
		"a title of 73 characters":  {text: `{"title": "fix(tui): ` + strings.Repeat("x", 63) + `"}`},
		"a title of 72 characters": {text: `{"title": "fix(tui): ` + strings.Repeat("x", 62) + `"}`,
			want: "fix(tui): " + strings.Repeat("x", 62)},
		"a title with no type": {text: `{"title": "Fixed the gauge rows"}`},
		"a title of two lines": {text: `{"title": "fix(tui): x\n\nmore"}`},
	} {
		t.Run(name, func(t *testing.T) {
			if got := titleOf(tc.text); got != tc.want {
				t.Errorf("titleOf = %q, want %q", got, tc.want)
			}
		})
	}
}

// claudeEnded is a session's end, with text as its result.
func claudeEnded(text string, failed bool) claude.Ended {
	return claude.Ended{Text: text, Failed: failed}
}

// TestNamedRun holds that kiln monitor run <name> runs held or not, but never starts a second fix of a monitor.
func TestNamedRun(t *testing.T) {
	m := monitor{Name: "m", Every: "1h", Check: "c", Command: "x"}
	afterStop := histories([]entry{{Kind: monitorKind, Monitor: "m", Found: foundFix, Build: "b"},
		{Kind: outcomeKind, Build: "b", Outcome: outStopped}})["m"]
	for name, tc := range map[string]struct {
		named bool
		rec   *record
		held  bool
	}{
		"due, after a fix that stopped":     {rec: &record{Name: "m", State: stopped, Monitor: true}, held: true},
		"named, after a fix that stopped":   {named: true, rec: &record{Name: "m", State: stopped, Monitor: true}},
		"named, while a fix waits":          {named: true, rec: &record{State: waiting, Monitor: true}, held: true},
		"named, while an accepted fix is":   {named: true, rec: &record{State: queued, Accepted: true}, held: true},
		"named, with a stopped landing":     {named: true, rec: &record{State: stopped, Accepted: true}, held: true},
		"due, while a fix waits, unchanged": {rec: &record{State: waiting, Monitor: true}, held: true},
	} {
		t.Run(name, func(t *testing.T) {
			run := &monitorRunning{named: tc.named}
			if held := run.held(m, afterStop, tc.rec, entry{}); held != tc.held {
				t.Errorf("held %v, want %v", held, tc.held)
			}
		})
	}
}

// TestAcceptedLandingStops holds that an accepted fix's landing only lands or stops. An error of any kind stops it,
// so its record and branch stay for kiln accept. So does a session that asks for the developer, or fails, with its
// summary and report as why. Only an interrupt leaves it as it was.
func TestAcceptedLandingStops(t *testing.T) {
	for name, tc := range map[string]struct {
		accepted bool
		err      error
		outcome  string
		why      string
	}{
		"a landing whose check couldn't run":  {accepted: true, err: &checkFailed{line: "dial tcp"}, why: "dial tcp"},
		"a landing kiln could not go on with": {accepted: true, err: errors.New("no disk"), why: "no disk"},
		"a landing whose session asks":        {accepted: true, outcome: outNeedsYou, why: "look\nthe report"},
		"a landing whose session fails":       {accepted: true, outcome: outFailed, why: "look\nthe report"},
		"a landing that landed":               {accepted: true, outcome: outLanded},
		"a fix, not accepted, that couldn't":  {err: &checkFailed{line: "dial tcp"}},
		"a fix, not accepted, that asks":      {outcome: outNeedsYou},
	} {
		t.Run(name, func(t *testing.T) {
			b := &build{rec: record{Accepted: tc.accepted}, fix: &fixing{}}
			b.fix.end(tc.outcome, "look", "the report")
			stop, stops := errors.AsType[*outOfRounds](b.landingStops(t.Context(), tc.err))
			why := ""
			if stops {
				why = stop.why
			}

			if stops != (tc.why != "") || !strings.HasSuffix(why, tc.why) {
				t.Errorf("stops %v, as %q; want a stop %v, as %q", stops, why, tc.why != "", tc.why)
			}
		})
	}
}

// TestMonitorStatusLines holds what kiln status shows below a monitor's line. A stopped fix shows what failed. The
// next step follows the fix's record where the book holds one, such as an accepted landing that stopped.
func TestMonitorStatusLines(t *testing.T) {
	stoppedFix := standing{outcome: outStopped, summary: "3 updates",
		report: "the gate failed:\nacceptance/a/x refused"}
	for name, tc := range map[string]struct {
		s    standing
		rec  *record
		want string
	}{
		"a stopped fix shows what failed": {s: stoppedFix, rec: &record{Name: "m", State: stopped, Monitor: true},
			want: "acceptance/a/x refused"},
		"and is run again by name": {s: stoppedFix, rec: &record{Name: "m", State: stopped, Monitor: true},
			want: "kiln monitor run m"},
		"an accepted landing that stopped is accepted again": {s: stoppedFix,
			rec: &record{Name: "m", State: stopped, Accepted: true, Monitor: true}, want: "kiln accept m"},
		"a waiting fix is accepted or rejected": {s: standing{outcome: outWaits},
			rec: &record{Name: "m", State: waiting, Monitor: true}, want: "kiln reject m"},
		"an agent's report stays out of status": {s: standing{outcome: outNeedsYou, summary: "look",
			report: "the long report"}, want: "kiln monitor run m"},
	} {
		t.Run(name, func(t *testing.T) {
			lines := strings.Join(tc.s.lines("m", tc.rec, "/trees/m"), "\n")
			if !strings.Contains(lines, tc.want) {
				t.Errorf("%q holds no %q", lines, tc.want)
			}

			if tc.s.outcome == outNeedsYou && strings.Contains(lines, tc.s.report) {
				t.Errorf("%q holds the agent's report", lines)
			}
		})
	}
}

// TestStoppedWhyOnce holds that a stopped fix's ledger line from before the fix split why still shows each line once.
func TestStoppedWhyOnce(t *testing.T) {
	first := "main changed what the fix changed, in these files:"
	h := histories([]entry{{Kind: monitorKind, Monitor: "m", Found: foundFix, Build: "b"},
		{Kind: outcomeKind, Build: "b", Outcome: outStopped, Summary: first, Report: first + "\ndocs.txt"}})["m"]
	lines := strings.Join(h.standing(monitor{Name: "m"}, nil).lines("m", nil, "/trees/m"), "\n")
	if strings.Count(lines, first) != 1 || !strings.Contains(lines, "docs.txt") {
		t.Errorf("%q holds %q other than once, or no docs.txt", lines, first)
	}
}

// TestAcceptedLanding holds that a monitor with an accepted fix stands as the book holds the fix. The ledger still
// says the fix waits.
func TestAcceptedLanding(t *testing.T) {
	h := histories([]entry{{Kind: monitorKind, Monitor: "docs", Found: foundFix, Build: "b"},
		{Kind: outcomeKind, Build: "b", Outcome: outWaits}})["docs"]
	for name, tc := range map[string]struct {
		landing state
		want    tui.State
	}{
		"its landing queued": {landing: queued, want: tui.Queued},
		"its landing builds": {landing: building, want: tui.Halted},
	} {
		t.Run(name, func(t *testing.T) {
			s := h.standing(monitor{Name: "docs"}, &record{Name: "docs", State: tc.landing, Accepted: true,
				Monitor: true, Build: "b"})
			if got := s.state(); got != tc.want || strings.Contains(s.result, "waiting") {
				t.Errorf("stands %q, as %q; want %q, and no wait on the developer", got, s.result, tc.want)
			}
		})
	}
}

// TestLandingSession holds that kiln status names why the session of an accepted fix's landing runs, while it runs.
func TestLandingSession(t *testing.T) {
	conflict := entry{Kind: startKind, Build: "b", Session: "s",
		Why: whyConflict, Round: 1, Rounds: 3}
	for name, tc := range map[string]struct {
		lines []entry
		want  string
	}{
		"while the session works":  {lines: []entry{conflict}, want: "accepted, landing: conflict with main, round 1 of 3"},
		"once its turn has ended":  {lines: []entry{conflict, {Kind: endKind, Build: "b", Session: "s"}}, want: "accepted, landing"},
		"before the session works": {want: "accepted, landing"},
	} {
		t.Run(name, func(t *testing.T) {
			entries := append([]entry{{Kind: monitorKind, Monitor: "docs", Found: foundFix, Build: "b"},
				{Kind: outcomeKind, Build: "b", Outcome: outWaits}}, tc.lines...)
			rec := &record{Name: "docs", State: building, Accepted: true, Monitor: true, Build: "b",
				PID: os.Getpid()}
			if got := histories(entries)["docs"].standing(monitor{Name: "docs"}, rec).result; got != tc.want {
				t.Errorf("kiln status shows %q; want %q", got, tc.want)
			}
		})
	}
}
