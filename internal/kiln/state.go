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
	// Audited is the commit the audit last read, once it has answered. An older kiln kept a bool under "audited",
	// which this key leaves unread.
	Audited git.Rev `json:"audited_at,omitempty"`
	// Accepted is a spec the developer accepted while it waited on its branch. kiln lands it as a build does, then
	// retires it.
	Accepted bool `json:"accepted,omitempty"`
	// Before is the commit kiln/<name> stood at when its landing began. It is set only while a landing runs. A
	// landing that doesn't move main puts the branch back there.
	Before git.Rev `json:"before,omitempty"`
	// Why says why the build stopped, with the gate's output.
	Why string `json:"why,omitempty"`
	// Failing names what failed a build stopped past its last round. It names tests, commands and refused paths, or
	// the files in conflict with main.
	Failing []string `json:"failing,omitempty"`
	// FailingAtMain names the tests that failed at main before the build.
	FailingAtMain []string `json:"failing_at_main,omitempty"`
	// Amendments is the text of the amendment file the build left on its branch.
	Amendments string `json:"amendments,omitempty"`
	// Monitor is true of a monitor's fix, whose Name is the monitor's. Approved is then main's tip when it started.
	Monitor bool `json:"monitor,omitempty"`
	runner
}

// runner is what the kiln run that builds a spec holds of it. next replaces it whole, so no kiln run inherits it.
type runner struct {
	// PID is the process ID of the kiln run that marked the spec building. Once it is gone, the build has halted.
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

// errRefused is the kind of a command kiln refuses for the state a spec is in.
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
)

// may answers nil when the book takes command on the spec name, or why it refuses. It holds spec 4's table:
//
//	state                                  queue                            accept           reject
//	no record                              queues it                        refuses          refuses
//	queued                                 queues it again, from main's tip refuses          refuses
//	building                               refuses                          refuses          refuses
//	stopped                                builds anew from main            refuses          refuses
//	waiting for UAT                        only once its spec on main has   retires it       refuses
//	                                       changed
//	waiting on its branch with amendments  refuses                          folds, and lands drops build, branch
//	landing accepted amendments, queued    refuses                          refuses          refuses
//	landing accepted amendments, building  refuses                          refuses          refuses
//	landing accepted amendments, stopped   refuses                          lands it again   drops build, branch
//	a fix, building                        refuses                          refuses          refuses
//	a fix, stopped                         refuses                          refuses          refuses
//	a fix, waiting on its branch           refuses                          lands it         drops build, branch
//	a fix's landing, queued or building    refuses                          refuses          refuses
//	a fix's landing, stopped               refuses                          lands it again   drops build, branch
//
// A fix is a monitor's, under its name. Its accept retires nothing, since a monitor has no spec.
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
	case r.Accepted && r.State != stopped:
		return fmt.Sprintf("its landing is %s", r.State), nil
	case r.Monitor:
		return r.fixRefusal(command), nil
	case command == queueCommand:
		return r.queueRefusal(changed)
	}

	return r.takeRefusal(command), nil
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

// shownState is where the spec of r stands, in the words kiln status shows.
func shownState(r *record) string {
	switch {
	case r.Monitor && r.Accepted:
		return string(r.State) + ", landing an accepted fix"
	case r.Accepted:
		return string(r.State) + ", landing accepted amendments"
	}

	return string(r.State)
}

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
// queued again afresh. It keeps only its build, sessions and audit, and that it lands accepted amendments. It keeps a
// branch a landing left to put back, too.
func (b *book) accept(name Spec) (record, error) {
	if err := b.may(acceptCommand, name, nil); err != nil {
		return record{}, err
	}

	r := b.find(name)
	was := *r
	if r.Accepted {
		*r = record{Name: name, State: queued, Approved: r.Approved, Build: r.Build, Session: r.Session,
			Verify: r.Verify, OldTests: r.OldTests, Told: r.Told, Audited: r.Audited, Accepted: true, Before: r.Before,
			Monitor: r.Monitor}
	}

	return was, nil
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

// next takes the first queued spec, and marks it building by the kiln run pid. With only, it takes the spec named only,
// or a landing of accepted amendments. A kiln accept leaves such a landing for the kiln run that holds the lock. It
// takes a fix's record only as an accepted landing, for a run started for its name or for landings.
func (b *book) next(only Spec, pid int) (record, bool) {
	for _, r := range b.Specs {
		if r.State == queued && r.runsFor(only) {
			r.State, r.runner = building, runner{PID: pid}
			return *r, true
		}
	}

	return record{}, false
}

// runsFor is whether a kiln run started for only builds the spec of r. Every run builds a landing of accepted
// amendments, and a run started for no spec builds every spec. A run started for landings builds only accepted
// landings, a fix's too. A fix's accepted landing is built otherwise only by a run started for its name.
func (r *record) runsFor(only Spec) bool {
	switch {
	case r.Monitor:
		return r.Accepted && (r.Name == only || only == landings)
	case only == landings:
		return r.Accepted
	}

	return only == "" || r.Name == only || r.Accepted
}

// landings is the only of a run started for the accepted landings alone, as kiln monitor run starts one. No spec's
// name starts with a dot.
const landings Spec = ".landings"

// requeue queues again each spec a kiln run that stopped left building. A build that was building goes on first. It
// queues a fix's accepted landing again, and leaves any other fix.
func (b *book) requeue() {
	for _, s := range b.Specs {
		if s.State == building && s.goesOnAfterKiln() {
			s.State = queued
		}
	}

	slices.SortStableFunc(b.Specs, func(x, y *record) int { return goesOn(y) - goesOn(x) })
}

// goesOnAfterKiln is whether a kiln run that stopped leaves the build of r to go on. A spec's does, and a fix's
// accepted landing. Any other fix is dropped, by kiln monitor run.
func (r *record) goesOnAfterKiln() bool {
	return !r.Monitor || r.Accepted
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
	runLock  = "run.lock"
	mainLock = "main.lock"
	// buildLock is the lock every kiln that drives a build session shares, in kiln's directory under the user's cache
	// directory. The reinstall takes it alone.
	buildLock = "build.lock"
)

// update applies change to the book, holding the store's lock from the read to the write.
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
