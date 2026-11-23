package kiln

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/practices/claim"
)

// TestSecondsPrinted holds that kiln ledger prints a duration in seconds to the millisecond, as a script reads it. A
// float's sum of whole seconds and their fraction would print 3.929 as 3.9290000000000003.
func TestSecondsPrinted(t *testing.T) {
	for d, want := range map[time.Duration]string{
		3928693 * time.Microsecond: "3.929",
		2520 * time.Millisecond:    "2.52",
		313 * time.Millisecond:     "0.313",
		time.Hour:                  "3600",
	} {
		if got, err := json.Marshal(seconds(d)); err != nil || string(got) != want {
			t.Errorf("%v printed %s, %v; want %s", d, got, err, want)
		}
	}
}

// TestRecount holds what kiln fire does with each run that has a start line but no end line. kiln leaves one when it
// dies mid-run, in any session: an audit try's is never resumed. Before any build goes on, each lost run is totaled
// from the stream it wrote. It ends as the stream shows: with a result only if the stream holds one. A lost audit
// run's end line holds the tally and the findings of the reply its stream holds.
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
		`"result":"1. x.go:3  bug\\nharm  h\\n\\nread 1 directory, found 1, reported 1",` +
		`"model_usage_since_last_result":{"m":{"costUSD":0.25}}}` + "\n"
	writeStreams(t, sessions, map[string]string{buildSession: ended + lost, auditSession: audited})

	b.set(nil, entry{Kind: builtKind, Spec: "x"})
	b.set(nil, entry{Kind: startKind, Role: "build", Session: buildSession})
	turns, cost := 1, 0.5
	b.set(nil, entry{Kind: endKind, Session: buildSession, Ended: endedResult,
		Totals: &claude.Totals{Turns: &turns, CostUSD: &cost, Tools: map[string]int{"Read": 1}}})
	b.set(nil, entry{Kind: startKind, Role: "build", Session: buildSession, Streamed: int64(len(ended))})
	b.set(nil, entry{Kind: startKind, Role: "audit", Session: auditSession})

	if _, err := r.takeUp(t.Context(), io.Discard, specsLane); err != nil {
		t.Fatal(err)
	}

	if _, err := r.takeUp(t.Context(), io.Discard, specsLane); err != nil {
		t.Fatal(err)
	}

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
		if got := endOf(run); got != want {
			t.Errorf("the lost %s run: %s; want %s, with an end", run.role, got, want)
		}
	}

	if audit := runs[2]; !claim.Holds(audit.tally != nil && *audit.tally == tally{1, 1}, len(audit.findings) == 1) {
		t.Errorf("the lost audit run holds the tally %v and the findings %+v; want 1 found and reported, and 1 listed",
			audit.tally, audit.findings)
	}

	if n := len(entries); n != 7 {
		t.Errorf("the ledger holds %d lines, want 7: a second recount ends nothing", n)
	}
}

// TestRecountLane holds that a kiln recounts the lost runs of its own lane alone. A run of the other lane may be a
// live kiln's, still running. A build line that names a monitor is a fix's, in the monitors' lane.
func TestRecountLane(t *testing.T) {
	r := &Repo{store: store{dir: t.TempDir()}, claude: claude.New(claude.Config{Sessions: t.TempDir()})}
	spec := &build{r: r, rec: record{Name: "x", Build: "b1"}, out: io.Discard}
	fix := &build{r: r, rec: record{Name: "deps", Build: "b2", Monitor: true}, out: io.Discard}
	spec.set(nil, entry{Kind: builtKind, Spec: "x"})
	spec.set(nil, entry{Kind: startKind, Role: buildRole, Session: "s"})
	fix.set(nil, entry{Kind: builtKind, Spec: "deps", Monitor: "deps"})
	fix.set(nil, entry{Kind: startKind, Role: "deps", Session: "f"})
	ended := func() []string {
		entries, err := r.store.entries()
		if err != nil {
			t.Fatal(err)
		}

		var names []string
		for _, e := range entries {
			if e.Kind == endKind {
				names = append(names, e.Session)
			}
		}

		return names
	}

	if _, err := r.takeUp(t.Context(), io.Discard, specsLane); err != nil {
		t.Fatal(err)
	}

	if got := ended(); !slices.Equal(got, []string{"s"}) {
		t.Errorf("the specs' lane ended the runs of %q, want only the spec's, s", got)
	}

	if _, err := r.takeUp(t.Context(), io.Discard, monitorsLane); err != nil {
		t.Fatal(err)
	}

	if got := ended(); !slices.Equal(got, []string{"s", "f"}) {
		t.Errorf("then the monitors' lane left the runs of %q ended, want the fix's, f, too", got)
	}
}

