// Package ledger holds fake builds shaped like spec 2's ledger, so each TUI prototype draws the same data.
//
//	for _, b := range ledger.Builds() {
//		fmt.Println(b.Spec, b.Outcome, b.Cost)
//	}
package ledger

import "time"

// Build is one build as kiln stats would total it.
type Build struct {
	Started time.Time
	Spec    string
	Outcome string
	Size    int
	Time    time.Duration
	Waiting time.Duration
	Gates   time.Duration
	Cost    float64
	Turns   int
	Roles   []Role
	Causes  []Cause
}

// Failed counts the build's failed calls over all causes.
func (b Build) Failed() int {
	n := 0
	for _, c := range b.Causes {
		n += c.Count
	}

	return n
}

// Role is one role's share of a build.
type Role struct {
	Name     string
	Runs     int
	Turns    int
	Cost     float64
	Time     time.Duration
	Context  []int
	ToolUses map[string]int
}

// Cause is one reason calls failed, with the first line of one such call's input.
type Cause struct {
	Count int
	Text  string
	Input string
}

// Builds answers eight builds, oldest first.
func Builds() []Build {
	day := time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)
	style := Cause{11, "a sentence in this comment has more than 20 words", "Write internal/kiln/ledger.go"}
	vars := Cause{4, "refused: A variable in this command can't be checked before it runs", "cd $dir && go test"}
	ops := Cause{4, "refused: This Bash command contains multiple operations", "go vet ./... && go test ./..."}
	grep := Cause{2, "grep matched nothing", "grep -rn compact_boundary internal"}
	gate := Cause{1, "--- FAIL: TestAT3 (0.41s)", "go test ./acceptance/kiln/ -run 'TestAT3$'"}
	build := func(turns int, cost float64, minutes int, tools int) Role {
		return Role{Name: "build", Runs: 1, Turns: turns, Cost: cost, Time: time.Duration(minutes) * time.Minute,
			Context:  []int{25_000, 180_000 + 1_000*turns, 170_000 + 900*turns},
			ToolUses: map[string]int{"Bash": tools, "Read": tools / 2, "Write": tools / 3, "Edit": tools / 6}}
	}

	audit := func(cost float64) Role {
		return Role{Name: "audit", Runs: 1, Turns: 14, Cost: cost, Time: 6 * time.Minute,
			Context: []int{18_000, 96_000, 96_000}, ToolUses: map[string]int{"Read": 40, "Grep": 12}}
	}

	return []Build{
		{day.Add(-7 * 24 * time.Hour), "status-line", "accepted", 4, 18 * time.Minute, 40 * time.Second, 50 * time.Second,
			6.10, 88, []Role{build(74, 5.20, 15, 60), audit(0.90)}, []Cause{grep}},
		{day.Add(-6 * 24 * time.Hour), "usage-limits", "accepted", 6, 31 * time.Minute, 70 * time.Second, 95 * time.Second,
			11.40, 150, []Role{build(136, 10.30, 26, 92), audit(1.10)}, []Cause{style, grep}},
		{day.Add(-5 * 24 * time.Hour), "amendments", "stopped", 7, 52 * time.Minute, 3 * time.Minute, 4 * time.Minute,
			22.80, 260, []Role{build(246, 21.60, 46, 150), audit(1.20)}, []Cause{style, vars, ops, gate}},
		{day.Add(-4 * 24 * time.Hour), "amendments", "dropped", 7, 12 * time.Minute, 30 * time.Second, 0,
			4.90, 61, []Role{build(61, 4.90, 12, 40)}, []Cause{ops}},
		{day.Add(-4*24*time.Hour + 2*time.Hour), "amendments", "landed", 7, 38 * time.Minute, 90 * time.Second,
			2 * time.Minute, 15.20, 190, []Role{build(176, 14.10, 32, 120), audit(1.10)}, []Cause{style, grep}},
		{day.Add(-3 * 24 * time.Hour), "gate-flakes", "landed", 3, 14 * time.Minute, 30 * time.Second, 40 * time.Second,
			4.30, 70, []Role{build(56, 3.40, 8, 44), audit(0.90)}, nil},
		{day.Add(-2 * 24 * time.Hour), "notifications", "landed", 5, 26 * time.Minute, 60 * time.Second,
			80 * time.Second, 9.70, 121, []Role{build(107, 8.60, 20, 80), audit(1.10)}, []Cause{vars, grep}},
		{day.Add(-1 * 24 * time.Hour), "build-stats", "waiting on its branch", 6, 44 * time.Minute, 2 * time.Minute,
			3 * time.Minute, 19.90, 230, []Role{build(216, 18.70, 38, 140), audit(1.20)}, []Cause{style, ops, gate}},
	}
}
