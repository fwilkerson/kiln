package claude

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTally holds the rules a stream shows: compaction, resumes, and a session that died mid-run.
func TestTally(t *testing.T) {
	for name, tc := range map[string]struct {
		lines []string
		want  string
	}{
		"a compaction is counted": {
			lines: []string{`{"type":"system","subtype":"compact_boundary"}`, `{"type":"system","subtype":"compact_boundary"}`},
			want:  "turns unknown, cost unknown, 2 compactions",
		},
		"a resume costs what it used since the last result, not the session's total": {
			lines: []string{`{"type":"result","num_turns":1,"total_cost_usd":0.00311,` +
				`"model_usage_since_last_result":{"claude-haiku-5-5":{"costUSD":0.00024}}}`},
			want: "1 turns, $0.00024, 0 compactions",
		},
		"a run that died before its result has no turns or cost": {
			lines: []string{`{"type":"assistant","message":{"id":"m","content":[],"usage":{"input_tokens":5}}}`},
			want:  "turns unknown, cost unknown, 0 compactions",
		},
	} {
		t.Run(name, func(t *testing.T) {
			var tl tally
			for _, line := range tc.lines {
				tl.line([]byte(line))
			}

			got := tl.totals()
			turns, cost := "turns unknown", "cost unknown"
			if got.Turns != nil {
				turns = fmt.Sprintf("%d turns", *got.Turns)
			}

			if got.CostUSD != nil {
				cost = fmt.Sprintf("$%.5f", *got.CostUSD)
			}

			if have := fmt.Sprintf("%s, %s, %d compactions", turns, cost, got.Compactions); have != tc.want {
				t.Errorf("%s, want %s", have, tc.want)
			}
		})
	}
}

// TestCause holds how an error's output names its cause.
func TestCause(t *testing.T) {
	for output, want := range map[string]string{
		"Exit code 1\ninternal/kiln/gate.go:123: a sentence in this comment has more than 20 words\nexit status 1": "" +
			"a sentence in this comment has more than 20 words",
		"Exit code 1\n# example.com/s\n./s.go:3:14: syntax error: unexpected newline": "syntax error: unexpected newline",
		"Exit code 2\nexit status 1\n":                          "",
		"<tool_use_error>File does not exist.</tool_use_error>": "<tool_use_error>File does not exist.</tool_use_error>",
	} {
		if got := cause(output); got != want {
			t.Errorf("cause(%q) = %q, want %q", output, got, want)
		}
	}
}

// TestRecountCalls holds how a run's stretch of the stream lists its calls, and the text the model said, in order.
// Each call has its time, and says whether it failed, was refused, or is still open.
func TestRecountCalls(t *testing.T) {
	use := func(id, at, tool, input string) string {
		return `{"type":"assistant","timestamp":"2026-11-08T10:00:` + at + `Z","parent_tool_use_id":null,` +
			`"message":{"id":"m` + id + `","content":[{"type":"tool_use","id":"` + id + `","name":"` + tool +
			`","input":` + input + `}],"usage":{}}}` + "\n"
	}

	result := func(id, at, content string, isError bool) string {
		return fmt.Sprintf(`{"type":"user","timestamp":"2026-11-08T10:00:%sZ","parent_tool_use_id":null,`+
			`"message":{"content":[{"type":"tool_result","tool_use_id":"%s","content":%q,"is_error":%v}]}}`+"\n", at,
			id, content, isError)
	}

	first := use("t1", "00", "Read", `{"file_path":"x.go"}`) + result("t1", "01", "package x", false)
	second := `{"type":"assistant","timestamp":"2026-11-08T10:00:02Z","parent_tool_use_id":null,"message":` +
		`{"id":"m2","content":[{"type":"text","text":"Now the test.\nMore."}],"usage":{}}}` + "\n" +
		use("t2", "03", "Write", `{"file_path":"specs/x.md"}`) +
		`{"type":"system","subtype":"permission_denied","tool_use_id":"t2","tool_name":"Write","message":"no"}` +
		"\n" + result("t2", "03", "no", true) +
		use("t3", "04", "Bash", `{"command":"go test ./...\nmore"}`) +
		result("t3", "07", "Exit code 1\n--- FAIL: TestX", true) +
		use("t4", "08", "Bash", `{"command":"sleep 9"}`)

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "s"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "s", streamFile), []byte(first+second), 0o600); err != nil {
		t.Fatal(err)
	}

	c := New(Config{Sessions: dir})
	for name, tc := range map[string]struct {
		from, to int64
		want     []string
	}{
		"the first run's bytes": {0, int64(len(first)), []string{"Read x.go 1s"}},
		"the second run, still running, to the stream's end": {int64(len(first)), -1, []string{
			"said Now the test. 0s",
			"Write specs/x.md 0s refused: no",
			"Bash go test ./... 3s failed: --- FAIL: TestX",
			"Bash sleep 9 0s open",
		}},
	} {
		t.Run(name, func(t *testing.T) {
			counted, err := c.Recount("s", tc.from, tc.to)
			if err != nil {
				t.Fatal(err)
			}

			var got []string
			for _, a := range counted.Actions {
				said := fmt.Sprintf("%s %s %s", cmp.Or(a.Tool, "said"), a.Input, a.Took)
				switch {
				case a.Refused:
					said += " refused: " + a.Cause
				case a.Failed:
					said += " failed: " + a.Cause
				case a.Open:
					said += " open"
				}

				got = append(got, said)
			}

			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("the calls are\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}
