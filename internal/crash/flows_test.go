package crash_test

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/fwilkerson/kiln/internal/scenariotest"
	"golang.org/x/sys/unix"
)

func TestMain(m *testing.M) { scenariotest.Main(m, "crash") }

// TestKilledAtEachStep kills kiln at each step of each of its main flows in turn. After each kill, the developer runs
// the command kiln status names next, until the flow reaches its end. Between two commands, and at the end, kiln's
// records must agree with git. They hold nothing a dead kiln left behind.
//
// A snapshot stands in for each SIGKILL, so one run of a flow serves each of its steps. At a step, before its change,
// the hook clones the scratch root. It holds the repository, kiln's records under .git/kiln, and HOME with kiln's
// worktrees. The test then restores the clone to the same path, and runs the recovery there. The clone holds what
// the kill would leave:
//
//   - The book names the snapshotting kiln's PID, which has exited by the time the recovery runs, as a killed kiln's.
//   - A lock is no state of the file, so the clone holds none, as a dead kiln holds none.
//   - Each step comes before its write, and a SIGKILL there ends kiln before the write, so no file is half-written.
//   - Every absolute path, in git's worktree links and in kiln's cache, holds, since the clone goes back where it was.
//   - No program kiln started runs at a step: a session's tree is gone before its end step, and git starts after.
//
// A root's recoveries run one at a time, since each restores to the root's path. So each flow runs in slots, each
// with a root of its own, and each slot takes every slots-th step. SIGINT takes kiln's graceful path. It differs from
// a kill only where kiln reads its context's end: as a session starts and ends. So a real SIGINT kills kiln at each
// session step.
func TestKilledAtEachStep(t *testing.T) {
	for _, f := range flows {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()
			// A run that no kill cuts short labels the flow's steps.
			taken := f.killed(t, kill{flow: f.name})
			for slot := 1; slot <= slots; slot++ {
				t.Run(fmt.Sprintf("KILL/%d+%d", slot, slots), func(t *testing.T) {
					t.Parallel()
					f.fromSnapshots(t, slot)
				})
			}

			for i, what := range taken {
				if strings.HasPrefix(what, "session ") || strings.HasPrefix(what, "attached ") {
					t.Run(fmt.Sprintf("INT/%d", i+1), func(t *testing.T) {
						t.Parallel()
						f.killed(t, kill{f.name, "INT", strconv.Itoa(i + 1)})
					})
				}
			}

			if at := slices.Index(taken, f.recovering); at >= 0 {
				t.Run("recovering", func(t *testing.T) {
					t.Parallel()
					f.killRecovering(t, at+1)
				})
			}
		})
	}
}

// slots is how many scratch roots each flow's snapshots share out.
const slots = 6

// defects are the kills known to leave kiln wrong, each with what goes wrong. A row's run fails once its defect is
// fixed, and then the row goes.
var defects = map[kill]string{
	{"fix", "KILL", "14,17"}: lostDropped,
	{"fix", "KILL", "14,18"}: lostDropped,
	{"fix", "KILL", "14,19"}: lostDropped,
}

// The defects that several kills share.
const (
	lostDropped = "The cut-short fix's record went first, so its outcome, worktree and branch are left to no record."
)

// kill is one run of a flow, killed by signal at the steps at names, as KILN_CRASH_AT names them. A run with no
// signal is killed at no step.
type kill struct {
	flow   string
	signal string
	at     string
}

// flow is one of kiln's main flows, from the developer's first command to its end.
type flow struct {
	// name names the flow's Given, testdata/<name>.txtar. Its _given/steps.json holds each role's steps, which every
	// call of that role takes.
	name string
	// subject is the spec or monitor the flow builds, by its name.
	subject Spec
	// recovering labels the step whose kill leaves work for the next command to recover. Each step of that recovery
	// gets a kill of its own too. "" kills no recovering kiln.
	recovering string
	// next is the kiln command the developer runs next in s, or nil once the flow reached its end.
	next func(s *scratch) []string
	// landed match the subject of each commit the flow lands on main, in order.
	landed []string
	// outcome is how the ledger's last build of the flow ended.
	outcome string
	// told is what a build session's prompt holds by the flow's end, or "".
	told string
}

