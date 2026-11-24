package crash_test

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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

// TestKilledAtEachStep holds that a kiln killed at any step of a main flow recovers at the developer's next command.
//
// A kill leaves a state, and the next kiln reads the state alone, whichever step left it. So the test stops kiln at
// each step, and reads what a kill there would leave, as left says. A rule that holds whatever command comes next
// is checked there, at every step. A leftover that no step showed before gets a drive. The test restores it, and
// runs the commands the developer runs next, until the flow reaches its end. Between two commands, and at the end,
// kiln's records must agree with git. They hold nothing a dead kiln left behind.
//
// A snapshot stands in for the SIGKILL, so one run of a flow serves each of its steps. At the step, before its
// change, the test clones the scratch root. It holds the repository, kiln's records under .git/kiln, and HOME with
// kiln's worktrees. The clone holds what the kill would leave:
//
//   - The book names the PID of the kiln at the step, which has exited once the test restores the clone.
//   - A lock is no state of the file, so the clone holds none, as a dead kiln holds none.
//   - Each step comes before its write, and a SIGKILL there ends kiln before the write, so no file is half-written.
//   - Every absolute path, in git's worktree links and in kiln's cache, holds, since the clone goes back where it was.
//   - No program kiln started runs at a step, but a second session of build-and-verify, as holds says.
//
// One flow gets a real SIGKILL at each kind of step it takes. It recovers from each as from a clone.
//
// A recovery is a run too. The test stops it at each step, and holds each rule there. It drives from each leftover
// whose record or lost runs no run showed. A flow marked redone gets a drive from each new leftover of its recovery.
//
// The flows share the leftovers they showed, so the test goes in rounds. In a round, each flow claims each leftover
// new to it, with a clone. settle then gives each leftover to the first claim of it, in the flows' order. So which
// flow drives a leftover never turns on which flow reached it first.
//
// SIGINT takes kiln's graceful path. It differs from a kill only where kiln reads its context's end: as a session
// starts and ends. So each flow runs a second time, and gets a real SIGINT at each such step, once for each leftover.
// The second run decides by its own steps alone. It shares nothing with the first, so the two run side by side.
//
// Two sessions that run side by side take their steps in an order that differs from run to run. So a run may never
// show a leftover that another run showed. Each check therefore holds for whichever leftovers a run shows, and none
// expects one to come again.
//
// The test keeps a budget of its own, as outlasted says. Past the budget, a run starts no further command, and skips.
// The test then fails once, by a message that names the budget.
func TestKilledAtEachStep(t *testing.T) {
	keepBudget(t)
	proofs := make([]*proof, len(flows))
	for i, f := range flows {
		proofs[i] = &proof{flow: f, scratch: newScratch(t, f)}
		proofs[i].scratch.taken = t.TempDir()
	}

	t.Run("killed", func(t *testing.T) {
		t.Parallel()
		seen := map[string]bool{}
		for round := 0; slices.ContainsFunc(proofs, func(p *proof) bool { return p.due(round) }); round++ {
			t.Run(strconv.Itoa(round), func(t *testing.T) { eachDue(t, proofs, round, seen) })
			settle(seen, proofs)
		}
	})

	t.Run("signaled", func(t *testing.T) {
		t.Parallel()
		signalEach(t)
	})
}

// eachDue runs one round of the test: the part of each flow that is due, all in parallel.
func eachDue(t *testing.T, proofs []*proof, round int, seen map[string]bool) {
	for _, p := range proofs {
		if p.due(round) {
			t.Run(p.flow.name, func(t *testing.T) {
				t.Parallel()
				p.round(t, round, seen)
			})
		}
	}
}

// signalEach runs each flow again, in parallel, with real signals. The first flow gets a SIGKILL at each kind of step
// it takes.
func signalEach(t *testing.T) {
	kills := []string{"state.json", "ledger.jsonl", "git", "session start", "session end"}
	for _, f := range flows {
		sig := &signals{kills: kills, sent: map[string]bool{}}
		kills = nil
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()
			f.signaled(t, sig)
		})
	}
}

// defects are the leftovers known to leave kiln wrong, each with what goes wrong. A key is the flow's name and the
// leftover's class, as a failure quotes them. A row's drive fails once its defect is fixed, and then the row goes.
var defects = map[string]string{}

