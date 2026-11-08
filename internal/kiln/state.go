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
)

// record is what kiln keeps of one approved spec.
type record struct {
	Name  string `json:"name"`
	State state  `json:"state"`
	// Approved is main's tip when the developer approved the spec.
	Approved string `json:"approved"`
	// Build is the ID of this approval's build, which names it in the ledger.
	Build string `json:"build,omitempty"`
	// Session is the build's Claude Code session, once it has one.
	Session string `json:"session,omitempty"`
	// Rounds counts the failed gates sent back to the session.
	Rounds int `json:"rounds,omitempty"`
	// Audited is true once the audit has answered.
	Audited bool `json:"audited,omitempty"`
	// Accepted is a spec the developer accepted while it waited on its branch. kiln lands it as a build does, then
	// retires it.
	Accepted bool `json:"accepted,omitempty"`
	// Why says why the build stopped, with the gate's output.
	Why string `json:"why,omitempty"`
	// FailingAtMain names the Go tests that failed at main before the build.
	FailingAtMain []string `json:"failing_at_main,omitempty"`
	// Amendments is the text of the amendment file the build left on its branch.
	Amendments string `json:"amendments,omitempty"`
	// Paused is when a usage limit that paused the build resets.
	Paused time.Time `json:"paused,omitzero"`
}

// book is every approved spec kiln has not retired, in the order they were approved. It keeps what the gate found
// at main.
type book struct {
	Specs []*record `json:"specs"`
	// Main holds the gate's checks at main, by the commit and the commands they ran.
	Main map[string][]check `json:"main,omitempty"`
}

// errRefused is the kind of a command kiln refuses for the state a spec is in.
var errRefused = errors.New("refused")

func (b *book) find(name string) *record {
	if i := slices.IndexFunc(b.Specs, func(r *record) bool { return r.Name == name }); i >= 0 {
		return b.Specs[i]
	}

	return nil
}

// approve queues a new build of name from the spec at main's tip, approved. It answers whether a build of an earlier
// approval left a branch to drop. It also answers the ID of an earlier build this approval drops before it ended. A
// spec that is building refuses: approve it again once its build ends.
func (b *book) approve(name, approved string) (drop bool, dropped string, err error) {
	r := b.find(name)
	switch {
	case r == nil:
		b.Specs = append(b.Specs, &record{Name: name, State: queued, Approved: approved, Build: newBuild()})
		return false, "", nil
	case r.State == building:
		return false, "", fmt.Errorf("%w: %s is building; approve it again once its build ends", errRefused, name)
	}

	// A queued build with a session was interrupted, and an amended one waits: neither has ended.
	if r.State == amended || r.State == queued && r.Session != "" {
		dropped = r.Build
	}

	drop = r.State != queued
	*r = record{Name: name, State: queued, Approved: approved, Build: newBuild()}
	return drop, dropped, nil
}

// accept returns the record of a spec waiting for the developer's acceptance.
func (b *book) accept(name string) (*record, error) {
	r := b.find(name)
	switch {
	case r == nil:
		return nil, fmt.Errorf("%w: kiln holds no approved spec named %s", errRefused, name)
	case r.State != landed && r.State != amended:
		return nil, fmt.Errorf("%w: %s is %s, and only a spec waiting for UAT is accepted", errRefused, name, r.State)
	}

	return r, nil
}

func (b *book) retire(name string) {
	b.Specs = slices.DeleteFunc(b.Specs, func(r *record) bool { return r.Name == name })
}

// next takes the first queued spec, or the one named only, and marks it building.
func (b *book) next(only string) (record, bool) {
	for _, r := range b.Specs {
		if r.State == queued && (only == "" || r.Name == only) {
			r.State = building
			return *r, true
		}
	}

	return record{}, false
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
)

// update applies change to the book, holding the store's lock from the read to the write.
func (s store) update(change func(*book) error) error {
	unlock, err := s.lock(lockFile, true)
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
func (s store) set(name string, change func(*record)) error {
	return s.update(func(b *book) error {
		if r := b.find(name); r != nil {
			change(r)
		}

		return nil
	})
}

// lock takes the lock file name. With wait false it refuses at once a lock another kiln holds.
func (s store) lock(name string, wait bool) (unlock func(), err error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(filepath.Join(s.dir, name), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}

	how := syscall.LOCK_EX
	if !wait {
		how |= syscall.LOCK_NB
	}

	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		f.Close()
		return nil, err
	}

	return func() { f.Close() }, nil
}
