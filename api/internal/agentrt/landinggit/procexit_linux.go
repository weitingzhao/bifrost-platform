package landinggit

import (
	"errors"
	"syscall"

	"golang.org/x/sys/unix"
)

// waitExit blocks until process pid has exited, without reaping it; false
// when that cannot be awaited.
func waitExit(pid int) bool {
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if !errors.Is(err, syscall.EINTR) {
			return err == nil
		}
	}
}
