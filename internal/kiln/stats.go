package kiln

import (
	"context"
	"fmt"
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
	runs       []run
	gates      []entry
}

// run is one session run. Its end is zero while it runs.
type run struct {
	role, session string
	start, end    time.Time
	ended         string
	totals        claude.Totals
	why           reason
	// pid is the kiln run's that started it.
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
	var all []*buildStats
	byID := map[string]*buildStats{}
	unbounded := map[string]func(to int64){}
	for _, e := range entries {
		// kiln writes a build line again if it died before the build's session started.
		if e.Kind == builtKind && byID[e.Build] == nil {
			byID[e.Build] = &buildStats{entry: e}
			all = append(all, byID[e.Build])
		}

		s := byID[e.Build]
		if s == nil || e.Kind == builtKind {
			continue
		}

		switch e.Kind {
		case startKind:
			if bound := unbounded[e.Session]; bound != nil {
				bound(e.Streamed)
				delete(unbounded, e.Session)
			}

			s.runs = append(s.runs, run{role: e.Role, session: e.Session, start: e.At, why: e.reason, pid: e.PID,
				from: e.Streamed, gates: len(s.gates)})
		case endKind:
			i := slices.IndexFunc(s.runs, func(r run) bool { return r.session == e.Session && r.end.IsZero() })
			if i < 0 {
				continue
			}

			s.runs[i].end, s.runs[i].ended, s.runs[i].to, s.runs[i].changed = e.At, e.Ended, e.Streamed, e.Changed
			if e.Totals != nil {
				s.runs[i].totals = *e.Totals
			}

			if e.Streamed == 0 {
				unbounded[e.Session] = func(to int64) { s.runs[i].to = to }
			}
		case gateKind:
			s.gates = append(s.gates, e)
		case outcomeKind:
			s.outcome = e.Outcome
			s.amendments = cmpOr(e.Amendments, s.amendments)
		}
	}

	for _, bound := range unbounded {
		bound(-1)
	}

	return all
}

// brief is the build's time, cost, turns and failed calls, for its landing commit.
func (s *buildStats) brief() string {
	t := total(s.runs, s.gates)
	return fmt.Sprintf("%s · %s · %s · %s", t.time.duration("time"), t.cost.dollars("cost"), t.turns.count("turn"),
		plural(t.failed, "failed call"))
}

// sum is a number summed over runs, of which lacks could not be read.
type sum[T int | float64 | time.Duration] struct {
	known T
	lacks int
	of    int
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

// totals is what a set of runs, and a build's gates, add up to.
type totals struct {
	time   sum[time.Duration]
	turns  sum[int]
	cost   sum[float64]
	failed int
}

// total adds up runs and gates.
func total(runs []run, gates []entry) totals {
	var t totals
	for _, g := range gates {
		t.time.known += g.End.Sub(g.At)
	}

	for _, r := range runs {
		var took *time.Duration
		if !r.end.IsZero() {
			d := r.end.Sub(r.start)
			took = &d
		}

		t.time.add(took)
		t.turns.add(r.totals.Turns)
		t.cost.add(r.totals.CostUSD)
		t.failed += len(r.totals.Failed)
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

// sumUp sums up the build of rec, which waits on the developer. kiln run prints its lines under its last.
// Each line comes from the record, the ledger and git, so the same build always sums up the same. A line kiln can't
// read shows as unknown, and the others still show. tree is the build's worktree. landedAt is the commit the build
// moved main to, if it landed.
func (r *Repo) sumUp(ctx context.Context, rec record, tree git.Worktree, landedAt git.Rev) []string {
	s, err := r.stats(rec.Build)
	var runs []run
	if s != nil {
		runs = s.runs
	}

	lines := []string{r.summary(rec.Build), roundsOf(rec) + " · " + auditDid(rec.Audited, runs, s != nil && err == nil)}
	switch {
	case rec.State == landed:
		lines = append(lines, r.filesChanged(ctx, landedAt+"^", landedAt))
	case rec.State == amended:
		base, err := r.git.MergeBase(ctx, mainBranch, git.Rev(branchOf(rec.Name)))
		changed := fmt.Sprintf("files changed unknown: %v", err)
		if err == nil {
			changed = r.filesChanged(ctx, base, git.Rev(branchOf(rec.Name)))
		}

		lines = append(lines, changed, strings.TrimRight(rec.Amendments, "\n"))
	case len(rec.Failing) > 0:
		lines = append(lines, "failing: "+strings.Join(rec.Failing, ", "))
	}

	if rec.State == stopped && !byKiln(rec) && rec.Session != "" {
		what := "the work"
		if rec.Accepted {
			what = "the accepted spec"
		}

		lines = append(lines, fmt.Sprintf("%s is on %s, in %s", what, branchOf(rec.Name), tree))
	}

	return append(lines, "next: "+nextStep(rec, tree, landedAt))
}

// roundsOf is the rounds the build of rec used, of those kiln.json gave its last gate.
func roundsOf(rec record) string {
	if rec.Limit == nil {
		return fmt.Sprintf("%d of an unknown number of rounds", rec.Rounds)
	}

	return fmt.Sprintf("%d of %s", rec.Rounds, plural(*rec.Limit, "round"))
}

// auditDid says what the audit did, from whether it answered and the build's runs. known is false when the ledger
// could not be read, or holds no line of the build.
func auditDid(audited bool, runs []run, known bool) string {
	var audits, findings bool
	for _, r := range runs {
		audits = audits || r.role == auditRole
		findings = findings || r.why.Why == whyFindings
	}

	switch {
	case !known:
		return "what the audit did is unknown"
	case !audited && !audits:
		return "the audit had not run"
	case !audited:
		return "the audit ended with no result"
	case findings:
		return "the audit sent findings back"
	case audits:
		return "the audit found nothing"
	}

	return "no Go package changed, so no audit ran"
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
// 4's table refuses in the state rec is in. A build stopped past its last round first resumes its session, in tree,
// to ask it why.
func nextStep(rec record, tree git.Worktree, landedAt git.Rev) string {
	name := rec.Name
	var then string
	switch {
	case rec.State == landed:
		return fmt.Sprintf("read it with git show %s, then run kiln accept %s, or change the spec and run "+
			"kiln queue %s", short(landedAt), name, name)
	case rec.State == amended:
		return fmt.Sprintf("read the amendments, then run kiln accept %s, or kiln reject %s", name, name)
	case rec.Accepted && byKiln(rec):
		then = fmt.Sprintf("fix main, or what the error names, and run kiln accept %s to land it again, or "+
			"kiln reject %s", name, name)
	case rec.Accepted:
		then = fmt.Sprintf("fix main, and run kiln accept %s to land it again, or kiln reject %s", name, name)
	case byKiln(rec):
		then = fmt.Sprintf("fix what the error names, then run kiln queue %s, which builds anew", name)
	case rec.Session != "":
		then = fmt.Sprintf("fix main or the spec, and run kiln queue %s", name)
	default:
		return fmt.Sprintf("read kiln status, fix main or the spec, then run kiln queue %s", name)
	}

	// The session never saw kiln's error, so it can't say why.
	if byKiln(rec) || rec.Session == "" {
		return then
	}

	return fmt.Sprintf("cd %s && claude --resume %s, to ask the session why;\n      then %s", shellQuote(string(tree)),
		rec.Session, then)
}

// byKiln reports whether the build of rec stopped for an error of kiln's, not its gate's.
func byKiln(rec record) bool {
	return rec.State == stopped && strings.HasPrefix(rec.Why, couldNotGoOn)
}
