package kiln

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/fwilkerson/kiln/internal/git"
)

// state is where a spec stands, in the words kiln status shows.
type state string

const (
	queued   state = "queued"
	building state = "building"
	stopped  state = "stopped"
	// landed is a spec whose build is on main, waiting for the developer's acceptance.
	landed state = "waiting for UAT"
	// amended is a spec whose finished build waits on its branch, with amendments for the developer to read.
	amended state = "waiting on its branch with amendments"
	// waiting is a monitor's fix that passed, and waits on its branch for kiln accept.
	waiting state = "waiting on its branch"
)

// record is what kiln keeps of one spec the developer queued.
type record struct {
	Name  Spec  `json:"name"`
	State state `json:"state"`
	// Approved is main's tip when the developer queued the spec.
	Approved git.Rev `json:"approved"`
	// Build is the ID of this queue's build, which names it in the ledger.
	Build string `json:"build,omitempty"`
	// Session is the build's Claude Code session, once it has one.
	Session string `json:"session,omitempty"`
	// Verify is a split build's verify session, once it has one. Session is then its build session.
	Verify string `json:"verify,omitempty"`
	// OldTests is true once kiln removed a revised spec's old acceptance tests from a split build's build side.
	OldTests bool `json:"old_tests,omitempty"`
	// ForBuild and ForVerify each hold a reply kiln read, but has not yet passed on to that session.
	ForBuild  string `json:"for_build,omitempty"`
	ForVerify string `json:"for_verify,omitempty"`
	// Asked is true while kiln waits on the verify session's bugs line, in its answer to a failing stack.
	Asked bool `json:"asked,omitempty"`
	// Told is the spec, with its amendments folded in, as kiln last told a split build's build session it.
	Told string `json:"told,omitempty"`
	// Running names the roles of a split build's sessions whose turn has not ended. kiln resumes only these.
	Running []string `json:"running,omitempty"`
	// Rounds counts the failed gates sent back to the session.
	Rounds int `json:"rounds,omitempty"`
	// Limit is how many rounds kiln.json gave the build's last gate. It is nil until a gate has run.
	Limit *int `json:"limit,omitempty"`
	// Audited is the commit the audit last read, once it has answered.
	Audited git.Rev `json:"audited_at,omitempty"`
	// Accepted is a spec the developer accepted while it waited on its branch. kiln lands it as a build does, then
	// retires it.
	Accepted bool `json:"accepted,omitempty"`
	// AcceptedAt is the commit of a monitor's fix the developer accepted: its branch's tip when kiln accept took it.
	AcceptedAt git.Rev `json:"accepted_at,omitempty"`
	// Before is the commit kiln/<name> stood at when its landing began. It is set only while a landing runs. A
	// landing that doesn't move main puts the branch back there.
	Before git.Rev `json:"before,omitempty"`
	// Landing is the commit a landing moves main to, set before main moves, and Onto the commit main stood at. Once
	// main holds Landing, the next kiln records the landing, though kiln died first.
	Landing git.Rev `json:"landing,omitempty"`
	Onto    git.Rev `json:"onto,omitempty"`
	stop
	// FailingAtMain names the tests that failed at main before the build.
	FailingAtMain []string `json:"failing_at_main,omitempty"`
	// Amendments is the text of the amendment file the build left on its branch.
	Amendments string `json:"amendments,omitempty"`
	// Monitor is true of a monitor's fix, whose Name is the monitor's. Approved is then main's tip when it started.
	Monitor bool `json:"monitor,omitempty"`
	// Title is the title the fix's agent last gave the commit that lands it, in place of kiln's own.
	Title string `json:"title,omitempty"`
	// TestSession is a test-first fix's test session, once it has one. Session is its fix session, once that starts.
	TestSession string `json:"test_session,omitempty"`
	// Tested is the commit of a test-first fix's tests that the gate last failed on alone, on kiln/<name>.test. The
	// fix holds each test file as Tested holds it.
	Tested git.Rev `json:"tested,omitempty"`
	// Owed is the message kiln owes each session of a stopped build that kiln tend saw to. kiln resumes each with it.
	Owed string `json:"owed,omitempty"`
	// Tended is true from kiln tend's queue of the stopped build until a kiln takes it. Any run in the specs' lane
	// takes it meanwhile. A build halted since then waits for kiln fire, as any halted build does.
	Tended bool `json:"tended,omitempty"`
	// LandedAt is the commit main moved to when the build's work landed.
	LandedAt git.Rev `json:"landed_at,omitempty"`
	runner
}

