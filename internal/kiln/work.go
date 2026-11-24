package kiln

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/fwilkerson/kiln/internal/claude"
	"github.com/fwilkerson/kiln/internal/git"
)

// One loop runs a spec's build and a monitor's fix, under each workflow. A build has one or two seats, each a
// session with its own rounds and the messages kiln owes it:
//
//	"build"              code ──▶ gate ──▶ audit ──▶ land
//	"verify-first"       verify ──▶ proof ──▶ join ──▶ code ──▶ gate ──▶ audit ──▶ land
//	"build-and-verify"   verify ──▶ proof ──┐
//	                     code ──────────────┴▶ join ──▶ gate ──▶ audit ──▶ land
//
// The verify session works in a clone of main of its own, on kiln/<name>.verify. The code session works in the
// build's worktree, on kiln/<name>. A proof gates the verify session's commit, which holds no code of the build's. It
// stands once the gate fails on tests alone. Then the join merges that commit into kiln/<name>. Each run of the code
// session after a join opens with what the join brought. kiln commits, proves, joins and gates only between runs.

// order is when a build's two sessions run, as its workflow names it.
type order int

const (
	// alone is the workflow build: one session writes the code and its tests.
	alone order = iota
	// sideBySide starts both sessions at once.
	sideBySide
	// inTurn starts the code session once the verify session's tests are proved.
	inTurn
)

// orderOf is the order of the workflow w. It is the one place kiln reads a workflow's order.
func orderOf(w workflow) order {
	switch w {
	case buildAndVerify:
		return sideBySide
	case verifyFirst:
		return inTurn
	}

	return alone
}

// isolated is whether o runs a verify session apart from the code session.
func (o order) isolated() bool { return o != alone }

// seatName names one of a build's two seats.
type seatName string

const (
	codeSeat   seatName = "code"
	verifySeat seatName = "verify"
)

// seat is what a build's record keeps of one of its sessions.
type seat struct {
	Session string `json:"session,omitempty"`
	// Rounds counts the setbacks sent back to the session.
	Rounds int `json:"rounds,omitempty"`
	// Owed holds each message kiln owes the session, oldest first.
	Owed []message `json:"owed,omitempty"`
	// From is the length of the session's stream as its last run started, until that run ended. A stream still From
	// long shows a run that never began.
	From *int64 `json:"from,omitempty"`
}

// again is s as a landing queued again keeps it: its session and its messages, with no round spent.
func (s seat) again() seat {
	s.Rounds = 0
	return s
}

// message is one message kiln owes a session, with why kiln tells it.
type message struct {
	Text string `json:"text"`
	reason
}

// seat is the seat s of r.
func (r *record) seat(s seatName) *seat {
	if s == verifySeat {
		return &r.Verify
	}

	return &r.Code
}

// seats names each seat of r that holds a session.
func (r *record) seats() []seatName {
	var held []seatName
	for _, s := range []seatName{codeSeat, verifySeat} {
		if r.seat(s).Session != "" {
			held = append(held, s)
		}
	}

	return held
}

// rounds is the most rounds one seat of r used.
func (r *record) rounds() int {
	return max(r.Code.Rounds, r.Verify.Rounds)
}

// owe adds m to what kiln owes the seat s of r. It and paid are the only writes of a seat's Owed. It clones the slice,
// so a record that shares it keeps its own.
func owe(r *record, s seatName, m message) {
	at := r.seat(s)
	at.Owed = append(slices.Clone(at.Owed), m)
}

// paid removes the n oldest messages kiln owed the seat s of r, which a run just told. It ends the run's mark.
func paid(r *record, s seatName, n int) {
	at := r.seat(s)
	at.Owed, at.From = slices.Clone(at.Owed[min(n, len(at.Owed)):]), nil
}

// toldOf is the reason of a run that tells owed: the oldest message's, at the highest round among them. Its Also
// lists the later messages' whys, so the ledger keeps each fact a message carried.
func toldOf(owed []message) reason {
	why := owed[0].reason
	var also []string
	for _, m := range owed[1:] {
		if m.Round > why.Round {
			why.Round, why.Rounds = m.Round, m.Rounds
		}

		also = append(also, m.Why)
	}

	why.Also = strings.Join(also, ", ")
	return why
}

