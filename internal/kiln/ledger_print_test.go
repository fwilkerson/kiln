package kiln

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/tui"
)

// ledgerRepo is a repository whose ledger holds a landed split build, x, and a build still running, y. It holds two
// runs of the monitor gate-time, and the fix the second started. It holds a run of code-health, which reads a gauge.
func ledgerRepo(t *testing.T) *Repo {
	base := time.Date(2026, 11, 18, 5, 0, 0, 0, time.UTC)
	at := func(minutes float64) time.Time { return base.Add(time.Duration(minutes * float64(time.Minute))) }
	totals := func(turns int, cost float64, failed int) *claude.Totals {
		return &claude.Totals{Turns: &turns, CostUSD: &cost, Failed: make([]claude.Failure, failed)}
	}

	seven, ten, five := 7.0, 10.0, 5.0
	r := &Repo{store: store{dir: t.TempDir()}}
	for _, e := range []entry{
		{Kind: builtKind, Build: "b1", At: at(0), Spec: "x", Size: 2},
		{Kind: startKind, Build: "b1", At: at(1), Role: buildRole, Session: "s1", Why: whyFirst},
		{Kind: endKind, Build: "b1", At: at(3), Session: "s1", Ended: endedResult, Totals: totals(2, 0.5, 1)},
		{Kind: gateKind, Build: "b1", At: at(3), End: at(4), RanMain: new(true)},
		{Kind: startKind, Build: "b1", At: at(4), Role: buildRole, Session: "s1",
			Why: whyGate, Round: 1, Rounds: 3},
		{Kind: endKind, Build: "b1", At: at(5), Session: "s1", Ended: endedResult, Totals: totals(1, 0.25, 0)},
		{Kind: gateKind, Build: "b1", At: at(5), End: at(6), Passed: true, RanMain: new(false)},
		{Kind: startKind, Build: "b1", At: at(6), Role: auditRole, Session: "s2", Why: whyAudit, Try: 1},
		{Kind: endKind, Build: "b1", At: at(7), Session: "s2", Ended: endedResult, Totals: totals(1, 0.125, 0)},
		{Kind: startKind, Build: "b1", At: at(7), Role: buildRole, Session: "s1", Why: whyFindings},
		{Kind: startKind, Build: "b1", At: at(7), Role: verifyRole, Session: "s4", Why: whyFindings},
		{Kind: endKind, Build: "b1", At: at(8), Session: "s1", Ended: endedResult, Totals: totals(1, 0.125, 0)},
		{Kind: endKind, Build: "b1", At: at(8), Session: "s4", Ended: endedResult, Totals: totals(1, 0, 0)},
		{Kind: gateKind, Build: "b1", At: at(8), End: at(9), Passed: true, Landing: true, RanMain: new(false)},
		{Kind: outcomeKind, Build: "b1", At: at(9), Outcome: outLanded},
		{Kind: gaugeKind, Build: "b1", At: at(10), Gauge: "files", Landed: "abc123", Value: &seven,
			AtMost: &five},
		{Kind: monitorKind, At: at(11), Monitor: "gate-time", Main: "abc123", Found: foundHealthy,
			Summary: "within 50%"},
		{Kind: monitorKind, At: at(12), Monitor: "code-health", Main: "abc123", Found: foundHealthy,
			Summary: "10", Gauge: "health", Value: &ten, AtLeast: &ten},
		{Kind: monitorKind, Build: "b3", At: at(20), Monitor: "gate-time", Main: "abc123", Found: foundFix,
			Summary: "the gate slowed 60%"},
		{Kind: builtKind, Build: "b3", At: at(20.5), Spec: "gate-time", Monitor: "gate-time"},
		{Kind: gateKind, Build: "b3", At: at(22), End: at(23), Passed: true},
		{Kind: builtKind, Build: "b2", At: at(21), Spec: "y", Size: 1},
		{Kind: startKind, Build: "b2", At: at(21), Role: buildRole, Session: "s3"},
		{Kind: gateKind, Build: "zz", At: at(24), End: at(25), Passed: true},
	} {
		if err := r.store.write(e); err != nil {
			t.Fatal(err)
		}
	}

	return r
}