// flow is one of kiln's main flows, from the developer's first command to its end.
type flow struct {
	// name names the flow's Given, testdata/<name>.txtar, unless given names another. Its _given/steps.json holds
	// each role's steps, which every call of that role takes.
	name, given string
	// subject is the spec or monitor the flow builds, by its name.
	subject Spec
	// redone marks a flow whose work the next kiln drops and starts again, once a kiln died at it. No other run
	// takes that code. So proved drives from each new leftover of the flow's recovery, whichever checkouts it shows.
	redone bool
	// next is the kiln command the developer runs next in s, or nil once the flow reached its end.
	next func(s *scratch) []string
	// landed match the subject of each commit the flow lands on main, in order.
	landed []string
	// outcome is how the ledger's last build of the flow ended.
	outcome string
	// told are patterns, each of which a session's prompt matches by the flow's end.
	told []string
	// once are patterns that no session's prompt matches twice. A message kiln owes twice would.
	once []string
}

// mainConflicts matches the start of the round kiln owes a session whose work conflicts with main.
const mainConflicts = `Main moved, and your work conflicts with it`

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
		name:    "fix",
		subject: "bump",
		redone:  true,
		landed:  []string{`\bbump\b`},
		outcome: "landed",
		next: func(s *scratch) []string {
			// A run that recovers a landing goes on to check again, and finds the monitor healthy.
			if shown := s.shown(s.subject); strings.HasPrefix(shown, "landed") || strings.HasPrefix(shown, "healthy") {
				return nil
			}

			return []string{"monitor", "run", "bump"}
		},
	},
	{
		name:    "split",
		subject: "v",
		landed:  []string{`^feat\(v\): land the approved spec$`},
		outcome: "landed",
		next: func(s *scratch) []string {
			switch s.shown(s.subject) {
			case "":
				return []string{"queue", "v"}
			case "waiting for UAT":
				return nil
			}

			return []string{"fire"}
		},
	},
	{
		name:    "turn",
		subject: "v",
		landed:  []string{`^feat\(v\): land the approved spec$`},
		outcome: "landed",
		next: func(s *scratch) []string {
			switch s.shown(s.subject) {
			case "":
				return []string{"queue", "v"}
			case "waiting for UAT":
				return nil
			}

			return []string{"fire"}
		},
	},
	{
		// The verify session's first test passes on main's code, so its first proof does not stand.
		name:    "disprove",
		subject: "w",
		landed:  []string{`^feat\(w\): land the approved spec$`},
		outcome: "landed",
		told:    []string{`The gate passed with your work, so no test fails on`},
		next: func(s *scratch) []string {
			switch shown := s.shown(s.subject); {
			case shown == "":
				return []string{"queue", "w"}
			case shown == "waiting for UAT", strings.HasPrefix(shown, "stopped"):
				return nil
			}

			return []string{"fire"}
		},
	},
	{
		name:    "reject",
		given:   "accept",
		subject: "y",
		outcome: "dropped",
		next: func(s *scratch) []string {
			shown := s.shown(s.subject)
			switch {
			case shown == "" && s.ended("dropped"):
				return nil
			case shown == "":
				return []string{"queue", "y"}
			case strings.HasPrefix(shown, "waiting on its branch"):
				return []string{"reject", "y"}
			}

			return []string{"fire"}
		},
	},
	{
		name:    "drop",
		subject: "bump",
		landed:  []string{`by hand`},
		outcome: "dropped",
		next: func(s *scratch) []string {
			// Once the fix stopped, the developer makes state.txt current, and the check finds nothing to fix.
			switch shown := s.shown(s.subject); {
			case strings.HasPrefix(shown, "healthy"):
				return nil
			case strings.HasPrefix(shown, "stopped"):
				s.fixByHand()
			}

			return []string{"monitor", "run", "bump"}
		},
	},
	{
		name:    "tend",
		subject: "z",
		landed:  []string{`by hand`, `^feat\(z\): land the approved spec$`},
		outcome: "landed",
		told:    []string{`The gate wanted \.tmp/tended\.`, mainConflicts},
		once:    []string{mainConflicts},
		next: func(s *scratch) []string {
			switch s.shown(s.subject) {
			case "":
				return []string{"queue", "z"}
			case "stopped":
				s.writeByHand()
				return []string{"tend", "z"}
			case "waiting for UAT":
				return nil
			}

			return []string{"fire"}
		},
	},
}