// stop is why a build stopped. Its kind, not its prose, decides what the developer runs next.
type stop struct {
	// Why says why the build stopped, with the gate's output.
	Why string `json:"why,omitempty"`
	// Failing names what failed a build stopped past its last round. It names tests, commands and refused paths, or
	// the files in conflict with main.
	Failing []string `json:"failing,omitempty"`
	Kind    stopKind `json:"stop,omitempty"`
}

// stopKind is what stopped a build. The zero kind is a setback the work met past its last round.
type stopKind string

const (
	// stopConflict is an accepted fix with no session, whose landing conflicts with main.
	stopConflict stopKind = "conflict"
	// stopKiln is an error of kiln's, not the work's, such as a full disk.
	stopKiln stopKind = "kiln"
)

// runner is what the kiln fire that builds a spec holds of it. next replaces it whole, so no kiln fire inherits it.
type runner struct {
	// PID is the process ID of the kiln fire that marked the spec building. Once it is gone, the build has halted.
	PID int `json:"pid,omitempty"`
	// Paused is when a usage limit that paused the build resets.
	Paused time.Time `json:"paused,omitzero"`
}

// book is every spec queued that kiln has not retired, in the order they were queued. It keeps what the gate found
// at main.
type book struct {
	Specs []*record `json:"specs"`
	// Main holds the gate's checks at main, by the commit and the commands they ran.
	Main map[string][]check `json:"main,omitempty"`
}

// errRefused is the error kind for a command that kiln refuses for the state a spec is in.
var errRefused = errors.New("refused")

func (b *book) find(name Spec) *record {
	if i := slices.IndexFunc(b.Specs, func(r *record) bool { return r.Name == name }); i >= 0 {
		return b.Specs[i]
	}

	return nil
}

// The developer's commands on a spec, which the book takes or refuses by the state the spec is in.
const (
	queueCommand  = "queue"
	acceptCommand = "accept"
	rejectCommand = "reject"
	tendCommand   = "tend"
)

// may answers nil when the book takes command on the spec name, or why it refuses. It holds spec 4's table, and
// spec 16's column for kiln tend:
//
//	state                                  queue                            accept           reject         tend
//	no record                              queues it                        refuses          refuses        refuses
//	queued                                 queues it again, from main's tip refuses          refuses        refuses
//	building                               refuses                          refuses          refuses        refuses
//	stopped                                builds anew from main            refuses          refuses        takes it
//	waiting for UAT                        only once its spec on main has   retires it       refuses        refuses
//	                                       changed
//	waiting on its branch with amendments  refuses                          folds, and lands drops build,   refuses
//	                                                                                         branch
//	landing accepted amendments, queued    refuses                          refuses          refuses        refuses
//	landing accepted amendments, building  refuses                          refuses          refuses        refuses
//	landing accepted amendments, stopped   refuses                          lands it again   drops build,   takes it
//	                                                                                         branch
//	a fix, building                        refuses                          refuses          refuses        refuses
//	a fix, stopped                         refuses                          refuses          refuses        takes it
//	a fix, waiting on its branch           refuses                          lands it         drops build,   refuses
//	                                                                                         branch
//	a fix's landing, queued or building    refuses                          refuses          refuses        refuses
//	a fix's landing, stopped               refuses                          lands it again   drops build,   takes it
//	                                                                                         branch
//
// A fix is a monitor's, under its name. Its accept retires nothing, since a monitor has no spec. kiln tend takes a
// monitor with no record by its last run, as Tend says. Each refusal of kiln tend names the next step.
//
// changed answers whether the spec on main differs from the spec at the commit since. Only queue calls it, on a spec
// waiting for UAT.
func (b *book) may(command string, name Spec, changed func(since git.Rev) (bool, error)) error {
	why, err := b.refusal(command, name, changed)
	if why == "" {
		return err
	}

	return fmt.Errorf("%w: %s: %s", errRefused, name, why)
}