// Lines that more than one row of TestLedger prints.
const (
	buildY = `{"kind":"build","build":"b2","name":"y","fix":false,"at":"2026-11-18T05:21:00Z","size":1,` +
		`"outcome":null,"seconds":null,"cost_usd":null,"turns":null,"failed_calls":null,"rounds":null,"audits":0,` +
		`"findings":0}`
	runY = `{"kind":"run","build":"b2","name":"y","role":"build","session":"s3","at":"2026-11-18T05:21:00Z",` +
		`"end":null,"seconds":null,"ended":null,"cost_usd":null,"turns":null,"failed_calls":null}`
	landingX = `{"kind":"gate","build":"b1","name":"x","at":"2026-11-18T05:08:00Z","end":"2026-11-18T05:09:00Z",` +
		`"seconds":60,"passed":true,"landing":true,"ran_main":false}`
	fixGate = `{"kind":"gate","build":"b3","name":"gate-time","at":"2026-11-18T05:22:00Z",` +
		`"end":"2026-11-18T05:23:00Z","seconds":60,"passed":true,"landing":false,"ran_main":null}`
)

// TestLedger holds what kiln ledger prints of a ledger. A build sums its runs and gates, and a sum is null while a
// run lacks its part. A split build's findings count once. Lines come oldest first by when each started, whatever
// order kiln wrote them in. A line of a build with no build line is left out. An unknown kind is refused.
func TestLedger(t *testing.T) {
	r := ledgerRepo(t)
	for _, tc := range []struct {
		name string
		opt  LedgerOptions
		want string
	}{
		{"a build sums its runs and gates, and counts its rounds, the audit's reads and its findings sent back",
			LedgerOptions{Kind: "build", Name: "x"},
			`{"kind":"build","build":"b1","name":"x","fix":false,"at":"2026-11-18T05:00:00Z","size":2,` +
				`"outcome":"landed","seconds":540,"cost_usd":1,"turns":6,"failed_calls":1,"rounds":1,"audits":1,` +
				`"findings":1}`},
		{"a build whose run has not ended, and gives no reason, knows none of its sums",
			LedgerOptions{Kind: "build", Name: "y"}, buildY},
		{"a run that ended", LedgerOptions{Kind: "run", Name: "x", Last: 1},
			`{"kind":"run","build":"b1","name":"x","role":"verify","session":"s4","at":"2026-11-18T05:07:00Z",` +
				`"end":"2026-11-18T05:08:00Z","seconds":60,"ended":"result","cost_usd":0,"turns":1,"failed_calls":0}`},
		{"a run that has not ended", LedgerOptions{Kind: "run", Name: "y"}, runY},
		{"each gate says whether it ran the commands at main", LedgerOptions{Kind: "gate", Name: "x"},
			`{"kind":"gate","build":"b1","name":"x","at":"2026-11-18T05:03:00Z","end":"2026-11-18T05:04:00Z",` +
				`"seconds":60,"passed":false,"landing":false,"ran_main":true}` + "\n" +
				`{"kind":"gate","build":"b1","name":"x","at":"2026-11-18T05:05:00Z","end":"2026-11-18T05:06:00Z",` +
				`"seconds":60,"passed":true,"landing":false,"ran_main":false}` + "\n" + landingX},
		{"the last gates leave out one of no build, and say null where the ledger does not say",
			LedgerOptions{Kind: "gate", Last: 2}, landingX + "\n" + fixGate},
		{"a gauge's reading of a landing", LedgerOptions{Kind: "reading"},
			`{"kind":"reading","build":"b1","name":"x","gauge":"files","at":"2026-11-18T05:10:00Z",` +
				`"landed":"abc123","value":7,"crossed":true}`},
		{"a monitor that reads a gauge", LedgerOptions{Name: "code-health"},
			`{"kind":"monitor","name":"code-health","at":"2026-11-18T05:12:00Z","main":"abc123",` +
				`"found":"healthy","build":null,"summary":"10","gauge":"health","value":10,"crossed":false}`},
		{"a monitor's runs, and the fix one started", LedgerOptions{Name: "gate-time"},
			`{"kind":"monitor","name":"gate-time","at":"2026-11-18T05:11:00Z","main":"abc123",` +
				`"found":"healthy","build":null,"summary":"within 50%","gauge":null,"value":null,"crossed":false}` +
				"\n" + `{"kind":"monitor","name":"gate-time","at":"2026-11-18T05:20:00Z","main":"abc123",` +
				`"found":"fix","build":"b3","summary":"the gate slowed 60%","gauge":null,"value":null,` +
				`"crossed":false}` + "\n" +
				`{"kind":"build","build":"b3","name":"gate-time","fix":true,"at":"2026-11-18T05:20:30Z","size":0,` +
				`"outcome":null,"seconds":60,"cost_usd":0,"turns":0,"failed_calls":0,"rounds":0,"audits":0,` +
				`"findings":0}` + "\n" + fixGate},
		{"lines come by when each started, not by when kiln wrote them", LedgerOptions{Last: 3},
			buildY + "\n" + runY + "\n" + fixGate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			if err := r.Ledger(&out, tc.opt); err != nil {
				t.Fatal(err)
			}

			if got := strings.TrimSpace(out.String()); got != tc.want {
				t.Errorf("kiln ledger printed\n%s\nwant\n%s", got, tc.want)
			}
		})
	}

	if err := r.Ledger(&strings.Builder{}, LedgerOptions{Kind: "gates"}); !errors.Is(err, ErrUsage) {
		t.Errorf("kiln ledger --kind gates answers %v, want ErrUsage", err)
	}
}

