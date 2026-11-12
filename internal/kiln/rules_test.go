package kiln

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/fwilkerson/kiln/internal/git"
)

func TestGuard(t *testing.T) {
	const place = "/Users/dev/Library/Caches/kiln/repo/x"
	for name, tc := range map[string]struct {
		tool, cwd, file string
		refused         bool
	}{
		"a build writes its code":                    {tool: "Write", file: place + "/x.go"},
		"a build writes its spec's amendments":       {tool: "Write", file: place + "/.kiln/specs/x.amend.md"},
		"a build may not edit its spec":              {tool: "Edit", file: place + "/.kiln/specs/x.md", refused: true},
		"nor another spec's amendments":              {tool: "Write", file: place + "/.kiln/specs/y.amend.md", refused: true},
		"nor anything deeper under specs":            {tool: "Write", file: place + "/.kiln/specs/x/x.amend.md", refused: true},
		"nor an agent's file":                        {tool: "Edit", file: place + "/.kiln/agents/audit.md", refused: true},
		"nor the gate's commands":                    {tool: "Write", file: place + "/.kiln/kiln.json", refused: true},
		"nor a spec spelled in other letters":        {tool: "Edit", file: place + "/.KILN/Specs/x.md", refused: true},
		"nor a spec by a path relative to its cwd":   {tool: "Edit", cwd: place + "/.kiln/specs", file: "x.md", refused: true},
		"nor a path that climbs out of its worktree": {tool: "Write", file: place + "/../y/x.go", refused: true},
		"nor the developer's Claude Code settings":   {tool: "Write", file: place + "/.claude/settings.json", refused: true},
		"nor a git directory":                        {tool: "Write", file: place + "/sub/.git/config", refused: true},
		"nor a notebook under .kiln":                 {tool: "NotebookEdit", file: place + "/.kiln/n.ipynb", refused: true},
		"a write that names no file is refused":      {tool: "Write", refused: true},
		"a path through /private is the same path":   {tool: "Write", file: "/private/tmp/w/.kiln/specs/x.md", refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			input := map[string]string{"file_path": tc.file}
			if tc.tool == "NotebookEdit" {
				input = map[string]string{"notebook_path": tc.file}
			}

			call := map[string]any{"tool_name": tc.tool, "cwd": tc.cwd, "tool_input": input}

			in, _ := json.Marshal(call)
			var out bytes.Buffer
			worktree := git.Worktree(place)
			if strings.HasPrefix(tc.file, "/private") {
				worktree = "/tmp/w"
			}

			if err := Hook(bytes.NewReader(in), &out, "x", worktree); err != nil {
				t.Fatal(err)
			}

			if refused := strings.Contains(out.String(), `"permissionDecision":"deny"`); refused != tc.refused {
				t.Errorf("refused %v, want %v: %s", refused, tc.refused, out.String())
			}
		})
	}
}

