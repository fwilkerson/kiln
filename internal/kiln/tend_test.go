package kiln

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/kiln/internal/gittest"
	"github.com/fwilkerson/practices/claim"
)

// standIn stands in for claude in a kiln tend. Called with -p, it is a build's session resumed headless. It keeps
// its prompt in DIR/resumed-<session>, and ends its turn. Any other call is the tend session. It keeps its first
// message in DIR/prompt, and writes DIR/note into the note's path. It waits for DIR/go, once it wrote DIR/started,
// when DIR/wait is there. It exits with DIR/exit's code, or 0.
const standIn = `#!/bin/sh
case " $* " in
*" -p "*)
	session=$(printf '%s\n' "$@" | sed -n '/^--resume$/{n;p;}')
	cat >>DIR/resumed-$session
	echo '{"type":"result","is_error":false,"result":"Done.","num_turns":1}'
	exit 0;;
esac
for a; do last=$a; done
printf '%s' "$last" >DIR/prompt
note=$(printf '%s\n' "$last" | sed -n 's/^A note for the build: //p')
[ -n "$note" ] && [ -e DIR/note ] && cp DIR/note "$note"
if [ -e DIR/wait ]; then
	touch DIR/started
	while [ ! -e DIR/go ]; do sleep 0.05; done
fi
exit $(cat DIR/exit 2>/dev/null || echo 0)
`

// tendRepo is a repository whose main holds config as kiln.json, and the build and verify agents. It holds a
// monitor m that fixes with a command, and a monitor w that watches a gauge. Its claude is standIn, whose files are
// in the directory answered.
func tendRepo(t *testing.T, config string) (r *Repo, repo, dir string) {
	t.Helper()
	r, repo = scratchRepo(t)
	writeTended(t, repo, config)
	shell(t, repo, "git add . && git commit --quiet -m start")
	return r, repo, tendClaude(t, r)
}

// tendHistory is tendRepo with tendConfig, on gittest's History h.
func tendHistory(t *testing.T, h *gittest.History) (r *Repo, dir string) {
	t.Helper()
	r, repo := historyRepo(t, h)
	writeTended(t, repo, tendConfig)
	h.CommitDir("main", repo)
	return r, tendClaude(t, r)
}

// writeTended writes into repo the files tendRepo commits, with config as kiln.json.
func writeTended(t *testing.T, repo, config string) {
	t.Helper()
	writeAll(t, repo, map[string]string{configFile: config, agentFile(buildRole): "Build it.\n",
		agentFile(verifyRole): "Test it.\n"})
	writeMonitor(t, repo, "m", map[string]string{"command": "true"})
	writeMonitor(t, repo, "w", map[string]string{"gauge": "echo 70", "monitor.json": `{"atLeast": 80}`})
}

// tendClaude makes r's claude standIn, and answers the directory of standIn's files.
func tendClaude(t *testing.T, r *Repo) (dir string) {
	t.Helper()
	dir = t.TempDir()
	program := filepath.Join(dir, "claude")
	if err := os.WriteFile(program, []byte(strings.ReplaceAll(standIn, "DIR", dir)), 0o700); err != nil {
		t.Fatal(err)
	}

	r.claude = claude.New(claude.Config{Program: program, Env: os.Environ(), Sessions: filepath.Join(dir, "sessions")})
	return dir
}

const tendConfig = `{"gate": ["true"], "rounds": 1, "acceptanceTests": "acceptance/{spec}/"}`

// given puts recs in r's book, and writes entries to its ledger. Each session a record names has begun: its stream
// holds a line, as a session that ran prints one.
func given(t *testing.T, r *Repo, recs []*record, entries []entry) {
	t.Helper()
	if err := r.store.update(func(b *book) error { b.Specs = recs; return nil }); err != nil {
		t.Fatal(err)
	}

	for _, rec := range recs {
		for _, session := range []string{rec.Session, rec.Verify} {
			if session == "" {
				continue
			}

			writeAll(t, r.claude.Dir(session), map[string]string{"stream.jsonl": `{"type":"system"}` + "\n"})
		}
	}

	for _, e := range entries {
		if err := r.store.write(e); err != nil {
			t.Fatal(err)
		}
	}
}