// proof is a flow's part of TestKilledAtEachStep: its scratch repository, and what it has yet to drive from.
type proof struct {
	flow    flow
	scratch *scratch
	// queue holds each clone settle left the flow, to drive from in the next round.
	queue []snapshot
}

// due is whether p has a part in the round. That is its flow's own run in round 0, and then each clone settle left
// it.
func (p *proof) due(round int) bool { return round == 0 || len(p.queue) > 0 }

// round runs p's part of one round of the test. In round 0, it runs the flow to its end. In a later round, it
// restores each clone of p's queue, and drives the flow from there to its end. It stops each kiln at each step, and
// holds each rule then, as holds says. It claims each leftover that seen and the round so far lack, with a clone of
// the scratch root. seen holds what the rounds before showed.
func (p *proof) round(t *testing.T, round int, seen map[string]bool) {
	f, s, queue := p.flow, p.scratch, p.queue
	s.t, s.seen, p.queue = t, seen, nil
	// The flow's own run gets a drive from each leftover, by its class.
	byClass := round == 0 || f.redone
	// running counts the sessions each kiln started and has yet to end, by its PID.
	running := map[int]int{}
	s.watch(func(st step) string {
		l := s.holds(f, st, running[st.pid])
		if c := s.claim(l.keys(byClass)); c != nil {
			c.clone = s.snapshot(st, l)
		}

		running[st.pid] += st.started()
		return "GO"
	})

	if round == 0 {
		s.judge("", "killed at no step", f.drive(s))
	}

	root := filepath.Dir(s.work)
	for _, k := range queue {
		s.said.Reset()
		if err := errors.Join(os.RemoveAll(root), os.Rename(k.dir, root)); err != nil {
			t.Fatal(err)
		}

		_, lines := s.kept()
		s.calm, s.main, s.ledger = len(lines), "", nil
		left := f.name + ": " + k.class
		s.judge(left, fmt.Sprintf("killed at step %d, %s, which left %q and %q", k.n, k.what, left, k.lost), f.drive(s))
	}
}

// signaled runs the flow f to its end in a scratch repository of its own. Its kilns get the real signals of sig. It
// holds each rule at each step, as holds says. It fails for each SIGKILL of sig that no step came for.
func (f flow) signaled(t *testing.T, sig *signals) {
	s := newScratch(t, f)
	s.calm = -1
	s.watch(func(st step) string {
		l := s.holds(f, st, 0)
		session := ""
		if st.session() {
			session = st.leaves(l)
		}

		return sig.at(session, st.kind())
	})

	s.judge("", "signaled as its sessions started and ended, and killed at a step of each kind", f.drive(s))
	for _, kind := range sig.kills {
		t.Errorf("%s: no step of the kind %q came, so no kiln got a real SIGKILL there", f.name, kind)
	}
}

// claim is a leftover a flow showed in a round, by the keys of leftover. clone is what a kill there would leave. It
// is nil where the flow's own run went on from the leftover, between two commands.
type claim struct {
	keys  []string
	clone *snapshot
}

// claim keeps the leftover of keys as one s showed this round, and answers the claim. It answers nil for a leftover
// that is not new. An earlier round showed each of its keys, or s did in this round.
func (s *scratch) claim(keys []string) *claim {
	found := false
	for _, key := range keys {
		held := s.seen[key] || s.own[key]
		found = found || !held
		s.own[key] = true
	}

	if !found {
		return nil
	}

	s.claims = append(s.claims, &claim{keys: keys})
	return s.claims[len(s.claims)-1]
}

// settle keeps a round's claims in seen, flow by flow in the flows' order. It keeps a flow's claims in the order the
// flow made them. A flow drives next round from each clone whose leftover no claim before showed. So which flow
// drives a leftover never turns on which flow reached it first.
func settle(seen map[string]bool, proofs []*proof) {
	for _, p := range proofs {
		for _, c := range p.scratch.claims {
			fresh := false
			for _, key := range c.keys {
				fresh = fresh || !seen[key]
				seen[key] = true
			}

			if fresh && c.clone != nil {
				p.queue = append(p.queue, *c.clone)
			}
		}

		p.scratch.claims, p.scratch.own = nil, map[string]bool{}
	}
}