func TestGate(t *testing.T) {
	const failing = "--- FAIL: TestFlaky (0.00s)\n    flaky_test.go:5: no\nFAIL\nFAIL\texample.com/s\t0.1s\n"
	const other = "--- FAIL: TestNew (0.00s)\nFAIL\nFAIL\texample.com/s\t0.1s\n"
	const broken = "# example.com/s\n./s.go:3:1: syntax error\nFAIL\texample.com/s [build failed]\n"
	const hung = "panic: test timed out after 10m0s\nFAIL\texample.com/t\t600.0s\n"
	for name, tc := range map[string]struct {
		tip, main []check
		refused   map[string]string
		passed    bool
		atMain    []string
		failing   []string
		hinted    bool
		// mentions is what the report must hold.
		mentions string
	}{
		"every command exits 0": {
			tip: []check{{Command: "go test ./...", Exit: 0}}, passed: true,
		},
		"a command fails": {
			tip:     []check{{Command: "go vet ./...", Exit: 1, Output: "vet: x"}},
			main:    []check{{Command: "go vet ./...", Exit: 0}},
			failing: []string{"`go vet ./...`"},
		},
		"a test fails that passes at main": {
			tip:     []check{{Command: "go test ./...", Exit: 1, Output: other}},
			main:    []check{{Command: "go test ./...", Exit: 0}},
			failing: []string{"TestNew (example.com/s)"},
		},
		"a test fails that fails at main too": {
			tip:    []check{{Command: "go test ./...", Exit: 1, Output: failing}},
			main:   []check{{Command: "go test ./...", Exit: 1, Output: failing}},
			passed: true, atMain: []string{"TestFlaky (example.com/s)"},
		},
		"a test fails at main, and another fails only at the tip": {
			tip:     []check{{Command: "go test ./...", Exit: 1, Output: failing + other}},
			main:    []check{{Command: "go test ./...", Exit: 1, Output: failing}},
			atMain:  []string{"TestFlaky (example.com/s)"},
			failing: []string{"TestNew (example.com/s)"},
		},
		"a test that fails at main is named though the tip passes": {
			tip:    []check{{Command: "go test ./...", Exit: 0}},
			main:   []check{{Command: "go test ./...", Exit: 1, Output: failing}},
			passed: true, atMain: []string{"TestFlaky (example.com/s)"},
		},
		"a build that fails at main too still fails": {
			tip:     []check{{Command: "go test ./...", Exit: 1, Output: broken}},
			main:    []check{{Command: "go test ./...", Exit: 1, Output: broken}},
			failing: []string{"`go test ./...`"},
		},
		"a package that hangs, while a test fails at main too": {
			tip:     []check{{Command: "go test ./...", Exit: 1, Output: failing + hung}},
			main:    []check{{Command: "go test ./...", Exit: 1, Output: failing}},
			atMain:  []string{"TestFlaky (example.com/s)"},
			failing: []string{"`go test ./...`"},
		},
		"a test that fails at main under another command": {
			tip:     []check{{Command: "go test -race ./...", Exit: 1, Output: failing}},
			main:    []check{{Command: "go test ./...", Exit: 1, Output: failing}},
			atMain:  []string{"TestFlaky (example.com/s)"},
			failing: []string{"TestFlaky (example.com/s)"},
		},
		"a command that names no test fails at main too": {
			tip:     []check{{Command: "./check", Exit: 1, Output: "no"}},
			main:    []check{{Command: "./check", Exit: 1, Output: "no"}},
			failing: []string{"`./check`"}, hinted: true,
		},
		"a test fails at main too, but a test main ran did not run": {
			tip:     []check{{Command: "pytest", Exit: 1, Failed: []string{"t"}, Ran: []string{"t"}}},
			main:    []check{{Command: "pytest", Exit: 1, Failed: []string{"t"}, Ran: []string{"t", "u"}}},
			atMain:  []string{"t"},
			failing: []string{"`pytest`"}, mentions: "not here: u.",
		},
		"a guarded path changed": {
			tip:     []check{{Command: "go test ./...", Exit: 0}},
			refused: map[string]string{".kiln/specs/x.md": ".kiln/specs/x.md is under .kiln/"},
			failing: []string{".kiln/specs/x.md"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			readOutputs(tc.tip, tc.main)
			v := judge(tc.tip, tc.main, tc.refused)
			got := judgment{
				v.passed, fmt.Sprintf("%q", v.atMain), fmt.Sprintf("%q", v.failing), v.report != "",
				strings.Contains(v.report, "KILN_JUNIT"), strings.Contains(v.report, tc.mentions),
			}
			want := judgment{
				tc.passed, fmt.Sprintf("%q", tc.atMain), fmt.Sprintf("%q", tc.failing), !tc.passed, tc.hinted, true,
			}
			if got != want {
				t.Errorf("got %+v, report %q; want %+v", got, v.report, want)
			}
		})
	}
}

// judgment is what TestGate compares of a verdict. reported is whether it has a report. hinted is whether the report
// points at KILN_JUNIT. mentioned is whether it holds what the row says it mentions.
type judgment struct {
	passed                      bool
	atMain, failing             string
	reported, hinted, mentioned bool
}