// owes is whether kiln owes the seat s of the build's record a message.
func (b *build) owes(s seatName) bool {
	rec := b.now()
	return len(rec.seat(s).Owed) > 0
}

// owesAny is whether kiln owes either seat a message.
func (b *build) owesAny() bool { return b.owes(codeSeat) || b.owes(verifySeat) }

// work runs the build until it ends: it lands, waits, stops, or a session ends it. A first start, a killed kiln and
// kiln tend all enter here, since it reads only the record and git.
func (b *build) work(ctx context.Context) error {
	for {
		proved, err := b.prove(ctx)
		if err != nil {
			return err
		}

		due := b.due()
		if over, err := b.tell(ctx, due); err != nil || over {
			return err
		}

		// A session answers before any gate of the joined work.
		waits := !proved || b.owesAny()
		if waits && len(due) == 0 {
			return errors.New("kiln owes a session a message, yet no session may run")
		}

		if waits {
			continue
		}

		if done, err := b.judge(ctx); err != nil || done {
			return err
		}
	}
}

// due names the seats that run now, each one kiln owes a message. It is the one place that reads the order. The verify
// session runs whenever kiln owes it. Once a proof stands, the code session runs only while kiln owes the verify
// session nothing. In turn, it runs only once a proof stands.
func (b *build) due() []seatName {
	var due []seatName
	verify := b.owes(verifySeat)
	if verify {
		due = append(due, verifySeat)
	}

	stood := b.now().Tested != ""
	runs := map[order]bool{alone: true, sideBySide: !stood || !verify, inTurn: stood && !verify}[b.order]
	if runs && b.owes(codeSeat) {
		due = append(due, codeSeat)
	}

	return due
}

// prove commits the verify session's work, proves it, and joins it into kiln/<name>. It answers whether a proof
// stands, joined. It proves and joins only while kiln owes the verify session nothing. A merge the verify session left
// unfinished goes back to it, as a round, with the files that still conflict.
func (b *build) prove(ctx context.Context) (bool, error) {
	switch {
	case !b.order.isolated():
		return true, nil
	case b.owes(verifySeat):
		return false, nil
	}

	head, err := b.verifyWork(ctx)
	if u, ok := errors.AsType[*git.Unmerged](err); ok {
		report := fmt.Sprintf(unfinished, strings.Join(u.Files, "\n"))
		return false, b.goBack(verifySeat, setback{why: whyConflict, report: report, failing: u.Files})
	}

	if err != nil {
		return false, err
	}

	if stands, err := b.proof(ctx, head); err != nil || !stands {
		return false, err
	}

	return b.join(ctx)
}

// verifyWork commits what the verify session left in its clone, and fetches its branch into the repository. It answers
// the commit, or a *git.Unmerged for a merge the session left unfinished, which kiln never commits.
func (b *build) verifyWork(ctx context.Context) (git.Rev, error) {
	clone := b.r.clone(b.rec.Name, verifyRole)
	head, err := b.r.git.Commit(ctx, clone, b.message("the verify session's work"))
	if err != nil {
		return "", err
	}

	return head, b.r.git.Fetch(ctx, clone, b.rec.Name.sideBranch(verifyRole))
}

// proof proves head, the verify session's commit, unless it leaves the proved tests as they were. It answers whether a
// proof stands. A proof that does not stand goes back to the verify session, as a round.
func (b *build) proof(ctx context.Context, head git.Rev) (bool, error) {
	rec := b.now()
	if head == rec.Tested {
		return true, nil
	}

	kept, err := b.testsKept(ctx, rec.Tested, head)
	switch {
	case err != nil:
		return false, err
	case kept:
		b.set(func(r *record) { r.Tested = head })
		return true, nil
	}

	if err := b.changedNothing(ctx, rec.Unproved, head); err != nil {
		return false, err
	}

	g := b.verifyGuard()
	g.head = head
	v, err := b.judgeWork(ctx, gating{proof: true}, g)
	if err != nil {
		return false, err
	}

	if !v.failsOnTests() {
		return false, b.disproved(head, v)
	}

	b.set(func(r *record) { r.Tested, r.Unproved, r.Proof, r.Proved = head, "", v.report, v.tests })
	b.say("the tests fail on main's code: %s", strings.Join(v.tests, ", "))
	return true, nil
}

