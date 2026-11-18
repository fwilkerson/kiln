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
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/fwilkerson/kiln/internal/git"
	"github.com/fwilkerson/practices/claim"
)

func TestGuard(t *testing.T) {
	const place = "/Users/dev/Library/Caches/kiln/repo/x"
	builds := side{role: buildRole, acceptance: []string{"acceptance/x/"}, harness: []string{"harness/"}}
	verifies := side{role: verifyRole, acceptance: []string{"acceptance/x/"}, harness: []string{"harness/"}}
	twoPaths := []string{"uat/x/", "integration/x/"}
	buildsTwo := side{role: buildRole, acceptance: twoPaths}
	verifiesTwo := side{role: verifyRole, acceptance: twoPaths}
	fixes := side{role: fixRole, acceptance: []string{"acceptance/"}}
	fixesTwo := side{role: fixRole, acceptance: []string{"uat/", "integration/"}}
	fixesTests := side{role: fixRole}
	fixesPython := side{role: fixRole, acceptance: []string{"tests/acceptance/test_"}}
	for name, tc := range map[string]struct {
		tool, cwd, file string
		side            side
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
		"nor an agent under .claude":                 {tool: "Write", file: place + "/.claude/agents/helper.md", refused: true},
		"nor a nested .claude's skill":               {tool: "Write", file: place + "/sub/.claude/skills/t/SKILL.md", refused: true},
		"nor a git directory":                        {tool: "Write", file: place + "/sub/.git/config", refused: true},
		"nor a notebook under .kiln":                 {tool: "NotebookEdit", file: place + "/.kiln/n.ipynb", refused: true},
		"a write that names no file is refused":      {tool: "Write", refused: true},
		"a path through /private is the same path":   {tool: "Write", file: "/private/tmp/w/.kiln/specs/x.md", refused: true},
		"a split build session writes its code":      {tool: "Write", file: place + "/x.go", side: builds},
		"and the harness":                            {tool: "Edit", file: place + "/harness/h.go", side: builds},
		"and another spec's tests":                   {tool: "Edit", file: place + "/acceptance/y/y_test.go", side: builds},
		"but not its spec's tests":                   {tool: "Write", file: place + "/acceptance/x/x_test.go", side: builds, refused: true},
		"nor its spec's tests in other letters":      {tool: "Write", file: place + "/Acceptance/X/x_test.go", side: builds, refused: true},
		"nor its spec's amendments":                  {tool: "Write", file: place + "/.kiln/specs/x.amend.md", side: builds, refused: true},
		"a verify session writes its spec's tests":   {tool: "Write", file: place + "/acceptance/x/x_test.go", side: verifies},
		"and its spec's amendments":                  {tool: "Write", file: place + "/.kiln/specs/x.amend.md", side: verifies},
		"and the harness too":                        {tool: "Write", file: place + "/harness/h.go", side: verifies},
		"but not code":                               {tool: "Write", file: place + "/x.go", side: verifies, refused: true},
		"nor another spec's tests":                   {tool: "Write", file: place + "/acceptance/y/y_test.go", side: verifies, refused: true},
		"nor a path that only starts like its tests": {tool: "Write", file: place + "/acceptance/xy/t.go", side: verifies, refused: true},
		"a fix writes code":                          {tool: "Write", file: place + "/x.go", side: fixes},
		"but no test under the acceptance path":      {tool: "Write", file: place + "/acceptance/y/y_test.go", side: fixes, refused: true},
		"nor the amendment file":                     {tool: "Write", file: place + "/.kiln/specs/x.amend.md", side: fixesTests, refused: true},
		"a fix whose monitor says tests writes them": {tool: "Write", file: place + "/acceptance/y/y_test.go", side: fixesTests},
		"a build session writes under neither path":  {tool: "Write", file: place + "/integration/x/x_test.go", side: buildsTwo, refused: true},
		"nor under the first":                        {tool: "Write", file: place + "/uat/x/x_test.go", side: buildsTwo, refused: true},
		"but writes another spec's second path":      {tool: "Write", file: place + "/integration/y/y_test.go", side: buildsTwo},
		"a verify session writes under the second":   {tool: "Write", file: place + "/integration/x/x_test.go", side: verifiesTwo},
		"and under the first":                        {tool: "Write", file: place + "/uat/x/x_test.go", side: verifiesTwo},
		"a fix writes under no path's fixed part":    {tool: "Write", file: place + "/integration/y/y_test.go", side: fixesTwo, refused: true},
		"and writes code beside them":                {tool: "Write", file: place + "/internal/integration/i.go", side: fixesTwo},
		"a fixed part with no slash guards a prefix": {tool: "Write", file: place + "/tests/acceptance/test_x.py", side: fixesPython, refused: true},
		"and leaves what only shares its directory":  {tool: "Write", file: place + "/tests/acceptance/conftest.py", side: fixesPython},
	} {
		t.Run(name, func(t *testing.T) {
			input := map[string]string{"file_path": tc.file}
			if tc.tool == "NotebookEdit" {
				input = map[string]string{"notebook_path": tc.file}
			}

			in, _ := json.Marshal(map[string]any{"tool_name": tc.tool, "cwd": tc.cwd, "tool_input": input})
			var out bytes.Buffer
			worktree := git.Worktree(place)
			if strings.HasPrefix(tc.file, "/private") {
				worktree = "/tmp/w"
			}

			g := guard{spec: "x", place: worktree, side: tc.side}
			if err := Hook(g.hookArgs(), bytes.NewReader(in), &out); err != nil {
				t.Fatal(err)
			}

			if refused := strings.Contains(out.String(), `"permissionDecision":"deny"`); refused != tc.refused {
				t.Errorf("refused %v, want %v: %s", refused, tc.refused, out.String())
			}
		})
	}
}