// snapshot is a clone of a scratch root, as a kill at its step would leave the root.
type snapshot struct {
	step
	leftover
	dir string
}

// snapshot clones the scratch root under s.taken, as the step st finds it, with l left.
func (s *scratch) snapshot(st step, l leftover) *snapshot {
	s.cloned++
	k := &snapshot{st, l, filepath.Join(s.taken, strconv.Itoa(s.cloned))}
	if err := unix.Clonefile(filepath.Dir(s.work), k.dir, unix.CLONE_NOFOLLOW); err != nil {
		s.t.Errorf("snapshot %s: %v", k.dir, err)
	}

	return k
}

// drive runs the commands the developer runs next in s, until the flow f reaches its end. It answers each rule s
// broke between two commands, and at the end.
func (f flow) drive(s *scratch) []string {
	var problems []string
	for range 24 {
		if why := overBudget(s.t); why != "" {
			s.t.Skip(why)
		}

		// No kiln runs here. So the run itself shows that the next command goes on from this leftover.
		l := s.left(f)
		s.claim(l.keys(true))
		s.status = s.read("kiln", "status")
		problems = append(problems, l.broken...)
		if atWork.MatchString(s.status) {
			problems = append(problems, "kiln status shows a build at work, though no kiln runs:\n"+s.status)
		}

		next := f.next(s)
		if next == nil {
			return slices.Concat(problems, s.problems(f), s.unanswered())
		}

		s.run("kiln", next...)
	}

	return append(problems, "the flow stopped short: kiln status shows "+s.read("kiln", "status"))
}

// judge fails the test when a run of the scratch found problems. It only logs them when defects holds left, the
// leftover the run started from. It fails that run once it recovers. after says what was done to the run.
func (s *scratch) judge(left, after string, problems []string) {
	t := s.t
	t.Helper()
	why, known := defects[left]
	switch {
	case known && len(problems) == 0:
		t.Errorf("%q now recovers: delete its row from defects, %q", left, why)
	case known:
		t.Logf("%q: a known defect: %s", left, why)
	case len(problems) > 0:
		t.Errorf("%s, %s:\n%s\n\n%s%s", s.subject, after, strings.Join(problems, "\n"), s.said.String(),
			s.stderrs())
	}
}

// stderrs is what each session's standard error holds, for a failure's report. A session that Claude Code refused
// says why only there.
func (s *scratch) stderrs() string {
	var b strings.Builder
	kept, _ := filepath.Glob(filepath.Join(s.work, ".git", "kiln", "sessions", "*", "stderr"))
	for _, path := range kept {
		if raw, _ := os.ReadFile(path); len(raw) > 0 {
			fmt.Fprintf(&b, "\nsession %s's standard error:\n%s", filepath.Base(filepath.Dir(path)), raw)
		}
	}

	return b.String()
}

// clones lists each session's clone that kiln's cache holds.
func (s *scratch) clones() []string {
	cache := filepath.Join(envValue(s.env, "HOME"), "Library", "Caches", "kiln")
	var found []string
	matched, _ := filepath.Glob(filepath.Join(cache, "*", ".verify-*"))
	found = append(found, matched...)

	return found
}

// envValue is the value env gives the variable key, or "".
func envValue(env []string, key string) string {
	for _, v := range env {
		if value, ok := strings.CutPrefix(v, key+"="); ok {
			return value
		}
	}

	return ""
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
	// status is what kiln status printed when drive last ran it.
	status string
	// seen holds each leftover the rounds before showed, by the keys of leftover. The flows share it, and none
	// changes it in a round. own holds each that s showed in this round, and claims those new to it, in order.
	seen, own map[string]bool
	claims    []*claim
	// taken is the directory of the clones that snapshot took, and cloned counts them.
	taken  string
	cloned int
	// main is the commit main was at when left last read it. named is the landing the subject's record then named.
	// main is "" once the developer or a clone moved main.
	main, named string
	// ledger holds the ledger's lines as left last read them, or none once a clone put an older ledger back.
	ledger []ledgerLine
	// calm is how many lines the ledger held when a kiln of the scratch was last killed. It is -1 while a signal
	// may kill one.
	calm int
	// landed and stray hold what onMain read of main, by the commit main was at.
	landed map[string][]string
	stray  map[string]bool
	// amends holds whether a commit holds amendments to the subject's spec, by the commit.
	amends map[string]bool
	// steps carries each step a kiln asks at, and answer answers one, once watch set both.
	steps  chan step
	answer func(step)
}