// testsKept is whether head leaves the tests at tested, a proved commit, as they were. Only an amendment, or the spec
// as kiln folded its accepted amendments, then differs. It is false while no proof has stood. It is false too where the
// verify session changed a path it may not write since tested. Then the proof's gate refuses that path.
func (b *build) testsKept(ctx context.Context, tested, head git.Rev) (bool, error) {
	if tested == "" {
		return false, nil
	}

	tests, err := b.verifyChanged(ctx, tested, head)
	if err != nil {
		return false, err
	}

	changesTest := func(p string) bool { return p != b.rec.Name.amendPath() }
	if slices.ContainsFunc(tests, changesTest) {
		return false, nil
	}

	refused, err := b.refusedSince(ctx, tested, head)
	return len(refused) == 0, err
}

// refusedSince lists each path from tested to head changed that the verify session may not write. It leaves out each
// path head holds as its merge base with main does. A merge of main brings such paths. It leaves out the spec's files
// that head holds as kiln folded them, too.
func (b *build) refusedSince(ctx context.Context, tested, head git.Rev) ([]string, error) {
	main, err := b.r.mainTip(ctx)
	if err != nil {
		return nil, err
	}

	branched, err := b.r.git.Branched(ctx, main, head)
	if err != nil {
		return nil, err
	}

	changed, err := b.r.git.Changed(ctx, tested, head)
	if err != nil {
		return nil, err
	}

	g := b.verifyGuard()
	g.head = head
	var refused []string
	for _, p := range changed {
		if g.guarded(p) == "" || !slices.Contains(branched, p) {
			continue
		}

		kept, err := b.r.keptAccepted(ctx, g, main, p)
		if err != nil {
			return nil, err
		}

		if !kept {
			refused = append(refused, p)
		}
	}

	return refused, nil
}

// verifyChanged lists each path from to to changed that the verify session may write.
func (b *build) verifyChanged(ctx context.Context, from, to git.Rev) ([]string, error) {
	changed, err := b.r.git.Changed(ctx, from, to)
	g := b.verifyGuard()
	return slices.DeleteFunc(changed, func(p string) bool { return g.guarded(p) != "" }), err
}

// changedNothing stops the build when head holds the tree that unproved, a proof's commit that did not stand, held.
// Then its session changed nothing since that proof. Taking out a refused path is a change, which a new proof gates.
func (b *build) changedNothing(ctx context.Context, unproved, head git.Rev) error {
	if unproved == "" {
		return nil
	}

	changed, err := b.r.git.Changed(ctx, unproved, head)
	if err != nil || len(changed) > 0 {
		return err
	}

	return &outOfRounds{stop{Why: changedNoTest, Kind: stopUnproved}}
}

// changedNoTest is why a build stopped once the verify session changed nothing after a proof that did not stand.
const changedNoTest = "the verify session changed no test after a proof that did not stand"

// disproved sends v, a proof of head that did not stand, back to the verify session, as a round. A gate that
// passed and one that failed on more than tests each say so. The write of the round, or of the stop past the last
// round, records head as Unproved. So the verify session is told before changedNothing reads its work.
func (b *build) disproved(head git.Rev, v verdict) error {
	report := fmt.Sprintf(testPassed, b.job.asks(), b.job.wayOut())
	if !v.passed {
		report = fmt.Sprintf(testBroke, v.report)
	}

	unproved := func(r *record) { r.Unproved = head }
	return b.goBack(verifySeat, setback{why: whyNoTest, report: report, failing: v.failing, with: unproved})
}

