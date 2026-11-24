package kiln

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/fwilkerson/kiln/internal/git"
)

// LedgerOptions are the lines of the ledger kiln ledger prints. Each field left zero keeps every line.
type LedgerOptions struct {
	// Kind keeps the lines of one kind, such as gate.
	Kind string
	// Name keeps the lines of that name: a spec's builds, and a monitor's runs and fixes.
	Name Spec
	// Last keeps the last lines, of those Kind and Name keep. 0 keeps them all.
	Last int
}

// Ledger prints the ledger to out, one JSON object a line, oldest first, as opt keeps them. The package doc lists
// each kind of line and its fields. Ledger takes no lock. It refuses a kind the doc does not list, with ErrUsage.
func (r *Repo) Ledger(out io.Writer, opt LedgerOptions) error {
	if opt.Kind != "" && !slices.Contains(printedKinds, opt.Kind) {
		return fmt.Errorf("%w: kiln ledger prints no kind %s, only %s", ErrUsage, opt.Kind,
			strings.Join(printedKinds, ", "))
	}

	entries, err := r.store.entries()
	if err != nil {
		return err
	}

	lines := slices.DeleteFunc(printable(entries), func(p printed) bool { return !opt.keeps(p) })
	if opt.Last > 0 {
		lines = lines[max(0, len(lines)-opt.Last):]
	}

	encoder := json.NewEncoder(out)
	for _, p := range lines {
		if err := encoder.Encode(p.line); err != nil {
			return err
		}
	}

	return nil
}

// keeps reports whether p is of the kind and the name opt keeps.
func (opt LedgerOptions) keeps(p printed) bool {
	return cmpOr(opt.Kind, p.kind) == p.kind && cmpOr(opt.Name, p.name) == p.name
}

// printedKinds are the kinds of line kiln ledger prints, as the package doc lists them.
var printedKinds = []string{"build", "run", "gate", "reading", "finding", "monitor"}

// printed is one line kiln ledger prints: its JSON object, and what LedgerOptions and the order read.
type printed struct {
	kind string
	name Spec
	at   time.Time
	line any
}

// printable is each line kiln ledger prints of entries, ordered by when each started. A line of a build the ledger
// holds no build line of counts for nothing, as collect says.
func printable(entries []entry) []printed {
	var lines []printed
	for _, s := range collect(entries) {
		lines = append(lines, s.printed()...)
	}

	for _, e := range entries {
		if e.Kind == monitorKind {
			lines = append(lines, printed{"monitor", e.Monitor, e.At, e.printedMonitor()})
		}
	}

	slices.SortStableFunc(lines, func(a, b printed) int { return a.at.Compare(b.at) })
	return lines
}

// printed is the build's lines: the build, then its runs with their findings, its gates and its readings.
func (s *buildStats) printed() []printed {
	lines := []printed{{"build", s.Spec, s.At, s.printedBuild()}}
	for _, r := range s.runs {
		lines = append(lines, printed{"run", s.Spec, r.start, s.printedRun(r)})
		for _, f := range r.findings {
			lines = append(lines, printed{"finding", s.Spec, r.end, s.printedFinding(r, f)})
		}
	}

	for _, g := range s.gates {
		lines = append(lines, printed{"gate", s.Spec, g.At, printedGate{Kind: "gate", Build: s.Build, Name: s.Spec,
			At: stamp(g.At), End: stamp(g.End), Seconds: seconds(g.End.Sub(g.At)), Passed: g.Passed,
			Landing: g.Landing, RanMain: g.RanMain, Proof: g.Proof}})
	}

	for _, e := range s.readings {
		lines = append(lines, printed{"reading", s.Spec, e.At, printedReading{Kind: "reading", Build: s.Build,
			Name: s.Spec, Gauge: e.Gauge, At: stamp(e.At), Landed: e.Landed, Value: e.Value,
			Crossed: e.crossed(e.Value)}})
	}

	return lines
}

// printedBuild is a build, or a monitor's fix, as kiln ledger prints it.
type printedBuild struct {
	Kind        string   `json:"kind"`
	Build       string   `json:"build"`
	Name        Spec     `json:"name"`
	Fix         bool     `json:"fix"`
	At          stamp    `json:"at"`
	Size        int      `json:"size"`
	Outcome     *string  `json:"outcome"`
	Seconds     *float64 `json:"seconds"`
	CostUSD     *float64 `json:"cost_usd"`
	Turns       *int     `json:"turns"`
	FailedCalls *int     `json:"failed_calls"`
	Rounds      *int     `json:"rounds"`
	Audits      int      `json:"audits"`
	Findings    int      `json:"findings"`
}

func (s *buildStats) printedBuild() printedBuild {
	n := s.sums()
	audits, findings := auditReads(s.runs)
	return printedBuild{Kind: "build", Build: s.Build, Name: s.Spec, Fix: s.Monitor != "", At: stamp(s.At),
		Size: s.Size, Outcome: orNull(s.outcome), Seconds: secondsOf(n.took), CostUSD: n.cost, Turns: n.turns,
		FailedCalls: n.failed, Rounds: n.rounds, Audits: audits, Findings: findings}
}