// TestTendMessage holds the tend session's first message for the states the acceptance tests leave out. A session
// that exits non-zero fails kiln tend, and nothing goes on.
func TestTendMessage(t *testing.T) {
	t.Parallel()
	fixLine := func(build string) entry { return entry{Kind: monitorKind, Monitor: "m", Found: foundFix, Build: build} }
	for name, tc := range map[string]struct {
		name    Spec
		recs    []*record
		entries []entry
		// tree makes the worktree of name's work. config, if set, is a kiln.json main gains after the start.
		tree       bool
		config     string
		want, lack []string
	}{
		"a monitor whose session failed": {name: "m",
			entries: []entry{fixLine("b1"), {Kind: outcomeKind, Build: "b1", Outcome: outFailed, Summary: "Not logged in"}},
			want:    []string{"The monitor m: session failed\nNot logged in\nIts directory: .kiln/monitors/m/\n"},
			lack:    []string{"A note for the build", "Its sessions"}},
		"a monitor whose fix stopped": {name: "m", tree: true,
			recs: []*record{{Name: "m", State: stopped, Monitor: true, Build: "b2", Session: "s2"}},
			entries: []entry{fixLine("b2"), {Kind: outcomeKind, Build: "b2", Outcome: outStopped, Summary: "3 updates",
				Report: "the gate failed"}},
			want: []string{"The monitor m: stopped\n3 updates\nthe gate failed\nIts work: kiln/m, in ",
				"Its sessions: fix s2, in "}},
		"a monitor that couldn't run, and is held": {name: "m",
			entries: []entry{{Kind: monitorKind, Monitor: "m", Found: foundCouldNot, Summary: "check: exit 2"},
				{Kind: monitorKind, Monitor: "m", Found: foundHeld, Past: true, Summary: "past the timeout"}},
			want: []string{"The monitor m: held\npast the timeout\n"}},
		"a monitor whose fix needs you, held since": {name: "m",
			entries: []entry{{Kind: monitorKind, Monitor: "m", Found: foundFix, Build: "b3", Summary: "it logged"}, {Kind: outcomeKind, Build: "b3", Outcome: outNeedsYou, Summary: "look",
				Report: "the report"}, {Kind: monitorKind, Monitor: "m", Found: foundHeld}},
			want: []string{"The monitor m: needs you\nit logged\nlook\nthe report\n"}},
		"a monitor that needs you, under a kiln.json kiln refuses": {name: "m", config: `{"gate": [`,
			entries: []entry{{Kind: monitorKind, Monitor: "m", Found: outNeedsYou, Summary: "m: 70"}},
			want: []string{"The monitor m: refused\nkiln could not read the monitors at main's tip: ",
				"kiln.json"}},
		"a watching monitor that needs you": {name: "w",
			entries: []entry{{Kind: monitorKind, Monitor: "w", Found: outNeedsYou, Summary: "w: 70, line ≥ 80, crossed"}},
			want:    []string{"The monitor w: needs you\nw: 70, line ≥ 80, crossed\nIts directory: .kiln/monitors/w/"}},
		"a stopped landing of accepted amendments": {name: "x",
			recs: []*record{{Name: "x", State: stopped, Accepted: true, Session: "s4",
				Why: "the gate still failed after its last round:\nFAIL", Failing: []string{"TestAT1"}}},
			want: []string{"The spec x: stopped, landing accepted amendments\nthe gate still failed after its last " +
				"round:\nFAIL\nfailing: TestAT1\nIts sessions: build s4, in "},
			lack: []string{"A note for the build", "Its directory", "Its work"}},
		"a build in build-and-verify that stopped": {name: "x",
			recs: []*record{{Name: "x", State: stopped, Session: "s5", Verify: "v5", Why: couldNotGoOn + "no disk",
				Kind: stopKiln}},
			want: []string{"The spec x: stopped\n" + couldNotGoOn + "no disk\nIts sessions: build s5, in ",
				"/.build-x; verify v5, in ", "/.verify-x\nA note for the build: "}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, repo, dir := tendRepo(t, tendConfig)
			given(t, r, tc.recs, tc.entries)
			if tc.config != "" {
				writeAll(t, repo, map[string]string{configFile: tc.config})
				shell(t, repo, "git commit --quiet -am config")
			}

			if tc.tree {
				if err := os.MkdirAll(string(r.tree(tc.name)), 0o700); err != nil {
					t.Fatal(err)
				}
			}

			writeAll(t, dir, map[string]string{"exit": "1"})
			err := r.Tend(t.Context(), tc.name, Terminal{Out: io.Discard, Err: io.Discard})
			prompt, _ := os.ReadFile(filepath.Join(dir, "prompt"))
			if err == nil {
				t.Errorf("kiln tend of a session that exited 1 answered no error")
			}

			wantHolds(t, string(prompt), tc.want, tc.lack)
		})
	}
}