var flows = []flow{
	{
		name:    "lands",
		subject: "x",
		landed:  []string{`^feat\(x\): land the approved spec$`},
		outcome: "landed",
		next: func(s *scratch) []string {
			switch s.shown(s.subject) {
			case "":
				return []string{"queue", "x"}
			case "waiting for UAT":
				return nil
			}

			return []string{"fire"}
		},
	},
	{
		name:    "accept",
		subject: "y",
		landed:  []string{`^feat\(y\): land the approved spec$`, `\by\b`},
		outcome: "accepted",
		next: func(s *scratch) []string {
			shown := s.shown(s.subject)
			switch {
			case shown == "" && s.retired():
				return nil
			case shown == "":
				return []string{"queue", "y"}
			case strings.HasPrefix(shown, "waiting on its branch"), strings.HasPrefix(shown, "stopped"):
				return []string{"accept", "y"}
			}

			return []string{"fire"}
		},
	},
	{
		name:       "fix",
		subject:    "bump",
		recovering: "session start",
		landed:     []string{`\bbump\b`},
		outcome:    "landed",
		next: func(s *scratch) []string {
			// A run that recovers a landing goes on to check again, and finds the monitor healthy.
			if shown := s.shown(s.subject); strings.HasPrefix(shown, "landed") || strings.HasPrefix(shown, "healthy") {
				return nil
			}

			return []string{"monitor", "run", "bump"}
		},
	},
	{
		name:    "tend",
		subject: "z",
		landed:  []string{`^feat\(z\): land the approved spec$`},
		outcome: "landed",
		told:    "The gate wanted .tmp/tended.",
		next: func(s *scratch) []string {
			switch s.shown(s.subject) {
			case "":
				return []string{"queue", "z"}
			case "stopped":
				return []string{"tend", "z"}
			case "waiting for UAT":
				return nil
			}

			return []string{"fire"}
		},
	},
}

// killed runs the flow f to its end in a scratch repository of its own. kiln is killed as k says. killed answers
// the label of each step the run took.
func (f flow) killed(t *testing.T, k kill) []string {
	s := newScratch(t, f)
	steps := filepath.Join(t.TempDir(), "steps")
	env := append(slices.Clone(s.env), "KILN_CRASH_LOG="+steps, "KILN_CRASH_AT="+k.at, "KILN_CRASH_SIGNAL="+k.signal)

	problems := f.drive(s, env)
	taken := labels(steps)
	s.judge(k, problems, taken)
	return taken
}

// fromSnapshots runs the flow f once, with a snapshot at each slots-th step from slot. Then it restores each snapshot
// in turn, and drives the flow from there to its end.
func (f flow) fromSnapshots(t *testing.T, slot int) {
	s := newScratch(t, f)
	steps, snapshots, root := filepath.Join(t.TempDir(), "steps"), t.TempDir(), filepath.Dir(s.work)
	env := append(slices.Clone(s.env), "KILN_CRASH_LOG="+steps, "KILN_CRASH_SNAPSHOTS="+snapshots,
		"KILN_CRASH_ROOT="+root, fmt.Sprintf("KILN_CRASH_AT=%d+%d", slot, slots))
	s.judge(kill{flow: f.name}, f.drive(s, env), labels(steps))
	taken := labels(steps)
	for at := slot; at <= len(taken); at += slots {
		s.said.Reset()
		if err := restore(filepath.Join(snapshots, strconv.Itoa(at)), root); err != nil {
			t.Fatal(err)
		}

		s.judge(kill{f.name, "KILL", strconv.Itoa(at)}, f.drive(s, s.env), taken)
	}
}

// killRecovering kills kiln at the step at of the flow f. Then it kills the next command at each step of its recovery,
// up to its next monitor line.
func (f flow) killRecovering(t *testing.T, at int) {
	taken := f.killed(t, kill{f.name, "KILL", strconv.Itoa(at)})
	for i := at; i < len(taken) && taken[i] != "ledger.jsonl monitor"; i++ {
		t.Run(strconv.Itoa(i+1), func(t *testing.T) {
			t.Parallel()
			f.killed(t, kill{f.name, "KILL", fmt.Sprintf("%d,%d", at, i+1)})
		})
	}
}

// restore puts the snapshot back at root, in place of what root holds.
func restore(snapshot, root string) error {
	if err := os.RemoveAll(root); err != nil {
		return err
	}

	return unix.Clonefile(snapshot, root, unix.CLONE_NOFOLLOW)
}

// labels reads the label of each step the log at path holds, in order.
func labels(path string) []string {
	log, _ := os.ReadFile(path)
	var taken []string
	for line := range strings.Lines(string(log)) {
		_, rest, _ := strings.Cut(strings.TrimSpace(line), " ")
		_, what, _ := strings.Cut(rest, " ")
		taken = append(taken, what)
	}

	return taken
}

// drive runs, with env, the commands the developer runs next in s, until the flow f reaches its end. It answers each
// rule s broke between two commands, and at the end.
func (f flow) drive(s *scratch, env []string) []string {
	problems := s.between()
	for range 12 {
		next := f.next(s)
		if next == nil {
			break
		}

		s.run(env, "kiln", next...)
		problems = append(problems, s.between()...)
	}

	return append(problems, s.problems(f)...)
}

