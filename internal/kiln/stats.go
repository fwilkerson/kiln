package kiln

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
)

// buildStats is one build, as the ledger holds it.
type buildStats struct {
	entry
	outcome    string
	amendments string
	// summary and report are a fix's, as its outcome line holds them.
	summary, report string
	runs            []run
	gates           []entry
	// readings are the build's gauge lines, as they were written.
	readings []entry
}

// run is one session run. Its end is zero while it runs.
type run struct {
	role, session string
	start, end    time.Time
	ended         string
	totals        claude.Totals
	why           reason
	// pid is the kiln fire's that started it.
	pid int
	// from and to are its bytes of the session's stream. A to below 0 reads to the stream's end.
	from, to int64
	changed  []string
	// gates counts the build's gates that ran before it.
	gates int
}

// collect groups the ledger's lines by build, in the order the builds started.
//
// An end line kiln wrote before it kept the stream's length holds none. That run's bytes end where the session's
// next run starts, or at the stream's end.
func collect(entries []entry) []*buildStats {
	c := collector{byID: map[string]*buildStats{}, unbounded: map[string]func(to int64){}}
	for _, e := range entries {
		c.add(e)
	}

	for _, bound := range c.unbounded {
		bound(-1)
	}

	return c.all
}

// collector groups the ledger's lines by build. unbounded ends, by session, a run whose end line holds no length.
type collector struct {
	all       []*buildStats
	byID      map[string]*buildStats
	unbounded map[string]func(to int64)
}

func (c *collector) add(e entry) {
	s := c.byID[e.Build]
	switch {
	case e.Kind == builtKind:
		c.built(e)
	case s == nil:
		// A line of a build the ledger holds no build line of counts for nothing.
	case e.Kind == startKind:
		c.start(s, e)
	case e.Kind == endKind:
		c.end(s, e)
	case e.Kind == gateKind:
		s.gates = append(s.gates, e)
	case e.Kind == gaugeKind:
		s.readings = append(s.readings, e)
	case e.Kind == outcomeKind:
		s.outcome = e.Outcome
		s.amendments = cmpOr(e.Amendments, s.amendments)
		s.summary, s.report = e.Summary, e.Report
	}
}

// built adds the build that e starts. kiln writes a build line again if it died before the build's session started.
func (c *collector) built(e entry) {
	if c.byID[e.Build] != nil {
		return
	}

	c.byID[e.Build] = &buildStats{entry: e}
	c.all = append(c.all, c.byID[e.Build])
}

// start adds the run that e starts to s. The session's last run, if unbounded, ends where this one starts.
func (c *collector) start(s *buildStats, e entry) {
	if bound := c.unbounded[e.Session]; bound != nil {
		bound(e.Streamed)
		delete(c.unbounded, e.Session)
	}

	s.runs = append(s.runs, run{role: e.Role, session: e.Session, start: e.At, why: e.reason, pid: e.PID,
		from: e.Streamed, gates: len(s.gates)})
}

// end ends the run of s that e ends.
func (c *collector) end(s *buildStats, e entry) {
	i := slices.IndexFunc(s.runs, func(r run) bool { return r.session == e.Session && r.end.IsZero() })
	if i < 0 {
		return
	}

	s.runs[i].end, s.runs[i].ended, s.runs[i].to, s.runs[i].changed = e.At, e.Ended, e.Streamed, e.Changed
	if e.Totals != nil {
		s.runs[i].totals = *e.Totals
	}

	if e.Streamed == 0 {
		c.unbounded[e.Session] = func(to int64) { s.runs[i].to = to }
	}
}

// brief is the build's time, cost, turns and failed calls, for its landing commit.
func (s *buildStats) brief() string {
	t := total(s.runs, s.gates)
	return fmt.Sprintf("%s · %s · %s · %s", t.time.duration("time"), t.cost.dollars("cost"), t.turns.count("turn"),
		t.failed.count("failed call"))
}

// sums are what a build's runs and gates add up to, which kiln ledger prints and kiln tui shows. A nil sum is
// unknown.
type sums struct {
	took                  *time.Duration
	cost                  *float64
	turns, failed, rounds *int
}

// sums adds up the build's runs and gates, as total does. Each sum is unknown while one of its runs lacks its part.
func (s *buildStats) sums() sums {
	return total(s.runs, s.gates).sums(s.runs)
}

// sums are t's sums, which runs add up to. Its rounds are the highest round of runs, unknown while a run's reason is.
func (t totals) sums(runs []run) sums {
	rounds := new(0)
	for _, r := range runs {
		if r.why.Why == "" {
			rounds = nil
			break
		}

		*rounds = max(*rounds, r.why.Round)
	}

	return sums{took: t.time.whole(), cost: t.cost.whole(), turns: t.turns.whole(), failed: t.failed.whole(),
		rounds: rounds}
}

// sum is a number summed over runs, of which lacks could not be read.
type sum[T int | float64 | time.Duration] struct {
	known T
	lacks int
	of    int
}

// whole is the sum, or nil while a run lacks its part.
func (s sum[T]) whole() *T {
	if s.lacks > 0 {
		return nil
	}

	return &s.known
}

