package landinggit

import (
	"errors"
	"syscall"
)

// waitExit blocks until process pid has exited, without reaping it (a
// kqueue NOTE_EXIT: Go has no waitid WNOWAIT on macOS); false when that
// cannot be awaited. Development hosts only: production runs Linux.
func waitExit(pid int) bool {
	kq, err := syscall.Kqueue()
	if err != nil {
		return false
	}
	defer syscall.Close(kq)
	ev := syscall.Kevent_t{Ident: uint64(pid), Filter: syscall.EVFILT_PROC, Flags: syscall.EV_ADD | syscall.EV_ONESHOT, Fflags: syscall.NOTE_EXIT}
	out := make([]syscall.Kevent_t, 1)
	for {
		n, err := syscall.Kevent(kq, []syscall.Kevent_t{ev}, out, nil)
		switch {
		case errors.Is(err, syscall.EINTR):
			continue
		case err == nil && n == 1, errors.Is(err, syscall.ESRCH):
			// Exited (ESRCH: before the filter was added); not reaped,
			// since only cmd.Wait reaps.
			return true
		default:
			return false
		}
	}
}
