package landinggit

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// reapDelay bounds how long a Git call waits for its pipes once its process
// group is killed (CUT §11.4 B1, reviews 660 B-01, 662 B-03): a helper Git
// started (an HTTP transport, a credential helper) cannot hold the call
// open. PushMain keeps it inside the push's deadline.
const reapDelay = 5 * time.Second

// groupStarts counts processes runGroup started (tests: a call past its
// deadline starts none).
var groupStarts atomic.Int64

// errNotStarted: runGroup started nothing (ctx already ended, or git could
// not be started).
var errNotStarted = errors.New("git not started")

// errExitUnobserved: git's exit could not be awaited without reaping it;
// the call was aborted, its whole group killed (review 663 R-02).
var errExitUnobserved = errors.New("git exit not observable: aborted")

// waitExitFn is waitExit; tests replace it to make it fail.
var waitExitFn = waitExit

// runGroup runs cmd in a process group of its own and returns when it is
// done with the whole group, not only git itself (review 662 B-03). Git's
// exit is awaited without reaping it (waitExit), so its unreaped exit keeps
// the group's id from being reused; then every process git left in the
// group is killed, also when git exited first, and only then are the pipes
// drained (at most reapDelay) and git reaped. When ctx ends first the group
// is killed at once. cmd must not have been built with a context.
func runGroup(ctx context.Context, cmd *exec.Cmd) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", errNotStarted, err)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = reapDelay
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%w: %w", errNotStarted, err)
	}
	groupStarts.Add(1)
	pid := cmd.Process.Pid
	exited := make(chan bool, 1)
	safego.Go("landinggit-wait-exit", func() { exited <- waitExitFn(pid) })
	var known bool
	select {
	case known = <-exited:
	case <-ctx.Done():
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		known = <-exited
	}
	// Git is not reaped yet (only cmd.Wait reaps): its id still names this
	// group, so the kill reaches its leftovers and nothing else. When the
	// exit could not be awaited the call is aborted the same way, the whole
	// group killed while git is still unreaped (review 663 R-02).
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	err := cmd.Wait()
	if !known {
		return errExitUnobserved
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// limitedBuffer keeps the first max bytes written to it and drops the rest:
// a remote cannot grow the output of a Git call without bound.
type limitedBuffer struct {
	b   []byte
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := l.max - len(l.b); room > 0 {
		l.b = append(l.b, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

func (l *limitedBuffer) String() string { return string(l.b) }