// TestGuardLinks holds that the hook judges a write by where it lands, after each link in its path. The links are
// real, in the worktree x, which w links to.
func TestGuardLinks(t *testing.T) {
	top := t.TempDir()
	shell(t, top, `mkdir -p main/.kiln main/.git/hooks x/.claude x/sub && ln -s x w && cd x && `+
		`ln -s ../main/.kiln k && ln -s "$PWD/../main/.git" g && ln -s .claude c && ln -s . a && `+
		`ln -s ../planted b && ln -s sub s`)
	const outside = "is outside this build's worktree"
	for name, tc := range map[string]struct{ place, file, refusal string }{
		"a link to the main checkout's .kiln":     {file: "k/monitors/planted", refusal: outside},
		"a link to git's directory":               {file: "g/hooks/post-commit", refusal: outside},
		"a long spelling, then a link to no file": {file: strings.Repeat("a/", 30) + "b", refusal: outside},
		"a link to the worktree's .claude": {file: "c/settings.local.json",
			refusal: ".claude/settings.local.json is a Claude Code settings file"},
		"a link to the worktree's own code is kept":   {file: "s/x.go"},
		"and kept in a worktree kiln names by a link": {place: "w", file: "s/x.go"},
	} {
		t.Run(name, func(t *testing.T) {
			place := filepath.Join(top, cmp.Or(tc.place, "x"))
			in, _ := json.Marshal(map[string]any{"tool_name": "Write", "cwd": place,
				"tool_input": map[string]string{"file_path": tc.file}})
			var out bytes.Buffer
			g := guard{spec: "x", place: git.Worktree(place)}
			if err := Hook(g.hookArgs(), bytes.NewReader(in), &out); err != nil {
				t.Fatal(err)
			}

			if got := out.String(); (got == "") != (tc.refusal == "") || !strings.Contains(got, tc.refusal) {
				t.Errorf("the hook answered %q, want the refusal %q", got, tc.refusal)
			}
		})
	}
}

// TestHookArguments holds that the hook refuses arguments kiln never writes, so no slip leaves a session unguarded.
func TestHookArguments(t *testing.T) {
	for name, args := range map[string][]string{
		"no spec":                         {"--place", "/w"},
		"no worktree":                     {"--spec", "x"},
		"a role kiln never writes":        {"--spec", "x", "--place", "/w", "--role", "verfy", "--acceptance", "a/"},
		"a split build's side, pathless":  {"--spec", "x", "--place", "/w", "--role", verifyRole},
		"a flag kiln no longer writes":    {"--spec", "x", "--place", "/w", "--fix"},
		"a flag with no value":            {"--spec", "x", "--place", "/w", "--role"},
		"a word after the flags":          {"--spec", "x", "--place", "/w", "y"},
		"an acceptance path with no role": {"--spec", "x", "--place", "/w", "--acceptance", "a/"},
		"two acceptance paths, no role":   {"--spec", "x", "--place", "/w", "--acceptance", "a/", "--acceptance", "b/"},
		"a harness in a fix":              {"--spec", "x", "--place", "/w", "--role", fixRole, "--harness", "h/"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := Hook(args, strings.NewReader("{}"), io.Discard); !errors.Is(err, ErrUsage) {
				t.Errorf("Hook answered %v, want ErrUsage", err)
			}
		})
	}
}

func TestGate(t *testing.T) {
	const failing = "--- FAIL: TestFlaky (0.00s)\n    flaky_test.go:5: no\nFAIL\nFAIL\texample.com/s\t0.1s\n"
	const other = "--- FAIL: TestNew (0.00s)\nFAIL\nFAIL\texample.com/s\t0.1s\n"
	const broken = "# example.com/s\n./s.go:3:1: syntax error\nFAIL\texample.com/s [build failed]\n"
	const hung = "panic: test timed out after 10m0s\nFAIL\texample.com/t\t600.0s\n"
	const exited = "FAIL\n--- FAIL: TestFlaky (0.00s)\nFAIL\texample.com/s\t0.1s\n"
	const flaky = "TestFlaky (example.com/s)"
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
		"every command exits 0": {tip: goTest(0, ""), passed: true},
		"a command fails": {
			tip:     []check{{Command: "go vet ./...", Exit: 1, Output: "vet: x"}},
			main:    []check{{Command: "go vet ./...", Exit: 0}},
			failing: []string{"`go vet ./...`"},
		},
		"a test fails that passes at main": {
			tip: goTest(1, other), main: goTest(0, ""), failing: []string{"TestNew (example.com/s)"},
		},
		"a test fails that fails at main too": {
			tip: goTest(1, failing), main: goTest(1, failing), passed: true, atMain: []string{flaky},
		},
		"a test fails at main, and another fails only at the tip": {
			tip: goTest(1, failing+other), main: goTest(1, failing),
			atMain: []string{flaky}, failing: []string{"TestNew (example.com/s)"},
		},
		"a test that fails at main is named though the tip passes": {
			tip: goTest(0, ""), main: goTest(1, failing), passed: true, atMain: []string{flaky},
		},
		"a build that fails at main too still fails": {
			tip: goTest(1, broken), main: goTest(1, broken), failing: []string{"`go test ./...`"},
		},
		"a package that hangs, while a test fails at main too": {
			tip: goTest(1, failing+hung), main: goTest(1, failing),
			atMain: []string{flaky}, failing: []string{"`go test ./...`"},
		},
		"a test binary that exits before it reports, after another run's lone FAIL line": {
			tip: goTest(1, exited), main: goTest(1, failing), atMain: []string{flaky},
			failing: []string{"`go test ./...`"}, mentions: "ended before their tests reported: example.com/s.",
		},
		"a test that fails at main under another command": {
			tip:    []check{{Command: "go test -race ./...", Exit: 1, Output: failing}},
			main:   goTest(1, failing),
			atMain: []string{flaky}, failing: []string{flaky},
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
			tip:     goTest(0, ""),
			refused: map[string]string{".kiln/specs/x.md": ".kiln/specs/x.md is under .kiln/"},
			failing: []string{".kiln/specs/x.md"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			v := judge(readOutputs(tc.tip), readOutputs(tc.main), tc.refused)
			got := judgmentOf(v, tc.mentions)
			want := judgment{
				tc.passed, fmt.Sprintf("%q", tc.atMain), fmt.Sprintf("%q", tc.failing), !tc.passed, tc.hinted, true,
			}
			if got != want {
				t.Errorf("got %+v, report %q; want %+v", got, v.report, want)
			}
		})
	}
}