// readOutputs reads each check's output for the Go tests that failed, as runChecks does. It reads only go test's
// output, and keeps what a row says its JUnit XML named.
func readOutputs(checks ...[]check) {
	for _, cs := range checks {
		for i := range cs {
			failed, broken := failedTests(cs[i].Output)
			cs[i].Failed, cs[i].Broken = append(cs[i].Failed, failed...), cs[i].Broken || broken
		}
	}
}

func TestSpecStates(t *testing.T) {
	for name, tc := range specStates {
		t.Run(name, func(t *testing.T) {
			b := givenBook(tc.given)
			got, r := tc.apply(t, &b)
			if want := tc.want(); got != want {
				t.Errorf("got %+v; want %+v", got, want)
			}

			if want, ok := tc.carries(); ok && carriedOf(r) != want {
				t.Errorf("the record carries %+v of the build before, want %+v", carriedOf(r), want)
			}
		})
	}
}

// specState is a row of TestSpecStates. given is a spec's state, with " landing" for one landing accepted amendments,
// or "" for no record. then is "" for a refusal, and "no record" once the record is gone. drop is a branch to drop,
// and dropped a build ended unended.
type specState struct {
	given, command, then string
	changed, drop        bool
	dropped              bool
}

const none = "no record"

var specStates = map[string]specState{
	"queue a spec with no record":              {given: "", command: "queue", then: "queued"},
	"queue a queued spec again":                {given: "queued", command: "queue", then: "queued", dropped: true},
	"queue a building spec":                    {given: "building", command: "queue"},
	"queue a stopped spec":                     {given: "stopped", command: "queue", then: "queued", drop: true},
	"queue a spec waiting for UAT, unchanged":  {given: "waiting for UAT", command: "queue"},
	"queue a spec waiting for UAT, changed":    {given: "waiting for UAT", command: "queue", changed: true, then: "queued", drop: true},
	"queue a spec with amendments":             {given: "waiting on its branch with amendments", command: "queue"},
	"queue a queued landing":                   {given: "queued landing", command: "queue"},
	"queue a building landing":                 {given: "building landing", command: "queue"},
	"queue a stopped landing":                  {given: "stopped landing", command: "queue"},
	"accept a spec with no record":             {given: "", command: "accept"},
	"accept a queued spec":                     {given: "queued", command: "accept"},
	"accept a building spec":                   {given: "building", command: "accept"},
	"accept a stopped spec":                    {given: "stopped", command: "accept"},
	"accept a spec waiting for UAT":            {given: "waiting for UAT", command: "accept", then: "waiting for UAT"},
	"accept a spec with amendments":            {given: "waiting on its branch with amendments", command: "accept", then: "waiting on its branch with amendments"},
	"accept a queued landing":                  {given: "queued landing", command: "accept"},
	"accept a building landing":                {given: "building landing", command: "accept"},
	"accept a stopped landing lands it afresh": {given: "stopped landing", command: "accept", then: "queued landing"},
	"reject a spec with no record":             {given: "", command: "reject"},
	"reject a queued spec":                     {given: "queued", command: "reject"},
	"reject a building spec":                   {given: "building", command: "reject"},
	"reject a stopped spec":                    {given: "stopped", command: "reject"},
	"reject a spec waiting for UAT":            {given: "waiting for UAT", command: "reject"},
	"reject a spec with amendments":            {given: "waiting on its branch with amendments", command: "reject", then: none},
	"reject a queued landing":                  {given: "queued landing", command: "reject"},
	"reject a building landing":                {given: "building landing", command: "reject"},
	"reject a stopped landing":                 {given: "stopped landing", command: "reject", then: none},
}

// givenBook is a book that holds the spec x in the state given words, as a row's given does.
func givenBook(given string) book {
	if given == "" {
		return book{}
	}

	given, landing := strings.CutSuffix(given, " landing")
	return book{Specs: []*record{{Name: "x", State: state(given), Accepted: landing, Approved: "old", Session: "s",
		Build: "b", Rounds: 2, Audited: true, Why: "w", FailingAtMain: []string{"TestF"}}}}
}

