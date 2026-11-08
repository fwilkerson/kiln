package proc

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	lsofLimit = 5 * time.Second
	// zombie is SZOMB from XNU's sys/proc.h, which golang.org/x/sys does not export.
	zombie = 5
)

// Process is one row of the process table, as [List] and [Of] read it.
type Process struct {
	PID    int
	Parent int
	Group  int
	// Born is the start in microseconds since the epoch. It tells a reused pid apart.
	Born int64
	// Zombie is true of a process that has exited and that its parent has not yet reaped.
	Zombie bool
}

func fromKinfo(p *unix.KinfoProc) Process {
	t := p.Proc.P_starttime
	return Process{
		PID: int(p.Proc.P_pid), Parent: int(p.Eproc.Ppid), Group: int(p.Eproc.Pgid),
		Born: t.Sec*1_000_000 + int64(t.Usec), Zombie: p.Proc.P_stat == zombie,
	}
}

// List reads every process kiln may see.
func List() ([]Process, error) {
	all, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	procs := make([]Process, 0, len(all))
	for i := range all {
		procs = append(procs, fromKinfo(&all[i]))
	}

	return procs, err
}

// Of reads the process pid.
func Of(pid int) (Process, error) {
	p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return Process{}, err
	}

	return fromKinfo(p), nil
}

// RealUser reads the real user of the process pid, and false if it cannot.
func RealUser(pid int) (uint32, bool) {
	p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, false
	}

	return p.Eproc.Pcred.P_ruid, true
}

// WorkingDirs reads the working directory of each of pids it can, and leaves out the rest.
//
// It asks lsof, since macOS gives another process's working directory to no call golang.org/x/sys exports.
func WorkingDirs(pids []int) map[int]string {
	dirs := map[int]string{}
	if len(pids) == 0 {
		return dirs
	}

	list := make([]string, 0, len(pids))
	for _, pid := range pids {
		list = append(list, strconv.Itoa(pid))
	}

	// lsof exits 1 when a pid it was given has exited, and still prints the rest.
	ctx, cancel := context.WithTimeout(context.Background(), lsofLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/sbin/lsof", "-a", "-d", "cwd", "-p", strings.Join(list, ","), "-Fpn")
	out, _ := cmd.Output()
	pid := 0
	lines := bufio.NewScanner(bytes.NewReader(out))
	for lines.Scan() {
		line := lines.Text()
		switch {
		case strings.HasPrefix(line, "p"):
			pid, _ = strconv.Atoi(line[1:])
		case strings.HasPrefix(line, "n"):
			dirs[pid] = line[1:]
		}
	}

	return dirs
}