// refusal says why the book refuses command on the spec name, or "" if it takes it.
func (b *book) refusal(command string, name Spec, changed func(since git.Rev) (bool, error)) (string, error) {
	r := b.find(name)
	switch {
	case r == nil && command == queueCommand:
		return "", nil
	case r == nil:
		return "kiln holds no record of it", nil
	case command == tendCommand:
		return r.tendRefusal(), nil
	case r.halted() && !r.Monitor:
		// A plain kiln fire takes up a spec's build or landing, but no monitor's fix.
		return "it is halted, as " + haltedNext, nil
	case r.landing():
		return fmt.Sprintf("its landing is %s", r.State), nil
	case r.Monitor:
		return r.fixRefusal(command), nil
	case command == queueCommand:
		return r.queueRefusal(changed)
	}

	return r.takeRefusal(command), nil
}

// landing reports whether kiln has yet to land r, an accepted build. The book takes no command on it until it stops.
func (r *record) landing() bool {
	return r.Accepted && r.State != stopped
}

// queueRefusal says why kiln queue refuses the spec of r, or "" if it takes it.
func (r *record) queueRefusal(changed func(since git.Rev) (bool, error)) (string, error) {
	switch {
	case r.State == amended, r.Accepted:
		return fmt.Sprintf("it is %s: accept or reject them first, with kiln accept %s or kiln reject %s",
			shownState(r), r.Name, r.Name), nil
	case r.State == building:
		return "it is building: queue it again once its build ends", nil
	case r.State != landed:
		return "", nil
	}

	revised, err := changed(r.Approved)
	if err != nil || revised {
		return "", err
	}

	return fmt.Sprintf("it is %s, and %s on main is as it was when its build was queued, so it would build the "+
		"same work: change the spec, commit it, and queue it", landed, r.Name.file()), nil
}

// fixRefusal says why command refuses the monitor's fix of r, or "" if it takes it.
func (r *record) fixRefusal(command string) string {
	switch {
	case command == queueCommand:
		return fmt.Sprintf("it names a monitor, whose fix is %s: kiln queue takes a spec", shownState(r))
	case r.State == waiting, r.Accepted:
		return ""
	}

	return fmt.Sprintf("its monitor's fix is %s, and kiln %s takes a fix waiting on its branch", shownState(r),
		command)
}

// takeRefusal says why command, kiln accept or kiln reject, refuses the spec of r. It says "" if command takes it.
// A landing of accepted amendments has reached it only once stopped.
func (r *record) takeRefusal(command string) string {
	switch {
	case r.State == amended, r.Accepted, r.State == landed && command == acceptCommand:
		return ""
	case r.State == landed:
		return fmt.Sprintf("it is %s, and its work is on main: change the spec, commit it, and queue it", landed)
	}

	why := fmt.Sprintf("it is %s, and kiln %s takes a spec waiting on its branch with amendments", shownState(r),
		command)
	if command == acceptCommand {
		why += ", or one waiting for UAT"
	}

	return why
}

// tendRefusal says why kiln tend refuses r, or "" if it takes it. It takes a stopped build, landing or fix.
func (r *record) tendRefusal() string {
	switch {
	case r.State == stopped:
		return ""
	case r.Monitor:
		return fmt.Sprintf("its monitor's fix is %s; next: %s", shownState(r), fixStep(*r))
	case r.halted():
		return "it is halted, as " + haltedNext
	case r.landing():
		return fmt.Sprintf("its landing is %s", r.State)
	case r.State == landed, r.State == amended:
		return fmt.Sprintf("it is %s; next: %s", r.State, nextStep(*r, r.LandedAt))
	}

	return fmt.Sprintf("it is %s, and kiln tend takes a stopped build", r.State)
}

// unacceptedStop reports whether r, which may be nil, is a monitor's stopped fix the developer has not accepted.
func (r *record) unacceptedStop() bool {
	return r != nil && r.Monitor && r.State == stopped && !r.Accepted
}