// goTest is the check of a go test run that exited with exit and printed output.
func goTest(exit int, output string) []check {
	return []check{{Command: "go test ./...", Exit: exit, Output: output}}
}

// TestReportNamesLostFailure holds the rule that a gate's report names each failed test. A check keeps only the end
// of a long output, so it may lose an early failure's line. The session would then read that the test passed.
func TestReportNamesLostFailure(t *testing.T) {
	const command = `printf -- '--- FAIL: TestLost (0.00s)\nFAIL\nFAIL\texample.com/s\t0.1s\n'; ` +
		`head -c 20000 /dev/zero | tr '\0' x; exit 1`
	tip, err := runChecks(t.Context(), git.Worktree(t.TempDir()), []string{command}, os.Environ())
	if err != nil {
		t.Fatal(err)
	}

	v := judge(tip, []check{{Command: command}}, nil)
	for _, want := range []string{"These tests failed: TestLost (example.com/s).", "lacks its first"} {
		if !strings.Contains(v.report, want) {
			t.Errorf("the report lacks %q: %.300q", want, v.report)
		}
	}

	if strings.Contains(tip[0].Output, "--- FAIL") {
		t.Fatalf("the check kept the failure's line, so the test shows nothing: %.300q", tip[0].Output)
	}
}

// TestCommandLeavesChild holds that a command which leaves a child behind ends once its shell exits. The child gets
// 10 s to close the command's output. kiln then kills the command's group, and keeps what the command printed.
func TestCommandLeavesChild(t *testing.T) {
	for name, run := range map[string]func(dir git.Worktree, command string) (string, error){
		"a gate's command": func(dir git.Worktree, command string) (string, error) {
			c, err := runCheck(t.Context(), dir, command, os.Environ())
			return c.Output, err
		},
		"a monitor's check, command or gauge": func(dir git.Worktree, command string) (string, error) {
			out, err := runShell(t.Context(), dir, command, os.Environ())
			return out.both, err
		},
		"a writable command": func(dir git.Worktree, command string) (string, error) {
			paths, err := (&Repo{top: string(dir)}).printed(t.Context(), command)
			return strings.Join(paths, " "), err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			began := time.Now()
			out, err := run(git.Worktree(dir), "sleep 30 & echo $! > child; echo /started")
			took := time.Since(began)
			raw, _ := os.ReadFile(filepath.Join(dir, "child"))
			var pid int
			_, _ = fmt.Sscan(string(raw), &pid)
			if err != nil || !claim.Holds(strings.TrimSpace(out) == "/started", took < 15*time.Second) {
				t.Fatalf("answered %q, %v after %v; want /started within 15 s", out, err, took)
			}

			for range 50 {
				if syscall.Kill(pid, 0) != nil {
					return
				}

				time.Sleep(100 * time.Millisecond)
			}

			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Errorf("the child, process %d, still runs", pid)
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

// judgmentOf is what TestGate compares of v, where mentions is what its report must hold.
func judgmentOf(v verdict, mentions string) judgment {
	return judgment{
		v.passed, fmt.Sprintf("%q", v.atMain), fmt.Sprintf("%q", v.failing), v.report != "",
		strings.Contains(v.report, "KILN_JUNIT"), strings.Contains(v.report, mentions),
	}
}

// readOutputs reads each check's output for the Go tests that failed, as runChecks does, and answers checks. It reads
// only go test's output, and keeps what a row says its JUnit XML named.
func readOutputs(checks []check) []check {
	for i := range checks {
		failed, cutShort, broken := failedTests(checks[i].Output)
		checks[i].Failed, checks[i].CutShort = append(checks[i].Failed, failed...), cutShort
		checks[i].Broken = checks[i].Broken || broken
	}

	return checks
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
	"queue a monitor's fix":                    {given: "fix waiting on its branch", command: "queue"},
	"queue a monitor's stopped fix":            {given: "fix stopped", command: "queue"},
	"accept a building fix":                    {given: "fix building", command: "accept"},
	"accept a stopped fix":                     {given: "fix stopped", command: "accept"},
	"accept a waiting fix":                     {given: "fix waiting on its branch", command: "accept", then: "fix waiting on its branch"},
	"accept a fix's queued landing":            {given: "fix queued landing", command: "accept"},
	"accept a fix's stopped landing":           {given: "fix stopped landing", command: "accept", then: "fix queued landing"},
	"reject a building fix":                    {given: "fix building", command: "reject"},
	"reject a stopped fix":                     {given: "fix stopped", command: "reject"},
	"reject a waiting fix":                     {given: "fix waiting on its branch", command: "reject", then: none},
	"reject a fix's stopped landing":           {given: "fix stopped landing", command: "reject", then: none},
}

// TestHaltedRefusal holds what a command says of x, building for a kiln fire that is gone. A plain kiln fire takes
// up a spec's build or landing, so the refusal names it. A fix's landing keeps its own refusal.
func TestHaltedRefusal(t *testing.T) {
	for name, tc := range map[string]struct {
		given, command, says string
	}{
		"queue a halted build":          {given: "building", command: queueCommand, says: "it is halted, as " + haltedNext},
		"queue a halted landing":        {given: "building landing", command: queueCommand, says: "it is halted, as " + haltedNext},
		"accept a halted landing":       {given: "building landing", command: acceptCommand, says: "it is halted, as " + haltedNext},
		"reject a halted landing":       {given: "building landing", command: rejectCommand, says: "it is halted, as " + haltedNext},
		"accept a fix's halted landing": {given: "fix building landing", command: acceptCommand, says: "its landing is building"},
	} {
		t.Run(name, func(t *testing.T) {
			b := givenBook(tc.given)
			if got, err := b.refusal(tc.command, "x", nil); got != tc.says || err != nil {
				t.Errorf("kiln %s says %q, %v; want %q", tc.command, got, err, tc.says)
			}
		})
	}
}

// givenBook is a book that holds the spec x in the state given words, as a row's given does. "fix " starts a
// monitor's fix.
func givenBook(given string) book {
	if given == "" {
		return book{}
	}

	given, landing := strings.CutSuffix(given, " landing")
	given, fix := strings.CutPrefix(given, "fix ")
	return book{Specs: []*record{{Name: "x", State: state(given), Accepted: landing, Approved: "old", Session: "s",
		Build: "b", Rounds: 2, Audited: "read", Why: "w", FailingAtMain: []string{"TestF"}, Monitor: fix}}}
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
	case strings.HasSuffix(tc.then, "queued landing"):
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
		noBuild: r.Build == "", audited: r.Audited != "", rounds: r.Rounds, failingAtMain: r.FailingAtMain != nil}
}

// thenOf words the state of r as a row's then does.
func thenOf(r *record) string {
	if r == nil {
		return none
	}

	then := string(r.State)
	if r.Accepted {
		then += " landing"
	}

	if r.Monitor {
		then = "fix " + then
	}

	return then
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
		t.Errorf("the reinstall held builds while a kiln fire drove one: %v", err)
	}

	unshare()
	release, err := HoldBuilds()
	if err != nil {
		t.Fatalf("the reinstall could not hold builds once none ran: %v", err)
	}

	if _, err := lockBuilds(syscall.LOCK_SH | syscall.LOCK_NB); err == nil {
		t.Error("a kiln fire drove a build while the reinstall held builds")
	}

	release()
}

// TestNext holds which queued spec a kiln fire takes. One started for a single spec, as kiln accept starts it, takes
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
		t.Errorf("a kiln fire for z took %q, want the accepted landings x and z", took)
	}
}

