package landinggit

import (
	"os/exec"
	"syscall"
	"time"
)

// reapDelay bounds how long a cancelled Git call waits for its pipes after
// its process group is killed (CUT §11.4 B1, review 660 B-01): a helper Git
// started (an HTTP transport, a credential helper) cannot hold the call open
// past its deadline.
const reapDelay = 5 * time.Second

// killGroupOnCancel runs cmd in a process group of its own and, when its
// context ends, kills the whole group, not only git itself; Wait returns
// within reapDelay of that.
func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = reapDelay
}