// shownState is where the spec of r stands, in the words kiln status shows.
func shownState(r *record) string {
	shown := string(r.State)
	if r.halted() {
		shown = "halted"
	}

	switch {
	case r.Monitor && r.Accepted:
		return shown + ", landing an accepted fix"
	case r.Accepted:
		return shown + ", landing accepted amendments"
	}

	return shown
}

// halted reports whether r is building for a kiln fire that is gone. The next kiln fire takes it up again.
func (r *record) halted() bool {
	return r.State == building && !alive(r.PID)
}

// haltedNext is what kiln status and kiln queue say of a halted build.
const haltedNext = "its kiln fire is gone: kiln fire takes it up again"

// queue queues a new build of name from the spec at main's tip, main. It answers whether an earlier build left a
// branch to drop. It answers the ID of an earlier build it drops before that build ended.
func (b *book) queue(name Spec, main git.Rev, changed func(since git.Rev) (bool, error)) (drop bool, dropped string,
	err error) {
	if err := b.may(queueCommand, name, changed); err != nil {
		return false, "", err
	}

	r := b.find(name)
	if r == nil {
		b.Specs = append(b.Specs, &record{Name: name, State: queued, Approved: main, Build: newBuild()})
		return false, "", nil
	}

	// A queued build with a session was interrupted, so it had not ended.
	if r.State == queued && r.Session != "" {
		dropped = r.Build
	}

	drop = r.State != queued
	*r = record{Name: name, State: queued, Approved: main, Build: newBuild()}
	return drop, dropped, nil
}

// accept answers the record of a spec the developer accepts, as it stood. A stopped landing of accepted amendments is
// queued again afresh. It keeps only its build, sessions, turns cut short and audit, and that it lands accepted
// amendments. It keeps a branch a landing left to put back, too. It keeps the rounds' limit, the title and the
// commit the developer accepted. Without the limit, a fix's landing that goes back to its session has no round.
func (b *book) accept(name Spec) (record, error) {
	if err := b.may(acceptCommand, name, nil); err != nil {
		return record{}, err
	}

	r := b.find(name)
	was := *r
	if r.Accepted {
		*r = record{Name: name, State: queued, Approved: r.Approved, Build: r.Build, Session: r.Session,
			Verify: r.Verify, OldTests: r.OldTests, Told: r.Told, Running: r.Running, Limit: r.Limit,
			Audited: r.Audited, Accepted: true, AcceptedAt: r.AcceptedAt, Before: r.Before, Monitor: r.Monitor,
			Title: r.Title, TestSession: r.TestSession, Tested: r.Tested}
	}

	return was, nil
}

// resume queues again the stopped build of name, as it stood, once kiln tend saw to it. It keeps the build's ID,
// sessions, audited commit and rounds' limit, and what a split build holds of its sessions. Its rounds start
// again at 0. The record owes each session a message, with why the build stopped and note, the tend session's note.
// It holds the message before why is cleared, so a kiln that dies next loses neither.
func (b *book) resume(name Spec, note string) error {
	r := b.find(name)
	switch {
	case r == nil, r.Monitor, r.Accepted, r.State != stopped:
		return fmt.Errorf("%w: %s: kiln tend goes on with a spec's stopped build alone", errRefused, name)
	}

	r.Owed, r.Tended = owedMessage(r.Why, note), true
	r.State, r.Rounds, r.stop, r.runner = queued, 0, stop{}, runner{}
	return nil
}

// reject removes the record of a spec whose amendments the developer turns away, and answers it.
func (b *book) reject(name Spec) (record, error) {
	if err := b.may(rejectCommand, name, nil); err != nil {
		return record{}, err
	}

	r := *b.find(name)
	b.retire(name)
	return r, nil
}

func (b *book) retire(name Spec) {
	b.Specs = slices.DeleteFunc(b.Specs, func(r *record) bool { return r.Name == name })
}

// next takes the first queued record of the lane l, which the caller holds. It marks it building by the kiln pid. In
// the specs' lane, with only, it takes the spec named only, or a landing of accepted amendments. A kiln accept leaves
// such a landing for the kiln that holds the lane. In the monitors' lane, it takes only a fix's accepted landing. The
// run must be started for the fix's name, or for landings.
func (b *book) next(l lane, only Spec, pid int) (record, bool) {
	for _, r := range b.Specs {
		if r.State == queued && r.runsFor(l, only) {
			r.State, r.runner, r.Tended = building, runner{PID: pid}, false
			return *r, true
		}
	}

	return record{}, false
}