// join merges the proved commit into kiln/<name>, once its work is committed. It answers whether kiln/<name> holds
// the proved commit. Whether it does is read from git, so a kill before the merge joins again. The proved commit holds
// main as mergeMain left it, so the join brings main into the work too. A conflict goes back to the code session, as a
// round. A run of that session that kiln cut short is told again first, so kiln commits no half-done answer.
func (b *build) join(ctx context.Context) (bool, error) {
	rec := b.now()
	tested := rec.Tested
	if held, err := b.holds(ctx, tested); err != nil || held {
		return held, err
	}

	if rec.Code.From != nil {
		return false, nil
	}

	if _, back, err := b.sessionWork(ctx); err != nil || back.why != "" {
		return false, b.goBackOn(codeSeat, back, err)
	}

	if err := b.putBack(ctx, tested); err != nil {
		return false, err
	}

	branch := b.rec.Name.sideBranch(verifyRole)
	conflict, err := b.r.git.Merge(ctx, b.tree, branch, b.message("join the verify session's proved tests"))
	if err != nil || conflict == nil {
		return err == nil, err
	}

	report := fmt.Sprintf(joinConflicts, branch, strings.Join(conflict.Files, "\n"))
	return false, b.goBack(codeSeat, setback{why: whyConflict, report: report, failing: conflict.Files})
}

// holds is whether kiln/<name>, as the build's worktree has it checked out, holds the commit at.
func (b *build) holds(ctx context.Context, at git.Rev) (bool, error) {
	tip, err := b.r.git.Head(ctx, b.tree)
	if err != nil {
		return false, err
	}

	base, err := b.r.git.MergeBase(ctx, tip, at)
	return base == at, err
}

// goBackOn sends back to s, as goBack does, unless err is set: it answers err then.
func (b *build) goBackOn(s seatName, back setback, err error) error {
	if err != nil {
		return err
	}

	return b.goBack(s, back)
}

// goBack sends a setback back to the seat s as a round, as said words it. Past the seat's last round it stops the
// build, once it made the setback's change to the record. It allows as many rounds as kiln.json gave the last gate.
func (b *build) goBack(s seatName, back setback) error {
	goesBack, stops, prompt := back.said()
	rounds, rec := b.limit(), b.now()
	at := rec.seat(s)
	if at.Rounds >= rounds {
		if back.with != nil {
			b.set(back.with)
		}

		return &outOfRounds{stop{Why: stops + " after its last round:\n" + back.report, Failing: back.failing}}
	}

	m := message{Text: prompt, Why: back.why, Round: at.Rounds + 1, Rounds: rounds}
	b.set(func(r *record) {
		r.seat(s).Rounds++
		owe(r, s, m)
		if back.with != nil {
			back.with(r)
		}
	})

	b.say("%s; it goes back to %s, round %d of %d", goesBack, map[seatName]string{codeSeat: "the session",
		verifySeat: "the verify session"}[s], m.Round, rounds)
	return nil
}

// roleOf is the role of the session at the seat s, as its runs' lines name it.
func (b *build) roleOf(s seatName) string {
	switch {
	case s == verifySeat:
		return verifyRole
	case b.fix != nil:
		return fixRole
	}

	return buildRole
}

// tell runs each session of due, at once, with what kiln owes it, and waits for each to end. It answers whether a
// session ended the build.
func (b *build) tell(ctx context.Context, due []seatName) (over bool, err error) {
	turns := make([]*turn, len(due))
	for i, s := range due {
		if turns[i], err = b.turn(ctx, s); err != nil {
			return false, err
		}
	}

	errs := make([]error, len(turns))
	var wg sync.WaitGroup
	for i, t := range turns {
		wg.Go(func() { _, errs[i] = b.talk(ctx, t.l, t.why, t) })
	}

	wg.Wait()
	return slices.ContainsFunc(turns, func(t *turn) bool { return t.over }), errors.Join(errs...)
}

// turn is one run of a seat's session, with what it tells. n counts the messages it tells, and told is the proved
// commit its prompt tells of. heard reads its reply. over is set once the reply ended the build.
type turn struct {
	seat  seatName
	l     claude.Launch
	why   reason
	n     int
	told  git.Rev
	heard func(claude.Ended) answer
	over  bool
}

// answer is what a run's reply asks: the build's end, a message to the other seat, or a title.
type answer struct {
	over  bool
	owed  *message
	title string
}

// starts is the change of the write that holds t's start line: it marks the run, at streamed. A nil turn changes
// nothing.
func (t *turn) starts(streamed int64) func(*record) {
	if t == nil {
		return nil
	}

	return func(r *record) { r.seat(t.seat).From = &streamed }
}