// TestNextFix holds that a fix's record is taken only as an accepted landing. A run started for its name or for the
// landings takes it. kiln fire takes none, so a monitor's fix is never a spec's build.
func TestNextFix(t *testing.T) {
	for name, tc := range map[string]struct {
		only Spec
		want []Spec
	}{
		"kiln fire":                 {only: "", want: []Spec{"s", "y"}},
		"a kiln accept of the fix":  {only: "f", want: []Spec{"f", "y"}},
		"a kiln accept of a spec":   {only: "s", want: []Spec{"s", "y"}},
		"kiln monitor run, landing": {only: landings, want: []Spec{"f", "y"}},
	} {
		t.Run(name, func(t *testing.T) {
			b := book{Specs: []*record{{Name: "f", State: queued, Accepted: true, Monitor: true},
				{Name: "g", State: queued, Monitor: true}, {Name: "s", State: queued},
				{Name: "y", State: queued, Accepted: true}}}
			var took []Spec
			for r, found := b.next(tc.only, 1); found; r, found = b.next(tc.only, 1) {
				took = append(took, r.Name)
			}

			if !slices.Equal(took, tc.want) {
				t.Errorf("took %q, want %q", took, tc.want)
			}
		})
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
		"no audit run":                {known: true, want: "the audit had not run"},
		"an audit run with no answer": {runs: []run{audit, audit, audit}, known: true, want: "the audit ended with no result"},
		"an audit that sent findings": {audited: true, runs: []run{audit, findings}, known: true, want: "the audit sent findings back"},
		"an audit that found nothing": {audited: true, runs: []run{audit}, known: true, want: "the audit found nothing"},
		"an audit that read the work again": {audited: true, runs: []run{audit, findings, audit}, known: true,
			want: "the audit sent findings back, and read the work 2 times"},
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
		// rerun is a step that runs the monitor again, by name.
		rerun bool
		// says is text the step holds.
		says string
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
		"a fix waiting on its branch": {
			rec: record{State: waiting, Monitor: true}, commands: []string{"accept", "reject"},
		},
		"a fix's landing stopped": {
			rec: record{State: stopped, Accepted: true, Monitor: true}, commands: []string{"accept", "reject"},
		},
		"a fix's landing queued": {
			rec: record{State: queued, Accepted: true, Monitor: true}, says: "the next kiln monitor run, which lands it",
		},
		"a fix's landing building": {
			rec:  record{State: building, Accepted: true, Monitor: true, runner: runner{PID: os.Getpid()}},
			says: "wait",
		},
		"a fix's landing whose kiln is gone": {
			rec: record{State: building, Accepted: true, Monitor: true}, says: "run kiln monitor run x, which lands it",
		},
		"a fix building": {
			rec: record{State: building, Monitor: true, Session: "s", runner: runner{PID: os.Getpid()}}, says: "wait",
		},
		"a fix whose kiln is gone": {
			rec: record{State: building, Monitor: true, Session: "s"}, says: "run kiln monitor run x, which drops",
		},
		// The book has no command for kiln monitor run x, so the step's text names it.
		"a fix stopped": {
			rec: record{State: stopped, Monitor: true, Session: "s"}, commands: nil, rerun: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			tc.rec.Name = "x"
			next := nextStep(tc.rec, "/cache/kiln/a b/x", "1a2b3c4d5e6f7a8b")
			named := namedIn(t, next, &tc.rec)
			slices.Sort(named)
			resume := strings.HasPrefix(next, "cd '/cache/kiln/a b/x' && claude --resume s, ")
			rerun := strings.HasSuffix(next, "kiln monitor run x")
			if !claim.Holds(slices.Equal(named, tc.commands), resume == tc.resume, rerun == tc.rerun,
				strings.Contains(next, tc.says)) {
				t.Errorf("%q names kiln %q, resumes the session %v, runs the monitor again %v; want %q, %v, %v, "+
					"with the text %q", next, named, resume, rerun, tc.commands, tc.resume, tc.rerun, tc.says)
			}
		})
	}
}