// TestLedgerShown holds that kiln tui shows a build's sums as kiln ledger prints them.
func TestLedgerShown(t *testing.T) {
	specs, err := view{ledgerRepo(t)}.Specs(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	x := specs[slices.IndexFunc(specs, func(s tui.Spec) bool { return s.Name == "x" })].Builds[0]
	if got := fmt.Sprint(*x.Took, *x.Cost, *x.Turns, *x.Failed, *x.Rounds); got != "9m0s 1 6 1 1" {
		t.Errorf("kiln tui shows the build's time, cost, turns, failed calls and rounds as %s, want 9m0s 1 6 1 1",
			got)
	}
}

// TestLedgerDoc holds the package doc's list of what kiln ledger prints to the code. Each kind of line lists each
// field, by its JSON name and type, in the order kiln prints them.
func TestLedgerDoc(t *testing.T) {
	src, err := os.ReadFile("kiln.go")
	if err != nil {
		t.Fatal(err)
	}

	_, section, _ := strings.Cut(string(src), "\n// # kiln ledger\n")
	section, _, _ = strings.Cut(section, "\n// # ")
	documented, kind := map[string][]string{}, ""
	for line := range strings.Lines(section) {
		text, inBlock := strings.CutPrefix(strings.TrimRight(line, "\n"), "//\t")
		fields := strings.Fields(text)
		switch {
		case !inBlock || len(fields) < 2:
		case strings.HasPrefix(text, "  "):
			documented[kind] = append(documented[kind], fields[0]+" "+fields[1])
		default:
			kind = fields[0]
		}
	}

	printedLines := map[string]any{"build": printedBuild{}, "run": printedRun{}, "gate": printedGate{},
		"reading": printedReading{}, "monitor": printedMonitor{}}
	if len(printedLines) != len(printedKinds) || len(documented) != len(printedKinds) {
		t.Errorf("the doc lists %d kinds of line, this test %d, and kiln ledger prints %d: %s", len(documented),
			len(printedLines), len(printedKinds), printedKinds)
	}

	for _, kind := range printedKinds {
		if want, got := jsonFields(printedLines[kind]), documented[kind]; !slices.Equal(got, want) {
			t.Errorf("the package doc lists a %s line's fields as\n%s\nkiln ledger prints\n%s", kind,
				strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
}

// jsonFields names each field of line but kind, with its type, as the package doc lists them.
func jsonFields(line any) []string {
	var fields []string
	typ := reflect.TypeOf(line)
	for f := range typ.Fields() {
		if f.Tag.Get("json") != "kind" {
			fields = append(fields, f.Tag.Get("json")+" "+jsonType(f.Type))
		}
	}

	return fields
}

// jsonType is how the package doc names the JSON type of a field of Go type typ.
func jsonType(typ reflect.Type) string {
	switch {
	case typ.Kind() == reflect.Pointer:
		return jsonType(typ.Elem()) + "|null"
	case typ == reflect.TypeFor[stamp]():
		return "time"
	case typ.Kind() == reflect.String:
		return "string"
	case typ.Kind() == reflect.Bool:
		return "bool"
	case typ.Kind() == reflect.Int, typ.Kind() == reflect.Float64:
		return "number"
	}

	return typ.String()
}