// TestTendRefuses holds what kiln tend refuses, with no session. It refuses a name that waits on no repair, and one
// another kiln tend is at work on. Each refusal starts with the name, and says where it stands.
func TestTendRefuses(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		name    Spec
		recs    []*record
		entries []entry
		// tending holds the name's lock of kiln tend.
		tending bool
		says    string
	}{
		"a healthy monitor": {name: "m", entries: []entry{{Kind: monitorKind, Monitor: "m", Found: foundHealthy}},
			says: "m: it is healthy, and waits on no repair"},
		"a monitor that never ran": {name: "m", says: "m: it is never run"},
		"no spec or monitor":       {name: "y", says: "y: kiln holds no record of it"},
		"a spec waiting for UAT": {name: "x", recs: []*record{{Name: "x", State: landed, LandedAt: "1a2b3c4d5e6f7"}},
			says: "x: it is waiting for UAT; next: read it with git show 1a2b3c4d5e6f, then run kiln accept x"},
		"a stopped spec another kiln tend is at work on": {name: "x", recs: []*record{{Name: "x", State: stopped}},
			tending: true, says: "x: a kiln tend is at work on it"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, dir := tendHistory(t, gittest.New(t))
			given(t, r, tc.recs, tc.entries)
			if tc.tending {
				unlock, err := r.lockTend(tc.name)
				if err != nil {
					t.Fatal(err)
				}

				defer unlock()
			}

			err := r.Tend(t.Context(), tc.name, Terminal{Out: io.Discard, Err: io.Discard})
			_, started := os.Stat(filepath.Join(dir, "prompt"))
			if !claim.Holds(errors.Is(err, errRefused), strings.Contains(fmt.Sprint(err), tc.says), started != nil) {
				t.Errorf("kiln tend answered %v, and started a session: %v; want a refusal that says %q, and none",
					err, started == nil, tc.says)
			}
		})
	}
}

// TestTendLanded holds kiln tend --landed: it takes a spec's stopped build alone. It takes it once main shows its
// work under the spec's acceptance paths. Then the spec waits for UAT, at main's tip, and its branch is gone.
func TestTendLanded(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		rec  record
		work string
		says string
	}{
		"a spec waiting for UAT": {rec: record{State: landed},
			says: "it is waiting for UAT, and kiln tend --landed takes a spec's stopped build"},
		"a stopped landing": {rec: record{State: stopped, Accepted: true}, says: "which kiln accept x lands again"},
		"work main does not show": {rec: record{State: stopped}, work: "mkdir src && echo 42 >src/x.txt",
			says: "main changed nothing under acceptance/x/ since"},
		"work main shows": {rec: record{State: stopped},
			work: "mkdir -p src acceptance/x && echo 42 >src/x.txt && echo t >acceptance/x/t.txt"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, repo, _ := tendRepo(t, tendConfig)
			approved := git.Rev(shell(t, repo, "git rev-parse main"))
			tc.rec.Name, tc.rec.Approved, tc.rec.Build = "x", approved, "b1"
			given(t, r, []*record{&tc.rec}, nil)
			if err := r.git.Checkout(t.Context(), r.tree("x"), Spec("x").branch(), approved); err != nil {
				t.Fatal(err)
			}

			if tc.work != "" {
				shell(t, repo, tc.work+" && git add . && git commit --quiet -m work")
			}

			err := r.TendLanded(t.Context(), "x", io.Discard)
			k, _ := r.store.read()
			if tc.says != "" {
				wantRefusal(t, err, tc.says)
				return
			}

			main := git.Rev(shell(t, repo, "git rev-parse main"))
			_, treeErr := os.Stat(string(r.tree("x")))
			rec := k.find("x")
			if !claim.Holds(err == nil, rec.State == landed, rec.LandedAt == main, os.IsNotExist(treeErr)) {
				t.Errorf("kiln tend --landed answered %v, and left %+v, its worktree there: %v; want it landed at %s",
					err, rec, treeErr == nil, main)
			}
		})
	}
}

