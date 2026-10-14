package agentd

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// waitPID waits for a process to write its pid to path.
func waitPID(t *testing.T, path string) int {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if b, err := os.ReadFile(path); err == nil {
			if pid, perr := strconv.Atoi(strings.TrimSpace(string(b))); perr == nil && pid > 0 {
				return pid
			}
		}
	}
	t.Fatalf("no pid in %s", path)
	return 0
}

// gone: the process does not run (none, or a zombie nothing reaped yet).
func gone(pid int) bool { return len(running([]int{pid})) == 0 }

func waitGone(t *testing.T, pid int, within time.Duration) {
	t.Helper()
	for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if gone(pid) {
			return
		}
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("process %d outlived its attempt", pid)
}

// Review 164 A1: a verify command stays in the attempt's group, so ending
// the attempt ends the command, once it is known to run.
func TestVerifyStaysInTheAttemptGroup(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	pidPath := filepath.Join(out, "check.pid")
	subject := TaskSpec{Text: "# x", Verify: [][]string{{"sh", "-c", "echo $$ > " + pidPath + "; exec sleep 30"}}}
	a := &Attempt{Claim: &Claimed{Role: "verify"}, Spec: TaskSpec{Check: "verify"}, Subject: &subject, Dir: dir, Out: out}
	sp, err := deterministicExecutor(os.Args[0], launcherEnv())(a)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Launch(os.Args[0], sp, func(int, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	check := waitPID(t, pidPath)
	done := make(chan int, 1)
	go func() { done <- p.Wait() }()
	Terminate(p.PGID, time.Second)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the launcher outlived Terminate")
	}
	waitGone(t, check, 3*time.Second)
}

// Review 164 A2: the launcher killed on its own leaves the executor in the
// group. After a restart, when it cannot be shown the group is still the
// attempt's, nothing is killed and the state is unknown; when agentd saw the
// leader die, the group is the attempt's (a process in it started before)
// and is ended. A group that cannot be listed is unknown, never empty.
func TestLauncherKilledAlone(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "executor.pid")
	sp := Spawn{Argv: []string{"sh", "-c", "echo $$ > " + pidPath + "; exec sleep 30"}, Env: launcherEnv()}
	var start string
	p, err := Launch(os.Args[0], sp, func(_ int, s string) error { start = s; return nil })
	if err != nil {
		t.Fatal(err)
	}
	executor := waitPID(t, pidPath)
	t.Cleanup(func() { _ = syscall.Kill(executor, syscall.SIGKILL) })
	time.Sleep(1100 * time.Millisecond) // the executor's start, in seconds, before the leader's death
	if err = syscall.Kill(p.PGID, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	p.Wait()
	died := time.Now()
	if gone(executor) {
		t.Fatal("killing the launcher alone ended the executor: the test shows nothing")
	}

	st, pids := ClearGroup(p.PGID, start, time.Time{}, time.Second)
	if st != GroupUnknown || !slices.Contains(pids, executor) || gone(executor) {
		t.Fatalf("after a restart = %s %v (executor gone: %v)", st, pids, gone(executor))
	}
	pgrep = "/nonexistent/pgrep"
	st, _ = ClearGroup(p.PGID, start, died, time.Second)
	pgrep = "pgrep"
	if st != GroupUnknown || gone(executor) {
		t.Fatalf("a group that cannot be listed = %s", st)
	}
	if _, lerr := groupMembers(p.PGID, 0); lerr != nil {
		t.Fatalf("list: %v", lerr)
	}
	if st, _ = ClearGroup(p.PGID, start, died, time.Second); st != GroupCleared {
		t.Fatalf("the death seen = %s", st)
	}
	waitGone(t, executor, 3*time.Second)
	if st, _ = ClearGroup(p.PGID, start, died, time.Second); st != GroupCleared {
		t.Fatalf("an empty group = %s", st)
	}
}

// A process that ignores TERM is killed, and the group is confirmed empty.
func TestClearGroupKillsWhatIgnoresTerm(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "stubborn.pid")
	sp := Spawn{Argv: []string{"sh", "-c", "trap '' TERM; echo $$ > " + pidPath + "; while :; do sleep 0.2; done"}, Env: launcherEnv()}
	var start string
	p, err := Launch(os.Args[0], sp, func(_ int, s string) error { start = s; return nil })
	if err != nil {
		t.Fatal(err)
	}
	stubborn := waitPID(t, pidPath)
	go p.Wait()
	if st, pids := ClearGroup(p.PGID, start, time.Time{}, 300*time.Millisecond); st != GroupCleared {
		t.Fatalf("= %s %v", st, pids)
	}
	waitGone(t, stubborn, time.Second)
}

// groupMembers tells a failed query from an empty group.
func TestGroupMembers(t *testing.T) {
	if pids, err := groupMembers(999999, 0); err != nil || len(pids) != 0 {
		t.Fatalf("no such group = %v %v", pids, err)
	}
	pgrep = "/nonexistent/pgrep"
	defer func() { pgrep = "pgrep" }()
	if _, err := groupMembers(syscall.Getpgrp(), 0); err == nil {
		t.Fatal("a failed query read as an empty group")
	}
}

// A zombie (dead, not reaped by its parent) is not a running process; where
// nothing reaps orphans (CI's container) the processes a group leaves stay
// zombies.
func TestRunningSkipsZombies(t *testing.T) {
	zombie, err := os.StartProcess("/bin/sh", []string{"sh", "-c", "exit 0"}, &os.ProcAttr{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = zombie.Wait() })
	live, err := os.StartProcess("/bin/sleep", []string{"sleep", "30"}, &os.ProcAttr{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = live.Kill(); _, _ = live.Wait() })
	for deadline := time.Now().Add(5 * time.Second); len(running([]int{zombie.Pid})) != 0; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the exited child never read as not running")
		}
	}
	if got := running([]int{zombie.Pid, live.Pid, 999999}); len(got) != 1 || got[0] != live.Pid {
		t.Fatalf("running = %v, want only %d", got, live.Pid)
	}
}