// TestFixStep holds a fix's next step to spec 4's table in every state, accepted or not. A new state needs no row. An
// accepted fix that conflicts with main names kiln reject alone, since no session resolves the conflict.
func TestFixStep(t *testing.T) {
	for _, s := range []state{queued, building, stopped, landed, amended, waiting} {
		for _, accepted := range []bool{false, true} {
			rec := record{Name: "x", State: s, Accepted: accepted, Monitor: true}
			namedIn(t, fixStep(rec), &rec)
		}
	}

	rec := record{Name: "x", State: stopped, Accepted: true, Monitor: true, Why: fixConflicts + "docs.txt"}
	if named := namedIn(t, fixStep(rec), &rec); !slices.Equal(named, []string{"reject"}) {
		t.Errorf("a conflicted fix's step %q names kiln %v; want kiln reject alone", fixStep(rec), named)
	}
}

// namedIn is each kiln command the step next names on x. It fails t for one the book refuses the record rec.
func namedIn(t *testing.T, next string, rec *record) []string {
	t.Helper()
	b := book{Specs: []*record{rec}}
	var named []string
	for _, m := range regexp.MustCompile(`kiln (\w+) x\b`).FindAllStringSubmatch(next, -1) {
		named = append(named, m[1])
		if err := b.may(m[1], "x", func(git.Rev) (bool, error) { return true, nil }); err != nil {
			t.Errorf("%s: %q names kiln %s, which the book refuses: %v", shownState(rec), next, m[1], err)
		}
	}

	return named
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

// TestLandedBefore holds what the next kiln makes of an accepted landing that moved main before kiln died. kiln fire
// retires the spec, and kiln monitor run ends the fix landed. Neither lands it again.
func TestLandedBefore(t *testing.T) {
	for name, tc := range map[string]struct {
		monitor bool
		next    func(*build, context.Context) error
		outcome string
	}{
		"kiln fire retires an accepted spec": {next: (*build).run, outcome: outAccepted},
		"kiln monitor run ends an accepted fix landed": {monitor: true, outcome: outLanded,
			next: func(b *build, ctx context.Context) error { return b.r.dropLost(ctx, b.rec, b.out) }},
	} {
		t.Run(name, func(t *testing.T) {
			next, landed := landedThenKilled(t, tc.monitor)
			if err := tc.next(next, t.Context()); err != nil {
				t.Fatal(err)
			}

			k, _ := next.r.store.read()
			entries, _ := next.r.store.entries()
			last := entries[len(entries)-1]
			tip := git.Rev(shell(t, next.r.top, "git rev-parse main"))
			if !claim.Holds(tip == landed, k.find("x") == nil, last.Outcome == tc.outcome) {
				t.Errorf("main at %s, landed at %s; x is %s, ending %q; want x %s, main held at its landing",
					short(tip), short(landed), thenOf(k.find("x")), last.Outcome, tc.outcome)
			}
		})
	}
}

// landedThenKilled is the build of x, an accepted landing, as the next kiln takes it up. The kiln before died once its
// landing moved main. It answers the commit main moved to. A monitor's build is the fix of the monitor x.
func landedThenKilled(t *testing.T, monitor bool) (*build, git.Rev) {
	t.Setenv(openVar, "true")
	b, repo, _ := landingBuild(t)
	shell(t, repo, `echo '{"gate": ["true"], "monitors": [{"name": "x", "every": "1h", "command": "true", `+
		`"tests": true}]}' >.kiln/kiln.json && git commit --quiet -am monitor`)
	b.rec = record{Name: "x", State: building, Accepted: true, Monitor: monitor, Build: "b",
		Approved: git.Rev(shell(t, repo, "git rev-parse HEAD"))}
	err := b.r.store.update(func(k *book) error { k.Specs = []*record{new(b.rec)}; return nil })
	if err != nil {
		t.Fatal(err)
	}

	shell(t, string(b.tree), "echo 42 >x && git add x && git commit --quiet -m x")
	landed, _, err := b.land(t.Context(), git.Rev(shell(t, string(b.tree), "git rev-parse HEAD")), nil)
	if err != nil || landed == "" {
		t.Fatalf("the landing answered %q, %v", landed, err)
	}

	k, err := b.r.store.read()
	if err != nil {
		t.Fatal(err)
	}

	return &build{r: b.r, rec: *k.find("x"), tree: b.tree, out: io.Discard}, landed
}

// landing is what TestLandChanges compares of a landing. It holds whether it landed, where main is, and whether the
// work went back unchanged.
type landing struct {
	landed, atLanded, atMain, unchanged bool
}

// landingBuild is a build of x in a new repository, on its branch from main. It answers the repository, and main.
func landingBuild(t *testing.T) (*build, string, git.Rev) {
	// kiln's git never sees GIT_CONFIG_GLOBAL, so HOME and XDG_CONFIG_HOME keep the developer's config from it.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
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

// TestGaugeRules holds what kiln.json may name as a gauge. It holds what a command's output reads as, and when a
// reading crossed its line.
func TestGaugeRules(t *testing.T) {
	for name, tc := range map[string]struct {
		gauges  string
		refused bool
	}{
		"a gauge with a min":            {gauges: `[{"name": "a", "command": "echo 1", "min": 0}]`},
		"two gauges, each with a max":   {gauges: `[{"name": "a", "command": "echo 1", "max": 0}, {"name": "b", "command": "echo 1", "max": 2}]`},
		"a gauge with neither":          {gauges: `[{"name": "a", "command": "echo 1"}]`, refused: true},
		"a gauge with both":             {gauges: `[{"name": "a", "command": "echo 1", "min": 0, "max": 1}]`, refused: true},
		"a gauge with no name":          {gauges: `[{"command": "echo 1", "min": 0}]`, refused: true},
		"two gauges that share a name":  {gauges: `[{"name": "a", "command": "echo 1", "min": 0}, {"name": "a", "command": "echo 2", "min": 0}]`, refused: true},
		"a gauge with a key kiln lacks": {gauges: `[{"name": "a", "command": "echo 1", "min": 0, "unit": "%"}]`, refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseConfig([]byte(`{"gate": ["true"], "rounds": 1, "gauges": ` + tc.gauges + `}`))
			if (err != nil) != tc.refused {
				t.Errorf("parseConfig answers %v, want refused %v", err, tc.refused)
			}
		})
	}

	type reading struct {
		value float64
		ok    bool
	}
	for out, want := range map[string]reading{
		"12\n":                    {12, true},
		"mutants: 152\n  82.5 \n": {82.5, true},
		"      2\n":               {2, true},
		"-1e3":                    {-1000, true},
		"0":                       {0, true},
		"":                        {},
		"82%\n":                   {},
		"0x10\n":                  {},
		"NaN\n":                   {},
		"Inf\n":                   {},
		"82.5\ndone\n":            {},
	} {
		var got reading
		if got.value, got.ok = readingOf(out); got != want {
			t.Errorf("%q reads as %v, %v, want %v, %v", out, got.value, got.ok, want.value, want.ok)
		}
	}

	// An interrupt of kiln is not the command's failure, so it reads no unknown.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := (&Repo{}).readGauge(ctx, git.Worktree(t.TempDir()), "sleep 10", ""); err == nil {
		t.Error("an interrupted gauge answers no error")
	}

	at := func(v float64) *float64 { return &v }
	for name, tc := range map[string]struct {
		line    gaugeLine
		value   *float64
		crossed bool
	}{
		"below a min crossed":       {gaugeLine{Min: at(80)}, at(79.9), true},
		"on a min did not":          {gaugeLine{Min: at(80)}, at(80), false},
		"above a max crossed":       {gaugeLine{Max: at(0)}, at(1), true},
		"on a max did not":          {gaugeLine{Max: at(0)}, at(0), false},
		"an unknown reading never":  {gaugeLine{Min: at(80)}, nil, false},
		"nor against a max of zero": {gaugeLine{Max: at(0)}, nil, false},
	} {
		if got := tc.line.crossed(tc.value); got != tc.crossed {
			t.Errorf("%s: crossed is %v", name, got)
		}
	}
}