// ends is the change of the write that follows the end of t's run, which ended as ended. It removes the messages t
// told, with what the reply asked. A run a usage limit ended told nothing. A nil turn changes nothing. A failed
// verify run answered no proof, so the next proof gates the verify session's clone again, as a round.
func (t *turn) ends(ended claude.Ended) func(*record) {
	if t == nil || ended.Limit != nil {
		return nil
	}

	a := t.heard(ended)
	t.over = a.over
	return func(r *record) {
		paid(r, t.seat, t.n)
		if t.seat == codeSeat {
			r.Told = t.told
		}

		if t.seat == verifySeat && ended.Failed {
			r.Unproved = ""
		}

		if a.owed != nil {
			owe(r, verifySeat, *a.owed)
		}

		r.Title = cmpOr(a.title, r.Title)
	}
}

// turn is the run of the seat s that tells what kiln owes it, for the reason toldOf gives. A run cut short is told
// its messages again, after a line that says kiln stopped. The code session's run after a join opens with what the
// join brought.
func (b *build) turn(ctx context.Context, s seatName) (*turn, error) {
	rec := b.now()
	owed := rec.seat(s).Owed
	texts := make([]string, len(owed))
	for i, m := range owed {
		texts[i] = m.Text
	}

	t := &turn{seat: s, why: toldOf(owed), n: len(owed), told: rec.Told}
	if b.cutShort(rec.seat(s)) {
		texts = append([]string{interrupted}, texts...)
		t.why.Why, t.why.Try = whyInterrupted, 0
	}

	if s == codeSeat && rec.Tested != rec.Told {
		note, held, err := b.joined(ctx, rec)
		if err != nil {
			return nil, err
		}

		if held {
			t.told = rec.Tested
		}

		if note != "" {
			texts = append([]string{note}, texts...)
		}
	}

	l, err := b.job.launch(ctx, b, s, strings.Join(texts, "\n\n"))
	l.Role = b.roleOf(s)
	t.l, t.heard = l, func(ended claude.Ended) answer { return b.job.heard(b, s, ended) }
	return t, err
}

// cutShort is whether a run of at's session began, but kiln stopped before it ended.
func (b *build) cutShort(at *seat) bool {
	if at.From == nil {
		return false
	}

	streamed, err := b.r.claude.Streamed(at.Session)
	return err == nil && streamed != *at.From
}

// joined is what a join brought the code session since rec.Told. Its note names the tests' files, the proof's report,
// and each new amendment's wording. held is whether the work holds the proved commit: a join that conflicted brought
// nothing yet. The note is "" until then. It is "" too where the join changed no file the verify session writes.
func (b *build) joined(ctx context.Context, rec record) (note string, held bool, err error) {
	if held, err = b.holds(ctx, rec.Tested); err != nil || !held {
		return "", false, err
	}

	if rec.Told != "" {
		if changed, err := b.verifyChanged(ctx, rec.Told, rec.Tested); err != nil || len(changed) == 0 {
			return "", true, err
		}
	}

	main, err := b.r.mainTip(ctx)
	if err != nil {
		return "", true, err
	}

	files, err := b.r.git.Branched(ctx, main, rec.Tested)
	if err != nil {
		return "", true, err
	}

	g := b.verifyGuard()
	files = slices.DeleteFunc(files, func(p string) bool { return g.guarded(p) != "" })
	note = fmt.Sprintf(joinedTests, strings.Join(files, ", "), rec.Proof)
	amended, err := b.amendedSince(ctx, rec)
	return note + amended, true, err
}

// amendedSince is the note of the spec as rec.Tested holds it, with its amendments folded in. It is "" where those
// stand as rec.Told held them, and for a fix.
func (b *build) amendedSince(ctx context.Context, rec record) (string, error) {
	if b.fix != nil {
		return "", nil
	}

	now, err := b.amendedSpec(ctx, rec.Tested)
	if err != nil {
		return "", err
	}

	was, err := b.amendedSpec(ctx, cmpOr(rec.Told, rec.Approved))
	if err != nil || was == now {
		return "", err
	}

	return "\n\n" + fmt.Sprintf(amendedNote, now), nil
}

