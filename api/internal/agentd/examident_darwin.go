//go:build darwin

package agentd

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// procIdentity is process pid's identity: when it started, to the
// microsecond (kern.proc.pid; review 419 B2: ps's start time is to the
// second, which two processes can share).
func procIdentity(pid int) (string, error) {
	p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", err
	}
	if p.Proc.P_pid != int32(pid) {
		return "", fmt.Errorf("agentd: no process %d", pid)
	}
	t := p.Proc.P_starttime
	return fmt.Sprintf("darwin:%d.%06d", t.Sec, t.Usec), nil
}
