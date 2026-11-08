package kiln

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
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
					checks[i].Failed = failedTests(checks[i].Output)
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
	for name, tc := range map[string]struct {
		given   State
		command string
		then    State
		drop    bool
		refused bool
	}{
		"a new spec is queued":                       {command: "approve", then: Queued},
		"approving a queued spec queues it":          {given: Queued, command: "approve", then: Queued},
		"a building spec refuses approval":           {given: Building, command: "approve", then: Building, refused: true},
		"approving a stopped spec builds it again":   {given: Stopped, command: "approve", then: Queued, drop: true},
		"approving a landed spec builds it again":    {given: Landed, command: "approve", then: Queued, drop: true},
		"approving an amended spec drops its branch": {given: Amended, command: "approve", then: Queued, drop: true},
		"a landed spec is accepted":                  {given: Landed, command: "accept", then: Landed},
		"an amended spec is accepted":                {given: Amended, command: "accept", then: Amended},
		"a queued spec refuses acceptance":           {given: Queued, command: "accept", then: Queued, refused: true},
		"a building spec refuses acceptance":         {given: Building, command: "accept", then: Building, refused: true},
		"a stopped spec refuses acceptance":          {given: Stopped, command: "accept", then: Stopped, refused: true},
		"an unknown spec refuses acceptance":         {command: "accept", refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			var b book
			if tc.given != "" {
				b.Specs = []*record{{Name: "x", State: tc.given, Approved: "old", Session: "s", Why: "w"}}
			}

			var drop bool
			var err error
			if tc.command == "approve" {
				drop, err = b.approve("x", "new")
			} else {
				_, err = b.accept("x")
			}

			var then State
			if r := b.find("x"); r != nil {
				then = r.State
			}

			if then != tc.then || drop != tc.drop || errors.Is(err, errRefused) != tc.refused {
				t.Errorf("got %q, drop %v, %v; want %q, drop %v, refused %v", then, drop, err, tc.then, tc.drop,
					tc.refused)
			}

			r := b.find("x")
			if tc.command == "approve" && !tc.refused && (r.Approved != "new" || r.Session != "") {
				t.Errorf("a new approval keeps %+v of the old", r)
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