func (s *sum[T]) add(v *T) {
	s.of++
	if v == nil {
		s.lacks++
		return
	}

	s.known += *v
}

// shown shows a sum that holds an unknown as at least its known part. It counts the runs the sum lacks.
func (s sum[T]) shown(what, known string) string {
	switch {
	case s.lacks == 0:
		return known
	case s.lacks == s.of:
		return what + " unknown"
	}

	return fmt.Sprintf("at least %s, %d of %d runs unknown", known, s.lacks, s.of)
}

func (s sum[T]) dollars(what string) string {
	return s.shown(what, fmt.Sprintf("$%.2f", float64(s.known)))
}

func (s sum[T]) count(what string) string {
	return s.shown(what+"s", plural(int(s.known), what))
}

func (s sum[T]) duration(what string) string {
	return s.shown(what, minutes(time.Duration(s.known)))
}

// totals is what a set of runs, and a build's gates, add up to. A run that has not ended lacks its failed calls.
type totals struct {
	time   sum[time.Duration]
	turns  sum[int]
	cost   sum[float64]
	failed sum[int]
}

// total adds up runs and gates.
func total(runs []run, gates []entry) totals {
	var t totals
	for _, g := range gates {
		t.time.known += g.End.Sub(g.At)
	}

	for _, r := range runs {
		var took *time.Duration
		var failed *int
		if !r.end.IsZero() {
			took, failed = new(r.end.Sub(r.start)), new(len(r.totals.Failed))
		}

		t.time.add(took)
		t.turns.add(r.totals.Turns)
		t.cost.add(r.totals.CostUSD)
		t.failed.add(failed)
	}

	return t
}

// minutes shows a span of time in seconds below a minute, and in minutes above.
func minutes(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Round(time.Second)/time.Second))
	case d < 10*time.Minute:
		return fmt.Sprintf("%.1f min", d.Minutes())
	}

	return fmt.Sprintf("%.0f min", d.Minutes())
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}

	return fmt.Sprintf("%d %ss", n, what)
}

// sumUp sums up the build of rec, which waits on the developer. kiln fire prints its lines under its last.
// Each line comes from the record, the ledger and git, so the same build always sums up the same. A line kiln can't
// read shows as unknown, and the others still show. tree is the build's worktree. landedAt is the commit the build
// moved main to, if it landed.
func (r *Repo) sumUp(ctx context.Context, rec record, tree git.Worktree, landedAt git.Rev) []string {
	lines := []string{r.summary(rec.Build), roundsOf(rec) + " · " + r.auditOf(rec)}
	lines = append(lines, r.ending(ctx, rec, landedAt)...)
	// A split build stopped before its first stack has no stack to name.
	place := tree
	if _, err := os.Stat(string(tree)); err != nil {
		place = ""
	}

	if where := workPlace(rec, place); where != "" {
		lines = append(lines, where)
	}

	return append(lines, "next: "+nextStep(rec, landedAt))
}

// auditOf says what the audit did on the build of rec, as the ledger holds it.
func (r *Repo) auditOf(rec record) string {
	s, err := r.stats(rec.Build)
	var runs []run
	if s != nil {
		runs = s.runs
	}

	return auditDid(rec.Audited != "", runs, s != nil && err == nil)
}

// ending says how the build of rec ended: the files it changed, its amendments, or what failed.
func (r *Repo) ending(ctx context.Context, rec record, landedAt git.Rev) []string {
	switch {
	case rec.State == landed:
		return []string{r.filesChanged(ctx, landedAt+"^", landedAt)}
	case rec.State == amended:
		branch := git.Rev(rec.Name.branch())
		base, err := r.git.MergeBase(ctx, mainBranch, branch)
		changed := fmt.Sprintf("files changed unknown: %v", err)
		if err == nil {
			changed = r.filesChanged(ctx, base, branch)
		}

		return []string{changed, strings.TrimRight(rec.Amendments, "\n")}
	case len(rec.Failing) > 0:
		return []string{"failing: " + strings.Join(rec.Failing, ", ")}
	}

	return nil
}

// workPlace says where the work of a build stopped by its gate is, in tree. It is "" for any other build.
func workPlace(rec record, tree git.Worktree) string {
	switch {
	case rec.State != stopped, byKiln(rec), rec.Session == "", tree == "":
		return ""
	}

	what := "the work"
	switch {
	case rec.Accepted && rec.Monitor:
		what = "the accepted fix"
	case rec.Accepted:
		what = "the accepted spec"
	}

	return fmt.Sprintf("%s is on %s, in %s", what, rec.Name.branch(), tree)
}

// roundsOf is the rounds the build of rec used, of those kiln.json gave its last gate.
func roundsOf(rec record) string {
	if rec.Limit == nil {
		return fmt.Sprintf("%d of an unknown number of rounds", rec.Rounds)
	}

	return fmt.Sprintf("%d of %s", rec.Rounds, plural(*rec.Limit, "round"))
}