// TestTendGoesOn holds that an interrupt while the tend session runs is the developer's, and leaves the build to go
// on. kiln resumes the build's own session, with the note, and its work lands.
func TestTendGoesOn(t *testing.T) {
	t.Parallel()
	r, repo, dir := tendRepo(t, tendConfig)
	approved := git.Rev(shell(t, repo, "git rev-parse main"))
	if err := r.git.Checkout(t.Context(), r.tree("x"), Spec("x").branch(), approved); err != nil {
		t.Fatal(err)
	}

	shell(t, string(r.tree("x")), "echo 42 >x.txt && git add x.txt && git commit --quiet -m work")
	head := git.Rev(shell(t, string(r.tree("x")), "git rev-parse HEAD"))
	session := claude.NewID()
	given(t, r, []*record{{Name: "x", State: stopped, Approved: approved, Build: "b1", Session: session,
		Audited: head, Limit: new(1), Rounds: 1, Why: "the gate still failed after its last round:\nFAIL"}}, nil)
	writeAll(t, dir, map[string]string{"wait": "", "note": "use 42"})
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		for !exists(filepath.Join(dir, "started")) {
			time.Sleep(10 * time.Millisecond)
		}

		cancel()
		writeAll(t, dir, map[string]string{"go": ""})
	}()

	err := r.Tend(ctx, "x", Terminal{Out: io.Discard, Err: io.Discard})
	resumed, _ := os.ReadFile(filepath.Join(dir, "resumed-"+session))
	k, _ := r.store.read()
	main := git.Rev(shell(t, repo, "git rev-parse main"))
	rec := k.find("x")
	if !claim.Holds(err == nil, rec != nil && rec.State == landed, rec != nil && rec.LandedAt == main,
		strings.Contains(string(resumed), "use 42"), strings.Contains(string(resumed), "the gate still failed")) {
		t.Errorf("kiln tend answered %v, and left %+v; the build session was told %q; want it landed at %s, told "+
			"why it stopped and the note", err, rec, resumed, main)
	}
}

// TestTendGoesOnSplit holds kiln tend of a stopped build in build-and-verify. kiln resumes both of its sessions, and
// starts none anew. Each is told why the build stopped, and the note. The work of both lands, with what main gained.
func TestTendGoesOnSplit(t *testing.T) {
	t.Parallel()
	// The gate passes only on work that holds main's fix, which main gains once the build stopped.
	r, repo, dir := tendRepo(t, `{"gate": ["test -e fix.txt"], "rounds": 1, "workflow": "build-and-verify", `+
		`"acceptanceTests": "acceptance/{spec}/"}`)
	approved := git.Rev(shell(t, repo, "git rev-parse main"))
	builder, verifier := claude.NewID(), claude.NewID()
	rec := record{Name: "x", State: stopped, Approved: approved, Build: "b1", Session: builder, Verify: verifier,
		Audited: approved, Limit: new(1), Rounds: 1, Why: "the gate still failed after its last round:\nFAIL"}
	given(t, r, []*record{&rec}, nil)
	b := &build{r: r, rec: rec, tree: r.tree("x"), out: io.Discard}
	if err := b.cloneSides(t.Context()); err != nil {
		t.Fatal(err)
	}

	shell(t, string(r.clone("x", buildRole)), "echo 42 >x.txt && git add . && git commit --quiet -m code")
	shell(t, string(r.clone("x", verifyRole)),
		"mkdir -p acceptance/x && echo t >acceptance/x/t.txt && git add . && git commit --quiet -m tests")
	shell(t, repo, "echo fixed >fix.txt && git add fix.txt && git commit --quiet -m fix")
	writeAll(t, dir, map[string]string{"note": "use 42"})
	err := r.Tend(t.Context(), "x", Terminal{Out: io.Discard, Err: io.Discard})
	k, _ := r.store.read()
	main := git.Rev(shell(t, repo, "git rev-parse main"))
	landedBoth := shell(t, repo, "git ls-tree -r --name-only main -- x.txt acceptance/x fix.txt")
	now := k.find("x")
	if !claim.Holds(err == nil, now != nil && now.State == landed, now != nil && now.LandedAt == main,
		landedBoth == "acceptance/x/t.txt\nfix.txt\nx.txt") {
		t.Errorf("kiln tend answered %v, and left %+v, main holding %q; want both sides landed at %s", err, now,
			landedBoth, main)
	}

	for _, session := range []string{builder, verifier} {
		told, _ := os.ReadFile(filepath.Join(dir, "resumed-"+session))
		if !claim.Holds(strings.Contains(string(told), "use 42"),
			strings.Contains(string(told), "the gate still failed")) {
			t.Errorf("session %s was told %q; want why the build stopped, and the note", session, told)
		}
	}

	// standIn keeps a headless call with no --resume in resumed-, a session started anew.
	if resumed, _ := filepath.Glob(filepath.Join(dir, "resumed-*")); len(resumed) != 2 {
		t.Errorf("kiln ran the headless sessions %q; want the build's own 2", resumed)
	}
}