// runsFor is whether a run of the lane l started for only builds the record r. A run builds no record of the other
// lane. A run started for no spec builds every spec. Every run builds a landing of accepted amendments. Every run
// builds a build kiln tend queued again, until a kiln takes it. A fix's accepted landing is built only by
// a run started for its name, or for landings.
func (r *record) runsFor(l lane, only Spec) bool {
	switch {
	case !l.holds(r):
		return false
	case r.Monitor:
		return r.Accepted && (r.Name == only || only == landings)
	}

	return only == "" || r.Name == only || r.Accepted || r.Tended
}

// landings, as a run's field only, starts the run for the accepted landings alone, as kiln monitor run does. No
// spec's name starts with a dot.
const landings Spec = ".landings"

// requeue queues again each record of the lane l, which the caller holds, that a kiln that stopped left building. A
// build that was building goes on first. In the monitors' lane, it queues a fix's accepted landing again, and leaves
// any other fix for dropLost.
func (b *book) requeue(l lane) {
	for _, s := range b.Specs {
		if l.leftBuilding(s) && s.goesOnAfterKiln() {
			s.State = queued
		}
	}

	slices.SortStableFunc(b.Specs, func(x, y *record) int { return goesOn(y) - goesOn(x) })
}

// goesOnAfterKiln is whether a kiln that stopped leaves the build of r to go on. A spec's does, and a fix's accepted
// landing. Any other fix is dropped, by kiln monitor run.
func (r *record) goesOnAfterKiln() bool {
	return !r.Monitor || r.Accepted
}

// dropLost drops each record of the lane l that a kiln left building, and that does not go on. That is a fix the
// developer had not accepted. The caller holds l. It answers the fixes dropped. The specs' lane drops none.
func (b *book) dropLost(l lane) []record {
	var lost []record
	b.Specs = slices.DeleteFunc(b.Specs, func(s *record) bool {
		if l.leftBuilding(s) && !s.goesOnAfterKiln() {
			lost = append(lost, *s)
			return true
		}

		return false
	})

	return lost
}

func goesOn(r *record) int {
	if r.State == queued && r.Session != "" {
		return 1
	}

	return 0
}

// store keeps the book in a file under the git common directory, so every worktree of the repository shares it.
type store struct {
	dir string
}

const (
	bookFile = "state.json"
	lockFile = "lock"
	// buildLock is the lock every kiln that drives a build session shares, in kiln's directory under the user's cache
	// directory. Reinstalling kiln takes the lock alone.
	buildLock = "build.lock"
)

// update applies change to the book, holding the store's lock from reading the book to writing it.
func (s store) update(change func(*book) error) error {
	unlock, err := s.lock(lockFile, syscall.LOCK_EX)
	if err != nil {
		return err
	}

	defer unlock()

	b, err := s.read()
	if err != nil {
		return err
	}

	if err := change(&b); err != nil {
		return err
	}

	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}

	tmp := filepath.Join(s.dir, bookFile+".tmp")
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}

	return os.Rename(tmp, filepath.Join(s.dir, bookFile))
}

// read reads the book as the last update left it.
func (s store) read() (book, error) {
	var b book
	raw, err := os.ReadFile(filepath.Join(s.dir, bookFile))
	if errors.Is(err, fs.ErrNotExist) {
		return b, nil
	}

	if err != nil {
		return b, err
	}

	return b, json.Unmarshal(raw, &b)
}

// set applies change to the record of name.
func (s store) set(name Spec, change func(*record)) error {
	return s.update(func(b *book) error {
		if r := b.find(name); r != nil {
			change(r)
		}

		return nil
	})
}

// lock takes the lock file name, as how says: syscall.LOCK_EX or LOCK_SH. With LOCK_NB, it refuses at once a lock
// another kiln holds.
func (s store) lock(name string, how int) (unlock func(), err error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(filepath.Join(s.dir, name), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}

	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		f.Close()
		return nil, err
	}

	return func() { f.Close() }, nil
}