// auditDid says what the audit did, from whether it answered and the build's runs. known is false when the ledger
// could not be read, or holds no line of the build. An audit that read the work again, for kiln.json's reaudit, says
// how many times it read it.
func auditDid(audited bool, runs []run, known bool) string {
	audits := slices.ContainsFunc(runs, func(r run) bool { return r.role == auditRole })
	findings := slices.ContainsFunc(runs, func(r run) bool { return r.why.Why == whyFindings })
	again := readAgain(runs)
	switch {
	case !known:
		return "what the audit did is unknown"
	case !audited && !audits:
		return "the audit had not run"
	case !audited:
		return "the audit ended with no result"
	case findings:
		return "the audit sent findings back" + again
	case audits:
		return "the audit found nothing" + again
	}

	return "the build changed nothing kiln audits, so no audit ran"
}

// readAgain says how many times the audit read the work, when it read it more than once.
func readAgain(runs []run) string {
	reads, _ := auditReads(runs)
	if reads < 2 {
		return ""
	}

	return fmt.Sprintf(", and read the work %d times", reads)
}

// auditReads counts the audit's reads of the work in runs, and the reads whose findings went back. A retry is no new
// read. A split build's findings go to both sessions, and count once.
func auditReads(runs []run) (reads, findings int) {
	sent := true
	for _, r := range runs {
		switch {
		case r.role == auditRole && r.why.Try <= 1:
			reads, sent = reads+1, false
		case r.why.Why == whyFindings && !sent:
			findings, sent = findings+1, true
		}
	}

	return reads, findings
}

// filesChanged counts the files, insertions and deletions from the commit from to the commit to.
func (r *Repo) filesChanged(ctx context.Context, from, to git.Rev) string {
	s, err := r.git.Shortstat(ctx, from, to)
	if err != nil {
		return fmt.Sprintf("files changed unknown: %v", err)
	}

	return fmt.Sprintf("%s changed, %s, %s", plural(s.Files, "file"), plural(s.Insertions, "insertion"),
		plural(s.Deletions, "deletion"))
}

// nextStep is what the developer runs next on the build of rec, which waits on them. It never names a command spec
// 4's table refuses in the state rec is in. landedAt is the commit a landed build moved main to.
func nextStep(rec record, landedAt git.Rev) string {
	name := rec.Name
	switch {
	case rec.Monitor:
		return fixStep(rec)
	case rec.State == landed:
		return fmt.Sprintf("read it with git show %s, then run kiln accept %s, or change the spec and run "+
			"kiln queue %s", short(landedAt), name, name)
	case rec.State == amended:
		return fmt.Sprintf("read the amendments, then run kiln accept %s, or kiln reject %s", name, name)
	case rec.Accepted:
		return landAgainStep(rec)
	}

	return fmt.Sprintf("%s; or kiln queue %s, which builds anew", tendStep(name), name)
}

// tendStep names kiln tend on name, which opens a session of the developer's own on what waits on a repair.
func tendStep(name Spec) string {
	return fmt.Sprintf("run kiln tend %s, to repair it in a session of your own", name)
}

// landAgainStep is what the developer runs on the stopped landing of rec, a spec's accepted amendments or a fix.
func landAgainStep(rec record) string {
	return fmt.Sprintf("%s; or fix main, and run kiln accept %s to land it again, or kiln reject %s",
		tendStep(rec.Name), rec.Name, rec.Name)
}

// fixStep is what the developer runs next on the monitor's fix of rec. It is "" in a state no fix is in. A landed
// fix leaves no record.
func fixStep(rec record) string {
	switch {
	case rec.fixHalted() && rec.Accepted:
		return fmt.Sprintf("run kiln monitor run %s, which lands it", rec.Name)
	case rec.fixHalted():
		return fmt.Sprintf("run kiln monitor run %s, which drops the fix and runs the monitor again", rec.Name)
	case rec.State == building:
		return "wait for it to end"
	case rec.landing():
		return "wait for the next kiln monitor run, which lands it"
	case conflicted(rec):
		return fmt.Sprintf("%s; or run kiln reject %s, and the monitor's next run fixes anew, from main",
			tendStep(rec.Name), rec.Name)
	case rec.Accepted:
		return landAgainStep(rec)
	case rec.State == waiting:
		return fixNext(rec.Name, outWaits)
	case rec.State == stopped:
		return fixNext(rec.Name, outStopped)
	}

	return ""
}

// conflicted reports whether rec is an accepted fix with no session, whose landing stopped on a conflict with main.
func conflicted(rec record) bool {
	return rec.Monitor && rec.Accepted && rec.Session == "" && strings.HasPrefix(rec.Why, fixConflicts)
}

// byKiln reports whether the build of rec stopped for an error of kiln's, not its gate's.
func byKiln(rec record) bool {
	return rec.State == stopped && strings.HasPrefix(rec.Why, couldNotGoOn)
}

// sideClone is the clone of role's session of a split build of name, beside the build's worktree, tree.
func sideClone(tree git.Worktree, name Spec, role string) string {
	return filepath.Join(filepath.Dir(string(tree)), "."+role+"-"+string(name))
}
