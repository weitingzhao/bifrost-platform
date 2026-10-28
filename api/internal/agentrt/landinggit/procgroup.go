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
	safego.Go("landinggit-wait-exit", func() { exited <- waitExit(pid) })
	var known bool
	select {
	case known = <-exited:
	case <-ctx.Done():
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		known = <-exited
	}
	if known {
		// Git is not reaped yet (only cmd.Wait reaps): its id still names
		// this group, so the kill reaches its leftovers and nothing else.
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	} else {
		// The exit could not be awaited (it never happens on Linux or
		// macOS): fall back to killing the group when ctx ends.
		stop := make(chan struct{})
		defer close(stop)
		safego.Go("landinggit-kill-on-cancel", func() {
			select {
			case <-ctx.Done():
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			case <-stop:
			}
		})
	}
	err := cmd.Wait()
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