// newScratch makes the scratch repository of the flow f. Its scripted claude answers each call of a role with the
// role's steps.
func newScratch(t *testing.T, f flow) *scratch {
	work, env := scenariotest.Repo(t, filepath.Join("testdata", cmp.Or(f.given, f.name)+".txtar"))
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
		for attempt := 1; attempt <= 16; attempt++ {
			script[role][attempt] = map[int][]scenariotest.ClaudeStep{}
			for call := 1; call <= 16; call++ {
				script[role][attempt][call] = list
			}
		}
	}

	if err := script.Check(); err != nil {
		t.Fatal(err)
	}

	raw, err = json.Marshal(script)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(work, scenariotest.ClaudeScriptFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	return &scratch{t: t, work: work, env: env, subject: f.subject, own: map[string]bool{},
		landed: map[string][]string{}, stray: map[string]bool{}, amends: map[string]bool{}}
}

// run runs program with args in the scratch repository, and answers what it printed.
func (s *scratch) run(program string, args ...string) string {
	cmd := exec.CommandContext(s.t.Context(), program, args...)
	cmd.Dir, cmd.Env = s.work, s.env
	out, err := s.output(cmd)
	fmt.Fprintf(&s.said, "$ %s %s\n%s", program, strings.Join(args, " "), out)
	if err != nil {
		fmt.Fprintf(&s.said, "[%v]\n", err)
	}

	return out
}

// read runs program with args, which change nothing, and answers what it printed.
func (s *scratch) read(program string, args ...string) string {
	cmd := exec.CommandContext(s.t.Context(), program, args...)
	cmd.Dir, cmd.Env = s.work, s.env
	out, _ := s.output(cmd)
	return out
}

// shown is where kiln status showed the spec or monitor name, when drive last ran it. It is "" when it showed none.
func (s *scratch) shown(name Spec) string {
	shown := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(string(name)) + `: (.*)$`)
	m := shown.FindStringSubmatch(s.status)
	if m == nil {
		return ""
	}

	return m[1]
}

// ended is whether the ledger holds a build that ended with outcome.
func (s *scratch) ended(outcome string) bool {
	return strings.Contains(s.read("kiln", "ledger", "--kind", "build"), `"outcome":"`+outcome+`"`)
}

// fixByHand makes state.txt current in a commit on main, as the developer would, unless main holds it so.
func (s *scratch) fixByHand() {
	path := filepath.Join(s.work, "state.txt")
	if raw, _ := os.ReadFile(path); string(raw) == "current\n" {
		return
	}

	if err := os.WriteFile(path, []byte("current\n"), 0o644); err != nil {
		s.t.Fatal(err)
	}

	s.run("git", "commit", "-qam", "fix(bump): make state.txt current by hand")
	s.main = ""
}

// retired is whether main no longer holds the spec of the flow's subject.
func (s *scratch) retired() bool {
	spec := "main:.kiln/specs/" + string(s.subject) + ".md"
	return exec.Command("git", "-C", s.work, "cat-file", "-e", spec).Run() != nil
}