// judge fails the test when the run k found problems, unless defects holds k. It fails it too when k recovers, though
// defects holds it. taken labels the run's steps.
func (s *scratch) judge(k kill, problems, taken []string) {
	t := s.t
	t.Helper()
	why, known := defects[k]
	switch {
	case known && len(problems) == 0:
		t.Errorf("%+v now recovers: delete its row from defects, %q", k, why)
	case known:
		t.Logf("%+v: a known defect: %s", k, why)
	case len(problems) > 0:
		t.Errorf("%+v, %s:\n%s\n\n%s", k, killedAt(k, taken), strings.Join(problems, "\n"), s.said.String())
	}
}

// killedAt labels the steps at which k killed kiln.
func killedAt(k kill, taken []string) string {
	var said []string
	for item := range strings.SplitSeq(k.at, ",") {
		if n, _ := strconv.Atoi(item); n > 0 && n <= len(taken) {
			said = append(said, fmt.Sprintf("step %d, %s", n, taken[n-1]))
		}
	}

	return "killed at " + cmp.Or(strings.Join(said, ", then "), "no step")
}

// between lists each rule s breaks while no kiln runs. No build is at work. No worktree or branch of the flow's
// subject is left once the book holds no record of it.
func (s *scratch) between() []string {
	var found []string
	if status := s.read("kiln", "status"); atWork.MatchString(status) {
		found = append(found, "kiln status shows a build at work, though no kiln runs:\n"+status)
	}

	if s.held() {
		return found
	}

	for _, tree := range s.trees() {
		if strings.TrimPrefix(strings.TrimPrefix(filepath.Base(tree), ".audit-"), ".gate-") == string(s.subject) {
			found = append(found, "the book holds no record of "+string(s.subject)+", yet its worktree is left: "+tree)
		}
	}

	if branches := s.read("git", "branch", "--list", "kiln/"+string(s.subject)+"*"); branches != "" {
		found = append(found, "the book holds no record of "+string(s.subject)+", yet its branch is left: "+branches)
	}

	return found
}

// held is whether kiln's book holds a record of the flow's subject.
func (s *scratch) held() bool {
	raw, _ := os.ReadFile(filepath.Join(s.work, ".git", "kiln", "state.json"))
	var b struct {
		Specs []struct {
			Name Spec `json:"name"`
		} `json:"specs"`
	}

	_ = json.Unmarshal(raw, &b)
	return slices.ContainsFunc(b.Specs, func(r struct {
		Name Spec `json:"name"`
	}) bool {
		return r.Name == s.subject
	})
}

// trees lists the path of each worktree git holds.
func (s *scratch) trees() []string {
	var found []string
	for line := range strings.Lines(s.read("git", "worktree", "list", "--porcelain")) {
		if tree, ok := strings.CutPrefix(strings.TrimSpace(line), "worktree "); ok {
			found = append(found, tree)
		}
	}

	return found
}

// Spec is a spec's or a monitor's name.
type Spec string

// atWork matches a line of kiln status that shows a spec's build or a monitor's fix at work.
var atWork = regexp.MustCompile(`(?m)^\S+: (fix )?building\b`)

// scratch is a flow's scratch repository, and what each command run there printed.
type scratch struct {
	t       *testing.T
	work    string
	env     []string
	subject Spec
	said    strings.Builder
}

// newScratch makes the scratch repository of the flow f. Its scripted claude answers each call of a role with the
// role's steps.
func newScratch(t *testing.T, f flow) *scratch {
	work, env := scenariotest.Repo(t, filepath.Join("testdata", f.name+".txtar"))
	raw, err := os.ReadFile(filepath.Join(work, "_given", "steps.json"))
	if err != nil {
		t.Fatal(err)
	}

	var steps map[string][]scenariotest.ClaudeStep
	if err := json.Unmarshal(raw, &steps); err != nil {
		t.Fatal(err)
	}

	// Kills add sessions and resumes, so every attempt and call up to a bound takes the role's steps.
	script := scenariotest.ClaudeScript{}
	for role, list := range steps {
		script[role] = map[int]map[int][]scenariotest.ClaudeStep{}
		for attempt := 1; attempt <= 9; attempt++ {
			script[role][attempt] = map[int][]scenariotest.ClaudeStep{}
			for call := 1; call <= 9; call++ {
				script[role][attempt][call] = list
			}
		}
	}

	raw, err = json.Marshal(script)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(work, scenariotest.ClaudeScriptFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	return &scratch{t: t, work: work, env: env, subject: f.subject}
}

// run runs program with args in the scratch repository, with env, and answers what it printed.
func (s *scratch) run(env []string, program string, args ...string) string {
	cmd := exec.CommandContext(s.t.Context(), program, args...)
	cmd.Dir, cmd.Env = s.work, env
	out, err := cmd.CombinedOutput()
	fmt.Fprintf(&s.said, "$ %s %s\n%s", program, strings.Join(args, " "), out)
	if err != nil {
		fmt.Fprintf(&s.said, "[%v]\n", err)
	}

	return string(out)
}

// read runs program with args, which change nothing, and answers what it printed.
func (s *scratch) read(program string, args ...string) string {
	cmd := exec.CommandContext(s.t.Context(), program, args...)
	cmd.Dir, cmd.Env = s.work, s.env
	out, _ := cmd.CombinedOutput()
	return string(out)
}

// shown is where kiln status shows the spec or monitor name stands, or "" when it shows none.
func (s *scratch) shown(name Spec) string {
	shown := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(string(name)) + `: (.*)$`)
	m := shown.FindStringSubmatch(s.read("kiln", "status"))
	if m == nil {
		return ""
	}

	return m[1]
}