// TestGaugeVerdict holds how a monitor's run reads its gauge: as a check's verdict, summed up as kiln status shows
// it. A reading kiln could not take couldn't run, and says why.
func TestGaugeVerdict(t *testing.T) {
	for name, tc := range map[string]struct {
		command, summary, found string
		ran, fix                bool
	}{
		"on its line":                 {command: "echo 0", ran: true, summary: "count: 0, line ≤ 0"},
		"crossed":                     {command: "echo a.txt is low; echo 1", ran: true, fix: true, summary: "count: 1, line ≤ 0, crossed", found: "a.txt is low"},
		"no number":                   {command: "echo nothing to read", summary: "count: its command's last line is no number"},
		"a command that says why":     {command: "echo no such tool >&2; echo 3; exit 1", summary: "no such tool"},
		"a command that says nothing": {command: "exit 3", summary: "count: its command failed: exit status 3"},
	} {
		g := gauge{Name: "count", Command: tc.command, Max: new(0.0)}
		c, err := (&Repo{env: os.Environ()}).measured(t.Context(), git.Worktree(t.TempDir()), g, "")
		got := fmt.Sprintf("%v %v %q %q", c.ran, c.fix, c.summary, c.found)
		if want := fmt.Sprintf("%v %v %q %q", tc.ran, tc.fix, tc.summary, tc.found); err != nil || got != want {
			t.Errorf("%s: got %s, %v; want %s", name, got, err, want)
		}
	}
}