// wantRefusal holds that err is a refusal that says says.
func wantRefusal(t *testing.T, err error, says string) {
	t.Helper()
	if !claim.Holds(errors.Is(err, errRefused), strings.Contains(fmt.Sprint(err), says)) {
		t.Errorf("answered %v; want a refusal that says %q", err, says)
	}
}

// exists is whether a file is at path.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// TestSplitOwed holds that a build in build-and-verify kiln tend queued again sends the owed message to both its
// sessions. Both turns had ended. passOn sends none of it, since it is no bug and no test that can't hold.
func TestSplitOwed(t *testing.T) {
	t.Parallel()
	r, repo, dir := tendRepo(t, `{"gate": ["true"], "rounds": 1, "workflow": "build-and-verify", `+
		`"acceptanceTests": "acceptance/{spec}/"}`)
	approved := git.Rev(shell(t, repo, "git rev-parse main"))
	builder, verifier := claude.NewID(), claude.NewID()
	rec := record{Name: "x", State: building, Approved: approved, Build: "b1", Session: builder, Verify: verifier,
		Owed: "OWED"}
	given(t, r, []*record{&rec}, nil)
	b := &build{r: r, rec: rec, tree: r.tree("x"), out: io.Discard}
	if err := b.cloneSides(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := b.splitGoOn(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := b.passOn(t.Context()); err != nil {
		t.Fatal(err)
	}

	for _, session := range []string{builder, verifier} {
		told, _ := os.ReadFile(filepath.Join(dir, "resumed-"+session))
		if strings.Count(string(told), "OWED") != 1 {
			t.Errorf("session %s was told %q; want the owed message once", session, told)
		}
	}

	if b.now().Owed != "" {
		t.Errorf("the record still owes %q", b.now().Owed)
	}
}

// TestMonitorRunBesideTend holds kiln monitor run's two rules for kiln tend. It skips a monitor a kiln tend is at
// work on. A run that finds nothing to fix drops the monitor's stopped fix, with its worktree.
func TestMonitorRunBesideTend(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		tending bool
		says    string
		kept    bool
	}{
		"a monitor a kiln tend is at work on is skipped": {tending: true, kept: true,
			says: "c: skipped, since a kiln tend is at work on it"},
		"a healthy run drops a stopped fix": {says: "c: dropped its stopped fix"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, repo, _ := tendRepo(t, tendConfig)
			writeMonitor(t, repo, "c", map[string]string{"command": "true",
				"check": `echo '{"fix": false, "summary": "fine"}'`})
			shell(t, repo, "git add . && git commit --quiet -m c")
			main := git.Rev(shell(t, repo, "git rev-parse main"))
			given(t, r, []*record{{Name: "c", State: stopped, Monitor: true, Build: "b1", Approved: main}}, nil)
			if err := r.git.Checkout(t.Context(), r.tree("c"), Spec("c").branch(), main); err != nil {
				t.Fatal(err)
			}

			if tc.tending {
				unlock, err := r.lockTend("c")
				if err != nil {
					t.Fatal(err)
				}

				defer unlock()
			}

			var out strings.Builder
			err := r.MonitorRun(t.Context(), "c", &out)
			k, _ := r.store.read()
			kept := k.find("c") != nil && exists(string(r.tree("c")))
			if !claim.Holds(err == nil, strings.Contains(out.String(), tc.says), kept == tc.kept) {
				t.Errorf("kiln monitor run answered %v, and said\n%s\nkept the fix %v; want it to say %q, and keep "+
					"it %v", err, &out, kept, tc.says, tc.kept)
			}
		})
	}
}