// writeStreams writes each session's stream under sessions, as Claude Code does.
func writeStreams(t *testing.T, sessions string, streams map[string]string) {
	for session, stream := range streams {
		if err := os.MkdirAll(filepath.Join(sessions, session), 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(sessions, session, "stream.jsonl"), []byte(stream), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// endOf says how run ended, its turns and its tools, or that it has no end.
func endOf(run run) string {
	if run.end.IsZero() {
		return "no end"
	}

	turns := "<nil>"
	if run.totals.Turns != nil {
		turns = fmt.Sprint(*run.totals.Turns)
	}

	return fmt.Sprintf("%s, %s turns, %v", run.ended, turns, run.totals.Tools)
}

// TestRecountAudit holds that an audit run lost when kiln died still shows what it changed. recount names each path
// in its end line, as talk would. Then it puts the audit's worktree back at the commit the audit read.
func TestRecountAudit(t *testing.T) {
	t.Parallel()
	r, read := auditedRepo(t)
	b := &build{r: r, rec: record{Name: "x", Build: "b1"}, out: io.Discard}
	b.set(nil, entry{Kind: builtKind, Spec: "x"})
	b.set(nil, entry{Kind: startKind, Role: auditRole, Session: "a", Read: read})
	shot := filepath.Join(string(r.auditTree("x")), "shot.png")
	if err := os.WriteFile(shot, []byte("a screen"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := r.takeUp(t.Context(), io.Discard, specsLane); err != nil {
		t.Fatal(err)
	}

	entries, err := r.store.entries()
	if err != nil {
		t.Fatal(err)
	}

	if runs := collect(entries)[0].runs; fmt.Sprintf("%q", changedBy(runs)) != `[["shot.png"]]` {
		t.Errorf("the lost audit run changed %+v, want [shot.png]", runs)
	}

	if _, err := os.Stat(shot); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the audit's worktree still holds shot.png: %v", err)
	}
}

// auditedRepo is a repository with one commit, and the audit of x's worktree detached at it. It answers the commit.
func auditedRepo(t *testing.T) (*Repo, git.Rev) {
	repo := t.TempDir()
	start := exec.Command("sh", "-c", "git init --quiet -b main && git -c user.name=t -c user.email=t@t commit "+
		"--quiet --allow-empty -m first && git rev-parse HEAD")
	start.Dir = repo
	head, err := start.Output()
	if err != nil {
		t.Fatal(err)
	}

	g, err := git.New(repo, os.Environ())
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	r := &Repo{git: g, store: store{dir: dir}, trees: t.TempDir(),
		claude: claude.New(claude.Config{Sessions: filepath.Join(dir, "sessions")})}
	read := git.Rev(strings.TrimSpace(string(head)))
	if err := g.Detached(t.Context(), r.auditTree("x"), read); err != nil {
		t.Fatal(err)
	}

	return r, read
}

// changedBy names the paths each run changed.
func changedBy(runs []run) [][]string {
	changed := make([][]string, len(runs))
	for i, r := range runs {
		changed[i] = r.changed
	}

	return changed
}

// replying stands in for claude. Its run n prints a result when character n of REPLIES is r. It prints an empty
// result for e, and nothing otherwise.
const replying = `#!/bin/sh
cat >/dev/null
n=$(($(cat "$0.n" 2>/dev/null || echo 0) + 1))
echo $n >"$0.n"
case "$(echo REPLIES | cut -c$n)" in
r) echo '{"type":"result","result":"Done."}' ;;
e) echo '{"type":"result","result":""}' ;;
esac
exit 0
`

// TestVerifyReruns holds when a build in build-and-verify runs its verify session again. A run that ends with no result
// runs again while the session owes a bugs line, up to auditTries runs. A session that still owes one is asked once
// more.
func TestVerifyReruns(t *testing.T) {
	t.Parallel()
	first, owed := reason{Why: whyFirst}, reason{Why: whyBugsLine}
	for name, tc := range map[string]struct {
		asked bool
		// replies holds r for each run that ends with a result, and - for each that ends with none.
		replies string
		want    []reason
		refused bool
	}{
		"a session that owes no bugs line": {replies: "-", want: []reason{first}},
		"a session whose runs all end with no result": {asked: true, replies: "----", refused: true,
			want: []reason{first, {Why: whyNoResult, Try: 2}, {Why: whyNoResult, Try: 3}, owed}},
		"a session whose second run ends with a result": {asked: true, replies: "-rr", refused: true,
			want: []reason{first, {Why: whyNoResult, Try: 2}, owed}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := verifyRuns(t, tc.replies, tc.asked)
			if (err != nil) != tc.refused {
				t.Errorf("converse answers %v, want refused %v", err, tc.refused)
			}

			if !slices.Equal(got, tc.want) {
				t.Errorf("the session ran for %+v, want %+v", got, tc.want)
			}
		})
	}
}

// verifyRuns runs converse for a verify session with a fake claude that replies as replies says, as replying reads
// it. Asked says whether the session owes a bugs line. It answers why each run started, and what converse answered.
func verifyRuns(t *testing.T, replies string, asked bool) ([]reason, error) {
	dir := t.TempDir()
	program := filepath.Join(dir, "claude")
	if err := os.WriteFile(program, []byte(strings.ReplaceAll(replying, "REPLIES", replies)), 0o700); err != nil {
		t.Fatal(err)
	}

	sessions := filepath.Join(dir, "sessions")
	r := &Repo{store: store{dir: dir}, claude: claude.New(claude.Config{Program: program, Sessions: sessions})}
	b := &build{r: r, rec: record{Name: "x", Build: "b1", Asked: asked}, out: io.Discard}
	l := claude.Launch{Session: "5f0c1a54-2a8e-4b0e-9a65-0d1f6b8c2e11", Role: verifyRole, Prompt: "Verify x.", Dir: dir,
		Silence: time.Hour}
	conversed := b.converse(t.Context(), l, reason{Why: whyFirst})
	entries, err := r.store.entries()
	if err != nil {
		t.Fatal(err)
	}

	var started []reason
	for _, e := range entries {
		if e.Kind == startKind {
			started = append(started, e.reason)
		}
	}

	return started, conversed
}

// TestOneWrite holds that the book and the ledger change in one write. A kiln may die before it appends an update's
// lines to the ledger, or part way. The ledger still reads whole, and the next update leaves its file whole. An older
// book put back costs the ledger no line.
func TestOneWrite(t *testing.T) {
	t.Parallel()
	for name, left := range map[string]func(t *testing.T, s store, older []byte) error{
		"kiln appended the update's lines":    func(*testing.T, store, []byte) error { return nil },
		"kiln died before it appended them":   func(_ *testing.T, s store, _ []byte) error { return s.cutLedger(1 << 20) },
		"kiln died with one line half there":  func(_ *testing.T, s store, _ []byte) error { return s.cutLedger(40) },
		"kiln died before the line's newline": func(_ *testing.T, s store, _ []byte) error { return s.cutLedger(1) },
		"an older book was put back": func(_ *testing.T, s store, older []byte) error {
			return os.WriteFile(filepath.Join(s.dir, bookFile), older, 0o600)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := store{dir: t.TempDir()}
			queue := func(b *book) error {
				b.Specs = []*record{{Name: "x", State: queued}}
				b.write(entry{Kind: builtKind, Build: "b1", Spec: "x"})
				return nil
			}

			err := s.update(queue)
			older, _ := os.ReadFile(filepath.Join(s.dir, bookFile))
			err = errors.Join(err, s.write(entry{Kind: outcomeKind, Build: "b1", Outcome: outStopped}))
			if err := errors.Join(err, left(t, s, older)); err != nil {
				t.Fatal(err)
			}

			b, _ := s.read()
			read, err := s.entries()
			if got := kinds(read); !claim.Holds(err == nil, got == "build outcome", b.find("x") != nil) {
				t.Errorf("the ledger reads %q, %v, and the book holds %v; want build outcome, and x", got, err, b.Specs)
			}

			if err := s.write(entry{Kind: gateKind, Build: "b1"}); err != nil {
				t.Fatal(err)
			}

			read, _ = s.entries()
			if got := kinds(ledgerLines(t, s)); !claim.Holds(got == "build outcome gate", kinds(read) == got) {
				t.Errorf("after the next update, the file holds %q and the ledger reads %q; want build outcome gate",
					got, kinds(read))
			}
		})
	}
}

// cutLedger cuts the ledger's file as a kiln that died appending the last update's lines left it. The file ends short
// bytes before those lines' end, or where they start.
func (s store) cutLedger(short int64) error {
	b, err := s.read()
	ledger := filepath.Join(s.dir, ledgerFile)
	info, unread := os.Stat(ledger)
	if err := errors.Join(err, unread); err != nil {
		return err
	}

	return os.Truncate(ledger, max(b.Ledger, info.Size()-short))
}

// ledgerLines parses each line the ledger's file of s holds, and fails t on a line it cannot parse.
func ledgerLines(t *testing.T, s store) []entry {
	t.Helper()
	var held []entry
	raw, _ := os.ReadFile(filepath.Join(s.dir, ledgerFile))
	for line := range strings.Lines(string(raw)) {
		var e entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Errorf("the ledger's file holds a line no one can read: %q", line)
		}

		held = append(held, e)
	}

	return held
}
