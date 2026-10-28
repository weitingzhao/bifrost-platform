package landinggit

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// gone waits for pid to be gone (a killed orphan is reaped by init).
func gone(pid int) bool {
	for i := 0; i < 200; i++ {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// Review 662 B-03: git exiting first, with success or failure, while a
// process it started still holds its pipes; or the deadline arriving while
// both run. The call returns promptly and the leftover is killed, not left
// to outlive it.
func TestRunGroupKillsWhatGitLeaves(t *testing.T) {
	for name, c := range map[string]struct {
		script  string
		timeout time.Duration
	}{
		"git exits ok":     {`sleep 30 & echo $!; exit 0`, time.Minute},
		"git exits failed": {`sleep 30 & echo $!; exit 3`, time.Minute},
		"deadline":         {`sleep 30 & echo $!; sleep 30`, 300 * time.Millisecond},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
			defer cancel()
			cmd := exec.Command("/bin/sh", "-c", c.script)
			out := &limitedBuffer{max: 1 << 10}
			cmd.Stdout = out
			start := time.Now()
			_ = runGroup(ctx, cmd)
			if elapsed := time.Since(start); elapsed > 2*time.Second {
				t.Fatal("held open for", elapsed)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(out.String()))
			if err != nil {
				t.Fatal("no child pid", out.String())
			}
			if !gone(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
				t.Fatal("a child outlived the call", pid)
			}
		})
	}
}

// A context already ended starts nothing.
func TestRunGroupStartsNothingPastItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := groupStarts.Load()
	if err := runGroup(ctx, exec.Command("/bin/sh", "-c", "exit 0")); err == nil || groupStarts.Load() != before {
		t.Fatal("started past its context", err)
	}
}

func TestLimitedBuffer(t *testing.T) {
	b := &limitedBuffer{max: 4}
	if n, _ := b.Write([]byte("abcdef")); n != 6 || b.String() != "abcd" {
		t.Fatal(n, b.String())
	}
}

// Review 663 R-02: git's exit cannot be awaited: the call is aborted at
// once, the whole group killed, and it says so; nothing is left running.
func TestRunGroupAbortsWhenTheExitCannotBeAwaited(t *testing.T) {
	defer func(f func(int) bool) { waitExitFn = f }(waitExitFn)
	// It fails after git has started its child, so the child's id is read.
	waitExitFn = func(int) bool { time.Sleep(300 * time.Millisecond); return false }
	cmd := exec.Command("/bin/sh", "-c", `sleep 30 & echo $!; sleep 30`)
	out := &limitedBuffer{max: 1 << 10}
	cmd.Stdout = out
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	err := runGroup(ctx, cmd)
	if elapsed := time.Since(start); err == nil || elapsed > 2*time.Second {
		t.Fatal("not aborted", err, elapsed)
	}
	pid, e := strconv.Atoi(strings.TrimSpace(out.String()))
	if e != nil {
		t.Fatal("no child pid", out.String())
	}
	if !gone(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("a child outlived the aborted call", pid)
	}
}