// commanded is what a command left of the spec x. It holds x's state, as a row's then words it, its error and
// its drops.
type commanded struct {
	then, err     string
	drop, dropped bool
}

// apply runs the row's command on x in b. It answers what the command left, and x's record after it.
func (tc specState) apply(t *testing.T, b *book) (commanded, *record) {
	changed := func(since git.Rev) (bool, error) {
		if since != "old" {
			t.Errorf("changed since %q, want since the queue", since)
		}

		return tc.changed, nil
	}

	var got commanded
	var dropped string
	var err error
	switch tc.command {
	case "queue":
		got.drop, dropped, err = b.queue("x", "new", changed)
	case "accept":
		_, err = b.accept("x")
	case "reject":
		_, err = b.reject("x")
	}

	r := b.find("x")
	got.then, got.err, got.dropped = thenOf(r), errKind(err), dropped == "b"
	return got, r
}

// want is what the row's command should leave. A refusal leaves the spec as it was.
func (tc specState) want() commanded {
	if tc.then == "" {
		return commanded{then: cmp.Or(tc.given, none), err: "refused"}
	}

	return commanded{then: tc.then, drop: tc.drop, dropped: tc.dropped}
}

// carries is what x's record should carry of the build before, if the row checks it. A new queue carries nothing.
// A landing accepted again keeps its build, session and audit.
func (tc specState) carries() (carried, bool) {
	switch {
	case tc.then == "":
		return carried{}, false
	case tc.command == "queue":
		return carried{approved: "new"}, true
	case tc.then == "queued landing":
		return carried{approved: "old", session: "s", oldBuild: true, audited: true}, true
	}

	return carried{}, false
}

// carried is what a record carries of the build before, as givenBook made it.
type carried struct {
	approved                   git.Rev
	session, why               string
	oldBuild, noBuild, audited bool
	rounds                     int
	failingAtMain              bool
}

func carriedOf(r *record) carried {
	return carried{approved: r.Approved, session: r.Session, why: r.Why, oldBuild: r.Build == "b",
		noBuild: r.Build == "", audited: r.Audited, rounds: r.Rounds, failingAtMain: r.FailingAtMain != nil}
}

// thenOf words the state of r as a row's then does.
func thenOf(r *record) string {
	if r == nil {
		return none
	}

	if r.Accepted {
		return string(r.State) + " landing"
	}

	return string(r.State)
}

// errKind is "" for no error, "refused" for a refusal, and the error's text for any other.
func errKind(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errRefused):
		return "refused"
	}

	return err.Error()
}