// problems lists each way the scratch repository breaks a rule that holds at the end of f, whatever killed kiln.
func (s *scratch) problems(f flow) []string {
	found := s.ledgerProblems(f)
	refs := s.refs()
	// left holds, at each step, that main's landings are the first of the flow's.
	if landed, _ := s.onMain(refs); len(landed) != len(f.landed) {
		found = append(found, fmt.Sprintf("main holds the landings %q, not one of each of %q", landed, f.landed))
	}

	// The worktrees that no one build owns stay.
	for _, tree := range s.worktrees() {
		if !slices.Contains([]string{".main", ".gauges", ".monitor-check"}, tree) {
			found = append(found, "a worktree is left behind: "+tree)
		}
	}

	for _, ref := range slices.Sorted(maps.Keys(refs)) {
		if strings.HasPrefix(ref, "refs/heads/kiln/") {
			found = append(found, "a branch is left behind: "+ref)
		}
	}

	for _, clone := range s.clones() {
		found = append(found, "a clone is left behind: "+clone)
	}

	book, _ := os.ReadFile(filepath.Join(s.work, ".git", "kiln", "state.json"))
	if strings.Contains(string(book), `"owed"`) {
		found = append(found, "the book still owes a session a message: "+string(book))
	}

	for _, pattern := range f.told {
		if !s.told(regexp.MustCompile(pattern)) {
			found = append(found, "no session was told "+pattern)
		}
	}

	for _, pattern := range f.once {
		if prompt := s.toldTwice(regexp.MustCompile(pattern)); prompt != "" {
			found = append(found, "a session was told "+pattern+" twice in one prompt:\n"+prompt)
		}
	}

	return found
}

// ledgerProblems lists each run that kiln ledger prints with no end, and each build with no outcome. An audit run
// that ended with a result holds a tally. The last build must end as f's does.
func (s *scratch) ledgerProblems(f flow) []string {
	var found []string
	var outcome *string
	for line := range strings.Lines(s.read("kiln", "ledger")) {
		var e struct {
			Kind    string  `json:"kind"`
			Outcome *string `json:"outcome"`
		}

		var run ledgerRun
		err := json.Unmarshal([]byte(line), &e)
		if err == nil && e.Kind == "run" {
			err = json.Unmarshal([]byte(line), &run)
		}

		switch {
		case err != nil:
			found = append(found, "kiln ledger printed a line the test cannot read: "+line)
		case e.Kind == "run" && run.End == nil:
			found = append(found, "a run has no end: "+line)
		case e.Kind == "run" && run.untallied():
			found = append(found, "an audit run ended with a result, but its line holds no tally: "+line)
		case e.Kind == "build" && e.Outcome == nil:
			found = append(found, "a build has no outcome: "+line)
		case e.Kind == "build":
			outcome = e.Outcome
		}
	}

	if outcome == nil || *outcome != f.outcome {
		found = append(found, fmt.Sprintf("the last build did not end %s", f.outcome))
	}

	return found
}

// ledgerRun is a run line, as kiln ledger prints it.
type ledgerRun struct {
	Role  string  `json:"role"`
	End   *string `json:"end"`
	Ended string  `json:"ended"`
	Found *int    `json:"found"`
}

// untallied is whether r is an audit run that ended with a result, yet holds no tally. The audit's reply ends with
// a tally, as the contract sets it.
func (r ledgerRun) untallied() bool {
	answered := r.Role == "audit" && r.Ended == "result"
	return answered && r.Found == nil
}

// told is whether a session's prompt matches text.
func (s *scratch) told(text *regexp.Regexp) bool {
	prompts, _ := filepath.Glob(filepath.Join(s.work, scenariotest.ClaudeCallsDir, "*", "*", "*",
		scenariotest.ClaudePromptFile))
	return slices.ContainsFunc(prompts, func(p string) bool {
		raw, _ := os.ReadFile(p)
		return text.Match(raw)
	})
}

// toldTwice is a session's prompt that matches text twice, or "" when none does.
func (s *scratch) toldTwice(text *regexp.Regexp) string {
	prompts, _ := filepath.Glob(filepath.Join(s.work, scenariotest.ClaudeCallsDir, "*", "*", "*",
		scenariotest.ClaudePromptFile))
	for _, p := range prompts {
		if raw, _ := os.ReadFile(p); len(text.FindAll(raw, 2)) > 1 {
			return string(raw)
		}
	}

	return ""
}

// writeByHand commits a z.go of the developer's own on main, unless main holds one. It conflicts with the build's.
func (s *scratch) writeByHand() {
	path := filepath.Join(s.work, "z.go")
	if _, err := os.Stat(path); err == nil {
		return
	}

	if err := os.WriteFile(path, []byte("// Package scratch is where kiln's builds land.\npackage scratch\n\n"+
		"// Z answers 0, by hand.\nfunc Z() int { return 0 }\n"), 0o644); err != nil {
		s.t.Fatal(err)
	}

	s.run("git", "add", "z.go")
	s.run("git", "commit", "-qm", "feat: write z by hand")
	s.main = ""
}
