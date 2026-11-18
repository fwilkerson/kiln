package claude

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fwilkerson/kiln/internal/proc"
)

const (
	treePoll   = 50 * time.Millisecond
	treeRounds = 100
)

// tree looks again at each fork. A shell that starts a command and exits at once orphans it before a poll.
type tree struct {
	known map[int]int64
	place string
	began int64
	forks forks
	quit  chan struct{}
	done  chan struct{}
	ended sync.Once
}

func watch(root int, place string) *tree {
	t := newTree(root, place)
	go t.run()
	return t
}

func newTree(root int, place string) *tree {
	if resolved, err := filepath.EvalSymlinks(place); err == nil {
		place = resolved
	}

	t := &tree{known: map[int]int64{}, place: place, forks: watchForks()}
	t.quit, t.done = make(chan struct{}), make(chan struct{})
	if p, err := proc.Of(root); err == nil {
		t.know(root, p.Born)
		t.began = p.Born
	}

	return t
}

func (t *tree) run() {
	defer close(t.done)
	for {
		select {
		case <-t.quit:
			return
		default:
		}

		t.wake()
		t.look()
	}
}

func (t *tree) wake() { t.forks.wait(treePoll) }

func (t *tree) know(pid int, started int64) {
	t.known[pid] = started
	t.forks.follow(pid)
}

func (t *tree) look() map[int]int64 {
	procs, err := proc.List()
	if err != nil {
		return nil
	}

	living := living(procs)
	t.grow(procs, living)
	return t.alive(living)
}

// grow adds each process of procs that joins the tree, until a pass adds none.
func (t *tree) grow(procs []proc.Process, living map[int]int64) {
	for grew := true; grew; {
		grew = false
		for _, p := range procs {
			if t.joins(p, living) {
				t.know(p.PID, p.Born)
				grew = true
			}
		}
	}
}

// alive is the tree's processes that living still holds.
func (t *tree) alive(living map[int]int64) map[int]int64 {
	alive := map[int]int64{}
	for pid, started := range t.known {
		if now, ok := living[pid]; ok && now == started {
			alive[pid] = started
		}
	}

	return alive
}

// living maps each process of procs that is not a zombie to when it was born.
func living(procs []proc.Process) map[int]int64 {
	living := map[int]int64{}
	for _, p := range procs {
		if !p.Zombie {
			living[p.PID] = p.Born
		}
	}

	return living
}

// joins says whether p, not yet known, descends from the tree or belongs to a group it leads.
func (t *tree) joins(p proc.Process, living map[int]int64) bool {
	if p.Zombie || t.knows(p) {
		return false
	}

	return t.parent(p.Parent, living) || t.leads(p.Group, living)
}

// knows says whether the tree holds p, and not another process that reused its pid.
func (t *tree) knows(p proc.Process) bool {
	known, ok := t.known[p.PID]
	return ok && known == p.Born
}

func (t *tree) parent(pid int, living map[int]int64) bool {
	known, ok := t.known[pid]
	now, lives := living[pid]
	return ok && lives && now == known
}

func (t *tree) leads(pgid int, living map[int]int64) bool {
	known, ok := t.known[pgid]
	now, lives := living[pgid]
	return ok && (!lives || now == known)
}

// end stops and kills each process of the tree. A call after the first waits for the first, then does nothing.
func (t *tree) end() { t.ended.Do(t.kill) }

func (t *tree) kill() {
	close(t.quit)
	<-t.done
	t.forks.close()
	stopped := map[int]bool{}
	for range treeRounds {
		more := false
		t.adopt()
		for pid := range t.look() {
			if !stopped[pid] && syscall.Kill(pid, syscall.SIGSTOP) == nil {
				stopped[pid], more = true, true
			}
		}

		if !more {
			break
		}
	}

	for pid := range stopped {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// adopt finds orphans by their place. Once a shell exits before a look and its child calls setsid, nothing leads
// back.
func (t *tree) adopt() {
	procs, err := proc.List()
	if err != nil || t.place == "" {
		return
	}

	orphans := t.orphans(procs)
	for pid, dir := range proc.WorkingDirs(slices.Sorted(maps.Keys(orphans))) {
		if started, ok := orphans[pid]; ok && t.inside(dir) {
			t.know(pid, started)
		}
	}
}

// orphans maps each orphan of procs to when it was born.
func (t *tree) orphans(procs []proc.Process) map[int]int64 {
	uid := uint32(syscall.Getuid())
	orphans := map[int]int64{}
	for _, p := range procs {
		if t.orphan(p, uid) {
			orphans[p.PID] = p.Born
		}
	}

	return orphans
}

// orphan says whether p is an unknown process of uid's that launchd adopted after the tree's root began.
func (t *tree) orphan(p proc.Process, uid uint32) bool {
	switch {
	case p.Parent != 1, p.Zombie, p.Born <= t.began, t.knows(p):
		return false
	}

	owner, ok := proc.RealUser(p.PID)
	return ok && owner == uid
}

// inside says whether dir is the tree's place or below it.
func (t *tree) inside(dir string) bool {
	return dir == t.place || strings.HasPrefix(dir, t.place+"/")
}
