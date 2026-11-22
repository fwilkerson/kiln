package kiln

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// lane is the work that one kiln at a time may do in a repository. The specs' lane builds specs, and the monitors'
// lane fixes for monitors. A record belongs to one lane, by its Monitor field.
type lane int

const (
	specsLane lane = iota
	monitorsLane
)

// laneOf is the lane of the record r: monitorsLane for a fix, and specsLane for a spec.
func laneOf(r *record) lane {
	if r.Monitor {
		return monitorsLane
	}

	return specsLane
}

// holds is whether the record r belongs to the lane l.
func (l lane) holds(r *record) bool { return laneOf(r) == l }

// leftBuilding is whether r is a record of the lane l marked building. To the kiln that holds l, as it starts, a kiln
// that stopped left it so.
func (l lane) leftBuilding(r *record) bool { return l.holds(r) && r.State == building }

// String names the lane l, as an error shows it.
func (l lane) String() string {
	if l == monitorsLane {
		return "monitors"
	}

	return "specs"
}

// lockFile is the file whose lock the one kiln at work in the lane l holds.
func (l lane) lockFile() string { return l.String() + ".lock" }

// The kilns that hold a lane's lock, as lockLane writes them into it.
const (
	runHolder     = "kiln fire"
	monitorHolder = "kiln monitor run"
	acceptHolder  = "kiln accept"
	tendHolder    = "kiln tend"
)

// lockLane takes the lock of the lane l, or refuses at once with a *laneHeld while another kiln holds it. holder
// names the kiln that takes it, for a kiln that finds it held.
func (r *Repo) lockLane(l lane, holder string) (unlock func(), err error) {
	unlock, err = r.store.lock(l.lockFile(), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		held, _ := os.ReadFile(filepath.Join(r.store.dir, l.lockFile()))
		return nil, &laneHeld{lane: l, holder: string(held)}
	}

	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(filepath.Join(r.store.dir, l.lockFile()), []byte(holder), 0o600); err != nil {
		unlock()
		return nil, err
	}

	return unlock, nil
}

// laneHeld refuses a kiln whose lane another kiln holds. holder names that kiln, or is "" when it had yet to write
// its name.
type laneHeld struct {
	lane   lane
	holder string
}

func (e *laneHeld) Error() string {
	return fmt.Sprintf("%v: %s is at work in this repository's lane of %s, where one kiln works at a time",
		errRefused, cmpOr(e.holder, "another kiln"), e.lane)
}

// Is makes a laneHeld a refusal, as errRefused names one.
func (e *laneHeld) Is(target error) bool { return target == errRefused }

// commandsLock is the lock a kiln holds while it runs what judges the work, or moves main. Each gate holds it, and
// each try of a landing. So do a monitor's check or gauge at main's tip, and the read of the gauges after a landing.
const commandsLock = "commands.lock"

// commandsRetry is how often a kiln that waits for commandsLock tries it again.
const commandsRetry = 200 * time.Millisecond

// commands is commandsLock, held until release. Only lockCommands makes one. What must run under the lock is a method
// of commands, so it never runs without the lock. Code that holds one passes it on, and never takes the lock again.
type commands struct {
	r       *Repo
	release func()
}

// lockCommands takes commandsLock, for the spec or monitor name. While another kiln holds it, it says so once on out,
// and waits. It answers the context's error once kiln is interrupted. A blocking flock would ignore the interrupt, so
// it tries the lock without blocking.
func (r *Repo) lockCommands(ctx context.Context, name Spec, out io.Writer) (commands, error) {
	for said := false; ; said = true {
		unlock, err := r.store.lock(commandsLock, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return commands{r: r, release: unlock}, nil
		}

		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return commands{}, err
		}

		if !said {
			fmt.Fprintf(out, "%s: waits while another kiln runs the project's commands\n", name)
		}

		select {
		case <-ctx.Done():
			return commands{}, ctx.Err()
		case <-time.After(commandsRetry):
		}
	}
}