// printedRun is a session run, as kiln ledger prints it.
type printedRun struct {
	Kind        string   `json:"kind"`
	Build       string   `json:"build"`
	Name        Spec     `json:"name"`
	Role        string   `json:"role"`
	Session     string   `json:"session"`
	Why         *string  `json:"why"`
	Round       *int     `json:"round"`
	At          stamp    `json:"at"`
	End         *stamp   `json:"end"`
	Seconds     *float64 `json:"seconds"`
	Ended       *string  `json:"ended"`
	CostUSD     *float64 `json:"cost_usd"`
	Turns       *int     `json:"turns"`
	FailedCalls *int     `json:"failed_calls"`
	Found       *int     `json:"found"`
	Reported    *int     `json:"reported"`
}

// printedRun is r, a run of the build, as kiln ledger prints it. Its numbers add up as a build's do.
func (s *buildStats) printedRun(r run) printedRun {
	n := total([]run{r}, nil).sums(nil)
	p := printedRun{Kind: "run", Build: s.Build, Name: s.Spec, Role: r.role, Session: r.session,
		Why: orNull(r.why.Why), At: stamp(r.start), Seconds: secondsOf(n.took), Ended: orNull(r.ended),
		CostUSD: n.cost, Turns: n.turns, FailedCalls: n.failed}
	if p.Why != nil {
		p.Round = &r.why.Round
	}

	if !r.end.IsZero() {
		p.End = new(stamp(r.end))
	}

	if r.tally != nil {
		p.Found, p.Reported = &r.tally.Found, &r.tally.Reported
	}

	return p
}

// printedGate is a run of the gate, as kiln ledger prints it.
type printedGate struct {
	Kind    string  `json:"kind"`
	Build   string  `json:"build"`
	Name    Spec    `json:"name"`
	At      stamp   `json:"at"`
	End     stamp   `json:"end"`
	Seconds float64 `json:"seconds"`
	Passed  bool    `json:"passed"`
	Landing bool    `json:"landing"`
	RanMain *bool   `json:"ran_main"`
	Proof   bool    `json:"proof"`
}

// printedReading is a gauge's reading of a landing, as kiln ledger prints it.
type printedReading struct {
	Kind    string   `json:"kind"`
	Build   string   `json:"build"`
	Name    Spec     `json:"name"`
	Gauge   string   `json:"gauge"`
	At      stamp    `json:"at"`
	Landed  git.Rev  `json:"landed"`
	Value   *float64 `json:"value"`
	Crossed bool     `json:"crossed"`
}

// printedFinding is a finding an audit reported, or an audit's reply kiln could not parse, as kiln ledger prints it.
type printedFinding struct {
	Kind     string  `json:"kind"`
	Build    string  `json:"build"`
	Name     Spec    `json:"name"`
	Fix      bool    `json:"fix"`
	Session  string  `json:"session"`
	At       stamp   `json:"at"`
	Parsed   bool    `json:"parsed"`
	Rank     *int    `json:"rank"`
	Path     *string `json:"path"`
	Line     *int    `json:"line"`
	Category *string `json:"category"`
	Reaches  *string `json:"reaches"`
	Chance   *string `json:"chance"`
	Harm     *string `json:"harm"`
	Design   *string `json:"design"`
	Patch    *string `json:"patch"`
	Proof    *string `json:"proof"`
	Reply    *string `json:"reply"`
}

// printedFinding is f, a finding of the build's audit run r, as kiln ledger prints it.
func (s *buildStats) printedFinding(r run, f finding) printedFinding {
	p := printedFinding{Kind: "finding", Build: s.Build, Name: s.Spec, Fix: s.Monitor != "", Session: r.session,
		At: stamp(r.end), Parsed: f.Parsed, Path: orNull(f.Path), Category: orNull(f.Category),
		Reaches: orNull(f.Reaches), Chance: orNull(f.Chance), Harm: orNull(f.Harm), Design: orNull(f.Design),
		Patch: orNull(f.Patch), Proof: orNull(f.Proof), Reply: orNull(f.Reply)}
	if p.Parsed {
		p.Rank = &f.Rank
	}

	if f.Line > 0 {
		p.Line = &f.Line
	}

	return p
}

// printedMonitor is a run of a monitor, as kiln ledger prints it.
type printedMonitor struct {
	Kind    string   `json:"kind"`
	Name    Spec     `json:"name"`
	At      stamp    `json:"at"`
	Main    git.Rev  `json:"main"`
	Found   string   `json:"found"`
	Build   *string  `json:"build"`
	Summary string   `json:"summary"`
	Gauge   *string  `json:"gauge"`
	Value   *float64 `json:"value"`
	Crossed bool     `json:"crossed"`
}

func (e entry) printedMonitor() printedMonitor {
	return printedMonitor{Kind: "monitor", Name: e.Monitor, At: stamp(e.At), Main: e.Main, Found: e.Found,
		Build: orNull(e.Build), Summary: e.Summary, Gauge: orNull(e.Gauge), Value: e.Value, Crossed: e.crossed(e.Value)}
}

// stamp is a time as kiln ledger prints it: RFC 3339 in UTC, to the second, which jq's fromdate reads.
type stamp time.Time

func (s stamp) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Time(s).UTC().Format(time.RFC3339))
}

// seconds is d in seconds, to the millisecond.
func seconds(d time.Duration) float64 {
	// Duration.Seconds adds the fraction to the whole seconds, which prints 3.929 as 3.9290000000000003.
	return float64(d.Round(time.Millisecond).Milliseconds()) / 1000
}

// secondsOf is d in seconds, or nil when d is unknown.
func secondsOf(d *time.Duration) *float64 {
	if d == nil {
		return nil
	}

	return new(seconds(*d))
}

// orNull is s, or nil when s is "", which JSON prints as null.
func orNull(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}