// retired is whether main no longer holds the spec of the flow's subject.
func (s *scratch) retired() bool {
	spec := "main:.kiln/specs/" + string(s.subject) + ".md"
	return exec.Command("git", "-C", s.work, "cat-file", "-e", spec).Run() != nil
}

// problems lists each way the scratch repository breaks a rule that holds at the end of f, whatever killed kiln.
func (s *scratch) problems(f flow) []string {
	var found []string
	if next := f.next(s); next != nil {
		found = append(found, "the flow stopped short: kiln status shows "+s.read("kiln", "status"))
	}

	found = append(found, s.ledgerProblems(f)...)
	landed := strings.Split(strings.TrimSpace(s.read("git", "log", "--reverse", "--format=%s", "main")), "\n")[1:]
	matched := len(landed) == len(f.landed)
	for i := range min(len(landed), len(f.landed)) {
		matched = matched && regexp.MustCompile(f.landed[i]).MatchString(landed[i])
	}

	if !matched {
		found = append(found, fmt.Sprintf("main holds the landings %q, not one of each of %q", landed, f.landed))
	}

	// The main checkout stays, with the worktrees that no one build owns.
	kept := []string{filepath.Base(s.work), ".main", ".gauges", ".monitor-check"}
	for _, tree := range s.trees() {
		if !slices.Contains(kept, filepath.Base(tree)) {
			found = append(found, "a worktree is left behind: "+tree)
		}
	}

	if branches := s.read("git", "branch", "--list", "kiln/*"); branches != "" {
		found = append(found, "a branch is left behind: "+branches)
	}

	if strings.Contains(s.read("git", "ls-tree", "-r", "--name-only", "main"), "audit-stray.txt") {
		found = append(found, "main holds the file the audit wrote")
	}

	book, _ := os.ReadFile(filepath.Join(s.work, ".git", "kiln", "state.json"))
	if strings.Contains(string(book), `"owed"`) {
		found = append(found, "the book still owes a session a message: "+string(book))
	}

	if f.told != "" && !s.told(f.told) {
		found = append(found, "no build session was told "+f.told)
	}

	return found
}

// ledgerProblems lists each run line of the ledger with no end, and each build with no outcome. The last build
// must end as f's does.
func (s *scratch) ledgerProblems(f flow) []string {
	var found []string
	for line := range strings.Lines(s.read("kiln", "ledger", "--kind", "run")) {
		var run struct {
			Session string  `json:"session"`
			End     *string `json:"end"`
		}

		if err := json.Unmarshal([]byte(line), &run); err != nil || run.End == nil {
			found = append(found, "a run has no end: "+line)
		}
	}

	var outcome *string
	for line := range strings.Lines(s.read("kiln", "ledger", "--kind", "build")) {
		var build struct {
			Outcome *string `json:"outcome"`
		}

		if err := json.Unmarshal([]byte(line), &build); err != nil || build.Outcome == nil {
			found = append(found, "a build has no outcome: "+line)
			continue
		}

		outcome = build.Outcome
	}

	if outcome == nil || *outcome != f.outcome {
		found = append(found, fmt.Sprintf("the last build did not end %s", f.outcome))
	}

	return found
}

// told is whether a build session's prompt holds text.
func (s *scratch) told(text string) bool {
	prompts, _ := filepath.Glob(filepath.Join(s.work, scenariotest.ClaudeCallsDir, "build", "*", "*",
		scenariotest.ClaudePromptFile))
	return slices.ContainsFunc(prompts, func(p string) bool {
		raw, _ := os.ReadFile(p)
		return strings.Contains(string(raw), text)
	})
}
