package claude

import (
	"path/filepath"
	"strings"
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

	living := map[int]int64{}
	for _, p := range procs {
		if !p.Zombie {
			living[p.PID] = p.Born
		}
	}

	for grew := true; grew; {
		grew = false
		for _, p := range procs {
			if known, ok := t.known[p.PID]; ok && known == p.Born || p.Zombie {
				continue
			}

			if t.parent(p.Parent, living) || t.leads(p.Group, living) {
				t.know(p.PID, p.Born)
				grew = true
			}
		}
	}

	alive := map[int]int64{}
	for pid, started := range t.known {
		if now, ok := living[pid]; ok && now == started {
			alive[pid] = started
		}
	}

	return alive
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

func (t *tree) end() {
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

	uid := uint32(syscall.Getuid())
	orphans := map[int]int64{}
	var pids []int
	for _, p := range procs {
		if p.Parent != 1 || p.Zombie || p.Born <= t.began {
			continue
		}

		if known, ok := t.known[p.PID]; ok && known == p.Born {
			continue
		}

		if owner, ok := proc.RealUser(p.PID); !ok || owner != uid {
			continue
		}

		orphans[p.PID] = p.Born
		pids = append(pids, p.PID)
	}

	for pid, dir := range proc.WorkingDirs(pids) {
		if started, ok := orphans[pid]; ok && (dir == t.place || strings.HasPrefix(dir, t.place+"/")) {
			t.know(pid, started)
		}
	}
}
