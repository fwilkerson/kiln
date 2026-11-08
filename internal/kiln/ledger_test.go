package kiln

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/fwilkerson/kiln/internal/claude"
)

// TestRecount holds what kiln run does with each run that has a start line but no end line. kiln leaves one when it
// dies mid-run, in any session: an audit try's is never resumed. Before any build goes on, each lost run is totaled
// from the stream it wrote. It ends as the stream shows: with a result only if the stream holds one.
func TestRecount(t *testing.T) {
	dir := t.TempDir()
	const buildSession, auditSession = "5f0c1a54-2a8e-4b0e-9a65-0d1f6b8c2e11", "8a1d2b3c-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
	sessions := filepath.Join(dir, "sessions")
	r := &Repo{store: store{dir: dir}, claude: claude.New(claude.Config{Sessions: sessions})}
	b := &build{r: r, rec: record{Name: "x", Build: "b1"}, out: io.Discard}

	ended := `{"type":"assistant","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Read",` +
		`"input":{}}],"usage":{}}}` + "\n" + `{"type":"result","num_turns":1,` +
		`"model_usage_since_last_result":{"m":{"costUSD":0.5}}}` + "\n"
	lost := `{"type":"assistant","message":{"id":"m2","content":[{"type":"tool_use","id":"t2","name":"Bash",` +
		`"input":{"command":"go test ./..."}}],"usage":{}}}` + "\n"
	audited := `{"type":"assistant","message":{"id":"m3","content":[{"type":"tool_use","id":"t3","name":"Read",` +
		`"input":{"file_path":"x.go"}}],"usage":{}}}` + "\n" + `{"type":"result","num_turns":1,` +
		`"model_usage_since_last_result":{"m":{"costUSD":0.25}}}` + "\n"
	for session, stream := range map[string]string{buildSession: ended + lost, auditSession: audited} {
		if err := os.MkdirAll(filepath.Join(sessions, session), 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(sessions, session, "stream.jsonl"), []byte(stream), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	b.record(entry{Kind: builtKind, Spec: "x"})
	b.record(entry{Kind: startKind, Role: "build", Session: buildSession})
	turns, cost := 1, 0.5
	b.record(entry{Kind: endKind, Session: buildSession, Ended: endedResult,
		Totals: &claude.Totals{Turns: &turns, CostUSD: &cost, Tools: map[string]int{"Read": 1}}})
	b.record(entry{Kind: startKind, Role: "build", Session: buildSession, Streamed: int64(len(ended))})
	b.record(entry{Kind: startKind, Role: "audit", Session: auditSession})

	r.recount(io.Discard)
	r.recount(io.Discard)
	entries, err := r.store.entries()
	if err != nil {
		t.Fatal(err)
	}

	runs := collect(entries)[0].runs
	if len(runs) != 3 {
		t.Fatalf("%d runs, want 3: %+v", len(runs), runs)
	}

	for i, want := range map[int]string{
		1: "no result, <nil> turns, map[Bash:1]",
		2: "result, 1 turns, map[Read:1]",
	} {
		run := runs[i]
		turns := "<nil>"
		if run.totals.Turns != nil {
			turns = fmt.Sprint(*run.totals.Turns)
		}

		got := fmt.Sprintf("%s, %s turns, %v", run.ended, turns, run.totals.Tools)
		if got != want || run.end.IsZero() {
			t.Errorf("the lost %s run: %s, ended at %v; want %s, with an end", run.role, got, run.end, want)
		}
	}

	if n := len(entries); n != 7 {
		t.Errorf("the ledger holds %d lines, want 7: a second recount ends nothing", n)
	}
}
