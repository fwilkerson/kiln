package claude

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestHandRun totals spec 1's hand run, the one stream Claude Code really printed that kiln's tests read. It gets the
// numbers docs/later.md gives for it. The stream is in .hand-run/ at the top of the main checkout, which git ignores.
// testdata/hand-run.jsonl, a trimmed copy of it, is read first if it is there.
func TestHandRun(t *testing.T) {
	stream := handRun(t)
	f, err := os.Open(stream)
	if err != nil {
		t.Fatal(err)
	}

	defer f.Close()

	var tl tally
	lines := bufio.NewScanner(f)
	lines.Buffer(nil, 64<<20)
	for lines.Scan() {
		tl.line(lines.Bytes())
	}

	if err := lines.Err(); err != nil {
		t.Fatal(err)
	}

	got := tl.totals()
	refused := 0
	causes := map[string]int{}
	for _, f := range got.Failed {
		if f.Refused {
			refused++
		}

		causes[f.Cause]++
	}

	want := map[string]string{
		"turns":     "201",
		"cost":      "$18.77",
		"context":   "25k → 457k → 457k",
		"7-day":     "13% → 14%",
		"compacted": "0",
		"tools":     "map[Agent:1 Bash:135 Edit:12 Read:51 Write:50]",
		"waiting":   "1m18s",
		"subagents": "6m30s",
		"failed":    "33, 16 refused",
		// A cause that recurs is grouped once, whatever path:line: it opened with.
		"variable":   "4",
		"long":       "7",
		"wide lines": "2",
	}
	k := func(n int) int { return (n + 500) / 1000 }
	have := map[string]string{
		"turns":      fmt.Sprint(*got.Turns),
		"cost":       fmt.Sprintf("$%.2f", *got.CostUSD),
		"context":    fmt.Sprintf("%dk → %dk → %dk", k(got.Context.First), k(got.Context.Peak), k(got.Context.Last)),
		"7-day":      fmt.Sprintf("%.0f%% → %.0f%%", got.SevenDay.First*100, got.SevenDay.Last*100),
		"compacted":  fmt.Sprint(got.Compactions),
		"tools":      fmt.Sprint(got.Tools),
		"waiting":    got.Waiting.Round(time.Second).String(),
		"subagents":  got.Subagents.Round(time.Second).String(),
		"failed":     fmt.Sprintf("%d, %d refused", len(got.Failed), refused),
		"variable":   fmt.Sprint(causes["A variable in this command can't be checked before it runs"]),
		"long":       fmt.Sprint(causes["a sentence in this comment has more than 20 words"]),
		"wide lines": fmt.Sprint(causes["the line is wider than 120 columns"]),
	}
	slowest := got.Slowest[0]
	const ran = "go test ./acceptance/kiln/ -run 'TestAT3$' -v"
	if took := slowest.Took.Round(time.Second); took != 6*time.Second || !strings.HasPrefix(slowest.Input, ran) {
		t.Errorf("the slowest call: %v %s, want 6s go test ./acceptance/kiln/ ...", took, slowest.Input)
	}

	for _, name := range slices.Sorted(maps.Keys(want)) {
		if have[name] != want[name] {
			t.Errorf("%s: %s, want %s", name, have[name], want[name])
		}
	}
}

// handRun finds the hand run's stream, or skips the test on a machine that lacks it.
func handRun(t *testing.T) string {
	if _, err := os.Stat("testdata/hand-run.jsonl"); err == nil {
		return "testdata/hand-run.jsonl"
	}

	common, err := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		t.Skip("no git repository holds the hand run's stream")
	}

	stream := filepath.Join(filepath.Dir(strings.TrimSpace(string(common))), ".hand-run", "stream.jsonl")
	if _, err := os.Stat(stream); err != nil {
		t.Skipf("the hand run's stream is not on this machine: %v", err)
	}

	return stream
}

// TestTally holds the rules the hand run's stream cannot show: it never compacted, resumed, or died mid-run.
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
