package kiln

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
)

func TestGuard(t *testing.T) {
	const place = "/Users/dev/Library/Caches/kiln/repo/x"
	for name, tc := range map[string]struct {
		tool, cwd, file string
		refused         bool
	}{
		"a build writes its code":                    {tool: "Write", file: place + "/x.go"},
		"a build writes its spec's amendments":       {tool: "Write", file: place + "/specs/x.amend.md"},
		"a build may not edit its spec":              {tool: "Edit", file: place + "/specs/x.md", refused: true},
		"nor another spec's amendments":              {tool: "Write", file: place + "/specs/y.amend.md", refused: true},
		"nor anything deeper under specs":            {tool: "Write", file: place + "/specs/x/x.amend.md", refused: true},
		"nor a spec spelled in other letters":        {tool: "Edit", file: place + "/SPECS/x.md", refused: true},
		"nor a spec by a path relative to its cwd":   {tool: "Edit", cwd: place + "/specs", file: "x.md", refused: true},
		"nor a path that climbs out of its worktree": {tool: "Write", file: place + "/../y/x.go", refused: true},
		"nor the developer's Claude Code settings":   {tool: "Write", file: place + "/.claude/settings.json", refused: true},
		"nor a git directory":                        {tool: "Write", file: place + "/sub/.git/config", refused: true},
		"nor a notebook under specs":                 {tool: "NotebookEdit", file: place + "/specs/n.ipynb", refused: true},
		"a write that names no file is refused":      {tool: "Write", refused: true},
		"a path through /private is the same path":   {tool: "Write", file: "/private/tmp/w/specs/x.md", refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			input := map[string]string{"file_path": tc.file}
			if tc.tool == "NotebookEdit" {
				input = map[string]string{"notebook_path": tc.file}
			}

			call := map[string]any{"tool_name": tc.tool, "cwd": tc.cwd, "tool_input": input}

			in, _ := json.Marshal(call)
			var out bytes.Buffer
			worktree := place
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
		refused   []string
		passed    bool
		atMain    []string
	}{
		"every command exits 0": {
			tip: []check{{Command: "go test ./...", Exit: 0}}, passed: true,
		},
		"a command fails": {
			tip:  []check{{Command: "go vet ./...", Exit: 1, Output: "vet: x"}},
			main: []check{{Command: "go vet ./...", Exit: 0}},
		},
		"a test fails that passes at main": {
			tip:  []check{{Command: "go test ./...", Exit: 1, Output: other}},
			main: []check{{Command: "go test ./...", Exit: 0}},
		},
		"a test fails that fails at main too": {
			tip:    []check{{Command: "go test ./...", Exit: 1, Output: failing}},
			main:   []check{{Command: "go test ./...", Exit: 1, Output: failing}},
			passed: true, atMain: []string{"TestFlaky (example.com/s)"},
		},
		"a test fails at main, and another fails only at the tip": {
			tip:    []check{{Command: "go test ./...", Exit: 1, Output: failing + other}},
			main:   []check{{Command: "go test ./...", Exit: 1, Output: failing}},
			atMain: []string{"TestFlaky (example.com/s)"},
		},
		"a test that fails at main is named though the tip passes": {
			tip:    []check{{Command: "go test ./...", Exit: 0}},
			main:   []check{{Command: "go test ./...", Exit: 1, Output: failing}},
			passed: true, atMain: []string{"TestFlaky (example.com/s)"},
		},
		"a build that fails at main too still fails": {
			tip:  []check{{Command: "go test ./...", Exit: 1, Output: broken}},
			main: []check{{Command: "go test ./...", Exit: 1, Output: broken}},
		},
		"a package that hangs, while a test fails at main too": {
			tip:    []check{{Command: "go test ./...", Exit: 1, Output: failing + hung}},
			main:   []check{{Command: "go test ./...", Exit: 1, Output: failing}},
			atMain: []string{"TestFlaky (example.com/s)"},
		},
		"a test that fails at main under another command": {
			tip:    []check{{Command: "go test -race ./...", Exit: 1, Output: failing}},
			main:   []check{{Command: "go test ./...", Exit: 1, Output: failing}},
			atMain: []string{"TestFlaky (example.com/s)"},
		},
		"a guarded path changed": {
			tip:     []check{{Command: "go test ./...", Exit: 0}},
			refused: []string{"specs/x.md is under specs/"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			for _, checks := range [][]check{tc.tip, tc.main} {
				for i := range checks {
					checks[i].Failed, checks[i].Broken = failedTests(checks[i].Output)
				}
			}

			v := judge(tc.tip, tc.main, tc.refused)
			if v.passed != tc.passed || !slices.Equal(v.atMain, tc.atMain) || v.passed != (v.report == "") {
				t.Errorf("passed %v, at main %q, report %q; want passed %v, at main %q", v.passed, v.atMain, v.report,
					tc.passed, tc.atMain)
			}
		})
	}
}

func TestSpecStates(t *testing.T) {
	// given is a spec's state, with " landing" for one landing accepted amendments, or "" for no record. then is "" for
	// a refusal, and "no record" once the record is gone. drop is a branch to drop, and dropped a build ended unended.
	type row struct {
		given, command, then string
		changed, drop        bool
		dropped              bool
	}

	const none = "no record"
	rows := map[string]row{
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

	for name, tc := range rows {
		t.Run(name, func(t *testing.T) {
			var b book
			if tc.given != "" {
				given, landing := strings.CutSuffix(tc.given, " landing")
				b.Specs = []*record{{Name: "x", State: state(given), Accepted: landing, Approved: "old", Session: "s",
					Build: "b", Rounds: 2, Audited: true, Why: "w", FailingAtMain: []string{"TestF"}}}
			}

			changed := func(since string) (bool, error) {
				if since != "old" {
					t.Errorf("changed since %q, want since the queue", since)
				}

				return tc.changed, nil
			}

			var drop bool
			var dropped string
			var err error
			switch tc.command {
			case "queue":
				drop, dropped, err = b.queue("x", "new", changed)
			case "accept":
				_, err = b.accept("x")
			case "reject":
				_, err = b.reject("x")
			}

			then := none
			if r := b.find("x"); r != nil {
				then = string(r.State)
				if r.Accepted {
					then += " landing"
				}
			}

			if tc.then == "" {
				if !errors.Is(err, errRefused) || then != cmp.Or(tc.given, none) {
					t.Errorf("got %q, %v; want it refused, and %q as it was", then, err, tc.given)
				}

				return
			}

			if err != nil || then != tc.then || drop != tc.drop || (dropped == "b") != tc.dropped {
				t.Errorf("got %q, %v, drop %v, dropped %q; want %q, drop %v, the build dropped %v", then, err, drop,
					dropped, tc.then, tc.drop, tc.dropped)
			}

			r := b.find("x")
			switch {
			case tc.command == "queue" && (r.Approved != "new" || r.Session != "" || r.Build == "b" || r.Build == ""):
				t.Errorf("a new queue keeps %+v of the old", r)
			case tc.then == "queued landing" && (r.Build != "b" || r.Session != "s" || !r.Audited || r.Rounds != 0 ||
				r.Why != "" || r.FailingAtMain != nil):
				t.Errorf("a landing accepted again keeps %+v", r)
			}
		})
	}
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
	var took []string
	for {
		r, found := b.next("z", 1)
		if !found {
			break
		}

		took = append(took, r.Name)
	}

	if !slices.Equal(took, []string{"x", "z"}) {
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