// judge commits what the code session left, gates it, and has the audit read it. Then it takes the work to the
// job's own end. It answers true once the build has ended. Each setback goes back to the code session.
func (b *build) judge(ctx context.Context) (bool, error) {
	head, back, err := b.sessionWork(ctx)
	if err != nil || back.why != "" {
		return false, b.goBackOn(codeSeat, back, err)
	}

	gates, err := b.job.gates(ctx, b, head)
	if err != nil {
		return false, err
	}

	if gates {
		if sent, err := b.check(ctx, head); err != nil || sent {
			return false, err
		}
	}

	back, err = b.job.passed(ctx, b, head)
	if err != nil || back.why == "" {
		return err == nil, err
	}

	return false, b.job.goneBack(ctx, b, back)
}

// check gates head, then has the audit read it. It answers whether either sent something back: a failed gate, as a
// round, or the audit's findings.
func (b *build) check(ctx context.Context, head git.Rev) (bool, error) {
	v, err := b.gate(ctx, head)
	if err != nil {
		return false, err
	}

	b.keepFailingAtMain(v.atMain)
	if !v.passed {
		return true, b.goBack(codeSeat, setback{why: whyGate, report: v.report, failing: v.failing})
	}

	return b.job.audited(ctx, b)
}

// mergeMain merges main into the build, once main moved under it. A build with one session takes main in its work.
// An isolated build takes main in the verify session's clone alone, and the join brings it into the work. So main
// reaches the work one way, and one conflict costs one round. A conflict in a file the verify session alone writes
// arises in its clone. Any other arises at the join.
func (b *build) mergeMain(ctx context.Context) error {
	if !b.order.isolated() {
		return b.mergeMainAt(ctx, b.tree, codeSeat)
	}

	clone := b.r.clone(b.rec.Name, verifyRole)
	if err := b.r.git.Pull(ctx, clone, mainBranch); err != nil {
		return err
	}

	return b.mergeMainAt(ctx, clone, verifySeat)
}

// sendBack sends back, which kept a landing from main, to the code session, as a round. In an isolated build, main
// moved unless the work changed nothing, so kiln merges main first, as mergeMain says. The join then meets a conflict
// with main, and sends that back itself.
func (b *build) sendBack(ctx context.Context, back setback) error {
	moved := b.order.isolated() && back.why != whyUnchanged
	if moved {
		if err := b.mergeMain(ctx); err != nil {
			return err
		}
	}

	if moved && back.why == whyConflict {
		return nil
	}

	return b.goBack(codeSeat, back)
}

// mergeMainAt merges main into checkout, the checkout of the seat s. A clean merge is a commit of kiln's. A
// conflict goes back to s, as a round, or stops a fix whose verify session's tests conflict. The round's write
// clears MergeMain, since the round is the merge that kiln tend owed. Past the last round, goBack clears it before
// the stop.
func (b *build) mergeMainAt(ctx context.Context, checkout git.Worktree, s seatName) error {
	conflict, err := b.r.git.Merge(ctx, checkout, mainBranch, b.message("merge main"))
	switch {
	case err != nil, conflict == nil:
		return err
	case s == verifySeat && b.fix != nil:
		return conflictStop(testsConflict, conflict.Files)
	}

	report := fmt.Sprintf(conflicts, strings.Join(conflict.Files, "\n"))
	return b.goBack(s, setback{why: whyConflict, report: report, failing: conflict.Files,
		with: func(r *record) { r.MergeMain = false }})
}

const (
	joinedTests = "kiln joined the verify session's tests into your branch. They are in %s, and the gate fails on " +
		"them at main's code, as it must until your work:\n\n%s"
	joinConflicts = "The branch %[1]s holds main and the verify session's proved tests. Your work conflicts with it " +
		"in these files:\n%[2]s\n\nMerge %[1]s into your branch with git merge, resolve each conflict, commit, and " +
		"end your turn."
	testPassed = "The gate passed with your work, so no test fails on %s. Write a test that fails on main's code " +
		"for that reason, and end your turn. Where main's code already does it, %s."
	testBroke = "The gate failed, but not on tests alone: a command failed with no test named, or a path is refused. " +
		"A test that does not compile on main's code names no failed test. Make the gate fail only on a test, " +
		"and end your turn.\n\n%s"
	testsConflict = "the verify session's tests conflict with main, in these files:\n"
)