func TestFoldAmendments(t *testing.T) {
	const spec = "# S\n\n### 3. T\n\n- **Given**: a.\n- **When**: b.\n- **Then**: c.\n"
	const section = "## Acceptance test 3: T\n\n### Old\n\n- **Then**: c.\n\n### New\n\n- **Then**: d.\n\n" +
		"### Reason\n\nc can't hold.\n"
	for name, tc := range map[string]struct {
		spec, amendments, want string
	}{
		"the new wording replaces the old": {
			spec: spec, amendments: "# Amendments\n\n" + section,
			want: "# S\n\n### 3. T\n\n- **Given**: a.\n- **When**: b.\n- **Then**: d.\n",
		},
		"an amendment with no reason is refused": {
			spec: spec, amendments: strings.Split(section, "### Reason")[0],
		},
		"old wording the spec does not hold is refused": {
			spec: strings.Replace(spec, "c.", "e.", 1), amendments: section,
		},
		"a file with no amendment is refused": {spec: spec, amendments: "nothing"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := foldAmendments(tc.spec, tc.amendments)
			if got != tc.want || (err == nil) != (tc.want != "") {
				t.Errorf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestBuildLock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	unshare, err := lockBuilds(syscall.LOCK_SH | syscall.LOCK_NB)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := HoldBuilds(); !errors.Is(err, errRefused) {
		t.Errorf("the reinstall held builds while a kiln run drove one: %v", err)
	}

	unshare()
	release, err := HoldBuilds()
	if err != nil {
		t.Fatalf("the reinstall could not hold builds once none ran: %v", err)
	}

	if _, err := lockBuilds(syscall.LOCK_SH | syscall.LOCK_NB); err == nil {
		t.Error("a kiln run drove a build while the reinstall held builds")
	}

	release()
}

// TestNext holds which queued spec a kiln run takes. One started for a single spec, as kiln accept starts it, takes
// every landing of accepted amendments too. A kiln accept that found it holding the lock counts on it to.
func TestNext(t *testing.T) {
	b := book{Specs: []*record{{Name: "y", State: queued}, {Name: "x", State: queued, Accepted: true},
		{Name: "z", State: queued, Accepted: true}}}
	var took []Spec
	for {
		r, found := b.next("z", 1)
		if !found {
			break
		}

		took = append(took, r.Name)
	}

	if !slices.Equal(took, []Spec{"x", "z"}) {
		t.Errorf("a kiln run for z took %q, want the accepted landings x and z", took)
	}
}

// TestAcceptDuringReinstall holds that an accept the reinstall refuses changes nothing.
func TestAcceptDuringReinstall(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	release, err := HoldBuilds()
	if err != nil {
		t.Fatal(err)
	}

	defer release()
	r := &Repo{store: store{dir: t.TempDir()}}
	stopped := record{Name: "x", State: stopped, Accepted: true, Build: "b", Rounds: 1, Why: "w"}
	if err := r.store.update(func(b *book) error { b.Specs = []*record{&stopped}; return nil }); err != nil {
		t.Fatal(err)
	}

	if err := r.Accept(context.Background(), "x", io.Discard); !errors.Is(err, errRefused) {
		t.Errorf("kiln accept during the reinstall answered %v, want it refused", err)
	}

	b, err := r.store.read()
	if err != nil {
		t.Fatal(err)
	}

	if got := *b.find("x"); !reflect.DeepEqual(got, stopped) {
		t.Errorf("a refused accept left %+v, want %+v", got, stopped)
	}
}

func TestAuditDid(t *testing.T) {
	audit := run{role: auditRole, why: reason{Why: whyAudit}}
	findings := run{role: "build", why: reason{Why: whyFindings}}
	for name, tc := range map[string]struct {
		audited bool
		runs    []run
		known   bool
		want    string
	}{
		"no audit run":                    {known: true, want: "the audit had not run"},
		"an audit run with no answer":     {runs: []run{audit, audit, audit}, known: true, want: "the audit ended with no result"},
		"an audit that sent findings":     {audited: true, runs: []run{audit, findings}, known: true, want: "the audit sent findings back"},
		"an audit that found nothing":     {audited: true, runs: []run{audit}, known: true, want: "the audit found nothing"},
		"an audit with nothing to read":   {audited: true, known: true, want: "the build changed nothing kiln audits, so no audit ran"},
		"a ledger that could not be read": {audited: true, want: "what the audit did is unknown"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := auditDid(tc.audited, tc.runs, tc.known); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNextStep holds the next step to spec 4's table: each command it names is one the book takes.
func TestNextStep(t *testing.T) {
	const why = "the gate still failed after its last round:\nreport"
	for name, tc := range map[string]struct {
		rec      record
		commands []string
		resume   bool
	}{
		"waiting for UAT": {rec: record{State: landed}, commands: []string{"accept", "queue"}},
		"waiting on its branch with amendments": {
			rec: record{State: amended}, commands: []string{"accept", "reject"},
		},
		"stopped past its last round": {
			rec: record{State: stopped, Session: "s", Why: why}, commands: []string{"queue"}, resume: true,
		},
		"stopped for a kiln error": {
			rec: record{State: stopped, Session: "s", Why: couldNotGoOn + "no disk"}, commands: []string{"queue"},
		},
		"stopped before its session began": {rec: record{State: stopped, Why: why}, commands: []string{"queue"}},
		"a landing of accepted amendments stopped past its last round": {
			rec:      record{State: stopped, Accepted: true, Session: "s", Why: why},
			commands: []string{"accept", "reject"}, resume: true,
		},
		"a landing of accepted amendments stopped for a kiln error": {
			rec:      record{State: stopped, Accepted: true, Session: "s", Why: couldNotGoOn + "main's checkout"},
			commands: []string{"accept", "reject"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			tc.rec.Name = "x"
			next := nextStep(tc.rec, "/cache/kiln/a b/x", "1a2b3c4d5e6f7a8b")
			var named []string
			for _, m := range regexp.MustCompile(`kiln (\w+) x\b`).FindAllStringSubmatch(next, -1) {
				named = append(named, m[1])
			}

			b := book{Specs: []*record{&tc.rec}}
			for _, c := range named {
				if err := b.may(c, "x", func(git.Rev) (bool, error) { return true, nil }); err != nil {
					t.Errorf("%q names kiln %s, which the book refuses: %v", next, c, err)
				}
			}

			slices.Sort(named)
			resume := strings.HasPrefix(next, "cd '/cache/kiln/a b/x' && claude --resume s, ")
			if !slices.Equal(named, tc.commands) || resume != tc.resume {
				t.Errorf("%q names kiln %q, and resumes the session %v; want %q, %v", next, named, resume,
					tc.commands, tc.resume)
			}
		})
	}
}

// TestLandChanges holds that main moves only to work that changes it. A session that ended with no result can leave
// its branch at main's tip. That work goes back to the session.
func TestLandChanges(t *testing.T) {
	for name, tc := range map[string]struct {
		work   string
		landed bool
	}{
		"work that changes a file lands":     {work: "echo 42 >x && git add x && git commit --quiet -m x", landed: true},
		"work that changes nothing does not": {work: "true"},
		"an empty commit changes nothing":    {work: "git commit --quiet --allow-empty -m nothing"},
	} {
		t.Run(name, func(t *testing.T) {
			b, repo, main := landingBuild(t)
			shell(t, string(b.tree), tc.work)
			head := git.Rev(shell(t, string(b.tree), "git rev-parse HEAD"))
			landed, back, err := b.land(t.Context(), head, nil)
			if err != nil {
				t.Fatal(err)
			}

			tip := git.Rev(shell(t, repo, "git rev-parse main"))
			got := landing{landed: landed != "", atLanded: tip == landed, atMain: tip == main,
				unchanged: back.why == whyUnchanged}
			want := landing{landed: tc.landed, atLanded: tc.landed, atMain: !tc.landed, unchanged: !tc.landed}
			if got != want {
				t.Errorf("landed %q, main at %s, setback %+v: %+v; want %+v", landed, tip, back, got, want)
			}
		})
	}
}

// landing is what TestLandChanges compares of a landing. It holds whether it landed, where main is, and whether the
// work went back unchanged.
type landing struct {
	landed, atLanded, atMain, unchanged bool
}

// landingBuild is a build of x in a new repository, on its branch from main. It answers the repository, and main.
func landingBuild(t *testing.T) (*build, string, git.Rev) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := t.TempDir()
	shell(t, repo, `git init --quiet -b main && git config user.name t && git config user.email t@t && `+
		`mkdir .kiln && echo '{"gate": ["true"], "rounds": 1}' >.kiln/kiln.json && git add . && `+
		`git commit --quiet -m start`)
	main := git.Rev(shell(t, repo, "git rev-parse HEAD"))
	r, err := Open(t.Context(), repo, os.Environ())
	if err != nil {
		t.Fatal(err)
	}

	rec := record{Name: "x", State: building}
	if err := r.store.update(func(k *book) error { k.Specs = []*record{&rec}; return nil }); err != nil {
		t.Fatal(err)
	}

	b := &build{r: r, rec: rec, tree: r.tree("x"), out: io.Discard}
	if err := r.git.Checkout(t.Context(), b.tree, Spec("x").branch(), main); err != nil {
		t.Fatal(err)
	}

	return b, repo, main
}

// shell runs script with sh in dir, and answers what it printed.
func shell(t *testing.T, dir, script string) string {
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", script, err, out)
	}

	return strings.TrimSpace(string(out))
}
