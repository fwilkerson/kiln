package claude

import (
	"time"

	"golang.org/x/sys/unix"
)

type forks struct {
	kq int
	ok bool
}

func watchForks() forks {
	kq, err := unix.Kqueue()
	if err != nil {
		return forks{}
	}

	return forks{kq: kq, ok: true}
}

func (f forks) follow(pid int) {
	if !f.ok {
		return
	}

	var ev unix.Kevent_t
	unix.SetKevent(&ev, pid, unix.EVFILT_PROC, unix.EV_ADD|unix.EV_CLEAR)
	ev.Fflags = unix.NOTE_FORK
	_, _ = unix.Kevent(f.kq, []unix.Kevent_t{ev}, nil, nil)
}

func (f forks) wait(limit time.Duration) {
	if !f.ok {
		time.Sleep(limit)
		return
	}

	timeout := unix.NsecToTimespec(limit.Nanoseconds())
	_, _ = unix.Kevent(f.kq, nil, make([]unix.Kevent_t, 16), &timeout)
}

func (f forks) close() {
	if f.ok {
		_ = unix.Close(f.kq)
	}
}