// TestGaugeTree holds that a gauge which leaves a file git can't remove stops no later reading. Each landing gets a
// reading of each gauge, and each monitor's run gets one too.
func TestGaugeTree(t *testing.T) {
	r, repo := scratchRepo(t)
	t.Cleanup(func() { _ = removeTree(r.trees) })
	config := `{"gate": ["true"], "rounds": 1, "gauges": [{"name": "n", "max": 5, ` +
		`"command": "mkdir -p ro/sub && touch ro/sub/f && chmod 555 ro/sub; echo 1"}]}`
	shell(t, repo, "mkdir .kiln && echo '"+config+"' >.kiln/kiln.json && git add . && git commit --quiet -m start")
	b := &build{r: r, rec: record{Name: "x", Build: "b-x"}, out: io.Discard}
	for landing := range 2 {
		shell(t, repo, fmt.Sprintf("git commit --quiet --allow-empty -m landing-%d", landing))
		b.onto, b.landed = b.landed, git.Rev(shell(t, repo, "git rev-parse HEAD"))
		if err := b.readGauges(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	if read := gaugeReadings(t, r); len(read) != 2 {
		t.Errorf("the landings read %v, want a reading of 1 for each of 2", read)
	}

	// A monitor's run reads its gauge in the monitors' own worktree, which a check then shares.
	c, err := r.config(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range []monitor{{Name: "m", Gauge: "n"}, {Name: "m", Gauge: "n"}, {Name: "u", Check: checkHealthy}} {
		if found, err := r.firstCheck(t.Context(), c, m, "main"); err != nil || !found.ran {
			t.Errorf("%s's run answers %v, %q; want it ran", m.Name, err, found.summary)
		}
	}
}

// checkHealthy is a check that finds nothing to fix.
const checkHealthy = `echo '{"fix": false, "summary": "fine"}'`

// countGauge counts the .txt files changed since $KILN_BASE that hold the word low, and names each. It reads no
// number where no .txt file changed. It fails while the commit holds broken.txt.
const countGauge = `if [ -e broken.txt ]; then echo cannot parse broken.txt >&2; exit 2; fi
files=$(git diff --name-only --diff-filter=d "$KILN_BASE" HEAD -- '*.txt')
[ -z "$files" ] && exit 0
n=0
for f in $files; do if grep -q low "$f"; then echo "$f is low"; n=$((n+1)); fi; done
echo $n
`

// TestGatedGauge holds how a fix's gate reads its monitor's gauge: over the fix's change, against main's line. A
// crossed reading or a failed command fails the gate. A reading of no number never crossed.
func TestGatedGauge(t *testing.T) {
	for name, tc := range map[string]struct {
		fix, gauge   string
		passed       bool
		holds, lacks string
	}{
		"a fix that brings its file on the line": {fix: "echo fine >a.txt", passed: true},
		"a fix whose file still crosses": {fix: "echo still low >a.txt", holds: "count: 1, line ≤ 0, crossed\n\n" +
			"a.txt is low", lacks: "b.txt"},
		"a fix that deletes what crossed":        {fix: "git rm --quiet a.txt b.txt", passed: true},
		"a fix of no file the gauge reads":       {fix: "echo x >notes.md", passed: true},
		"a gauge whose command fails":            {fix: "echo x >broken.txt", holds: "cannot parse broken.txt"},
		"a gauge main's kiln.json no longer has": {fix: "echo still low >a.txt", gauge: "gone", passed: true},
	} {
		t.Run(name, func(t *testing.T) {
			b := gaugedFix(t, tc.fix, monitor{Name: "m", Gauge: cmp.Or(tc.gauge, "count")})
			v, err := b.gatedCheck(t.Context(), verdict{passed: true})
			if err != nil {
				t.Fatal(err)
			}

			if v.passed != tc.passed {
				t.Errorf("passed %v, want %v: %s", v.passed, tc.passed, v.report)
			}

			if !strings.Contains(v.report, tc.holds) {
				t.Errorf("the report holds no %q:\n%s", tc.holds, v.report)
			}

			if tc.lacks != "" && strings.Contains(v.report, tc.lacks) {
				t.Errorf("the report holds %q:\n%s", tc.lacks, v.report)
			}
		})
	}
}

// gaugedFix is the fix of m, in a repository whose main counts a.txt and b.txt against countGauge's line. The fix
// commits what the shell script fix changes.
func gaugedFix(t *testing.T, fix string, m monitor) *build {
	t.Helper()
	r, repo := scratchRepo(t)
	t.Cleanup(func() { _ = removeTree(r.trees) })
	config := `{"gate": ["true"], "rounds": 1, "gauges": [{"name": "count", "max": 0, ` +
		`"command": "sh .kiln/gauges/count"}]}`
	shell(t, repo, "mkdir -p .kiln/gauges && echo '"+config+"' >.kiln/kiln.json && echo low >a.txt && "+
		"echo low >b.txt && echo fine >c.txt")
	if err := os.WriteFile(filepath.Join(repo, ".kiln/gauges/count"), []byte(countGauge), 0o755); err != nil {
		t.Fatal(err)
	}

	tree := filepath.Join(t.TempDir(), "m")
	shell(t, repo, "git add . && git commit --quiet -m start && git worktree add --quiet -b kiln/m "+tree+" main")
	shell(t, tree, fix+" && git add -A && git commit --quiet -m fix")
	return &build{r: r, rec: record{Name: "m", Monitor: true}, tree: git.Worktree(tree), out: io.Discard,
		fix: &fixing{monitor: m}}
}

// gaugeReadings is each reading of a gauge the store holds, as the landing it read and its value.
func gaugeReadings(t *testing.T, r *Repo) []string {
	t.Helper()
	entries, err := r.store.entries()
	if err != nil {
		t.Fatal(err)
	}

	var read []string
	for _, e := range entries {
		if e.Kind == gaugeKind && e.Value != nil {
			read = append(read, fmt.Sprintf("%s %v", short(e.Landed), *e.Value))
		}
	}

	return read
}

// TestBugsLine holds how kiln reads the verify session's bugs line. A line it can't read never passes as none.
func TestBugsLine(t *testing.T) {
	tests := map[int]string{1: "### 1.", 2: "### 2.", 4: "### 4."}
	for name, tc := range map[string]struct {
		line    string
		numbers []int
		read    bool
	}{
		"none":                           {line: "none", read: true},
		"one test":                       {line: "1", numbers: []int{1}, read: true},
		"tests by commas":                {line: "2, 4", numbers: []int{2, 4}, read: true},
		"tests by spaces, once each":     {line: "4 2 4", numbers: []int{4, 2}, read: true},
		"a test with a period":           {line: "1."},
		"a test by its code's name":      {line: "AT1"},
		"a test by a hash":               {line: "#1"},
		"a number the spec holds none":   {line: "3"},
		"a known test beside an unknown": {line: "1, 3"},
		"nothing":                        {line: ""},
		"none, and a test":               {line: "none, 1"},
	} {
		t.Run(name, func(t *testing.T) {
			numbers, read := bugsLine(tc.line, tests)
			if !slices.Equal(numbers, tc.numbers) || read != tc.read {
				t.Errorf("bugsLine(%q) = %v, %v; want %v, %v", tc.line, numbers, read, tc.numbers, tc.read)
			}
		})
	}
}

// TestSumUpPlaces holds that a stopped build's summary names its stack only where its worktree is. A split build
// stopped before its first stack has none, and its summary still names each session's clone.
func TestSumUpPlaces(t *testing.T) {
	dir := t.TempDir()
	r := &Repo{store: store{dir: dir}}
	rec := record{Name: "x", State: stopped, Session: "b", Verify: "v", Why: "the gate still failed",
		Failing: []string{"`go vet ./...`"}}
	for name, tc := range map[string]struct {
		made  bool
		stack bool
	}{
		"a stack kiln made":       {made: true, stack: true},
		"no stack, so none named": {},
	} {
		t.Run(name, func(t *testing.T) {
			tree := filepath.Join(dir, name, "x")
			if tc.made {
				if err := os.MkdirAll(tree, 0o755); err != nil {
					t.Fatal(err)
				}
			}

			lines := strings.Join(r.sumUp(t.Context(), rec, git.Worktree(tree), ""), "\n")
			if strings.Contains(lines, "the work is on kiln/x") != tc.stack {
				t.Errorf("the summary names the stack %v, want %v:\n%s", !tc.stack, tc.stack, lines)
			}

			if !strings.Contains(lines, "/.build-x' && claude --resume b") &&
				!strings.Contains(lines, "/.build-x && claude --resume b") {
				t.Errorf("the summary names no resume of the build session in its clone:\n%s", lines)
			}
		})
	}
}

// TestBlindTranscripts holds where a split build's session may not read the other session's transcripts. The folder
// follows the environment the session gets, as Claude Code's own does.
func TestBlindTranscripts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  []string
		want string
	}{
		{"the config dir the developer set", []string{"HOME=/home/dev", "CLAUDE_CONFIG_DIR=/x"}, "/x/projects"},
		{"the home folder, with no config dir", []string{"HOME=/home/dev"}, "/home/dev/.claude/projects"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &Repo{trees: "/trees", top: "/repo", common: "/repo/.git", env: tt.env}
			want := filepath.Join(tt.want, projectDir(string(r.clone("x", verifyRole))))
			denyRead, _, deny := r.blinds("x", buildRole)
			if !slices.Contains(denyRead, want) || !slices.Contains(deny, "Read(/"+want+"/**)") {
				t.Errorf("blinds deny %v and %v, want both to hold %s", denyRead, deny, want)
			}
		})
	}
}
