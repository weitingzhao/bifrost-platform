package agentd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Review 417 A1, B2: an exam sitting cut short while its executor ran (the
// exam process killed, the launcher and claude left) is agentd's to end too:
// at start and before each claim it ends the group the run's record names
// while its leader is the recorded one, and takes no new work while a group
// is left that cannot be shown to be the run's, or a record does not read. A
// record whose process id another process has now (it started at another
// time) names a group that is gone: nothing is killed, and the record goes
// on naming the task alone.
func TestExamRunLeftBlocksTheDaemon(t *testing.T) {
	d := fakeDaemon(t, newFake())
	dir := filepath.Join(d.Root, "exam", "chief_of_staff-claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	record := func(pgid int, start string) {
		t.Helper()
		b, err := json.Marshal(examSitting{Task: "t_01M5BD0F29Z47H2FH8RN5V2V6D", PGID: pgid, Start: start})
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "sitting"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	recorded := func() examSitting {
		t.Helper()
		s, err := readSitting(filepath.Join(dir, "sitting"))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	blocked := func(want bool) {
		t.Helper()
		id, err := d.Blocked()
		if err != nil || (id != "") != want || (want && !strings.Contains(id, "the exam run recorded in "+dir)) {
			t.Fatalf("blocked %q, %v; want blocked %v", id, err, want)
		}
	}

	// The run's launcher still leads its group: agentd ends the group.
	pidPath := filepath.Join(t.TempDir(), "executor.pid")
	p, err := Launch(os.Args[0], Spawn{Argv: []string{"sh", "-c", "echo $$ > " + pidPath + "; exec sleep 60"}, Env: launcherEnv()},
		func(pgid int, start string) error { record(pgid, start); return nil })
	if err != nil {
		t.Fatal(err)
	}
	go p.Wait()
	executor := waitPID(t, pidPath)
	t.Cleanup(func() { _ = syscall.Kill(executor, syscall.SIGKILL) })
	blocked(true)
	if err = d.Recover(bg); err != nil {
		t.Fatal(err)
	}
	waitGone(t, executor, 5*time.Second)
	if s := recorded(); s.PGID != 0 || s.Start != "" || s.Task == "" {
		t.Fatalf("the record after its group was ended: %+v", s)
	}
	blocked(false)

	// Its leader gone, its executor left: not known to be the run's, so
	// nothing is killed and no new work is taken, until it is gone.
	pidPath = filepath.Join(t.TempDir(), "executor.pid")
	if p, err = Launch(os.Args[0], Spawn{Argv: []string{"sh", "-c", "echo $$ > " + pidPath + "; exec sleep 60"}, Env: launcherEnv()},
		func(pgid int, start string) error { record(pgid, start); return nil }); err != nil {
		t.Fatal(err)
	}
	executor = waitPID(t, pidPath)
	t.Cleanup(func() { _ = syscall.Kill(executor, syscall.SIGKILL) })
	if err = syscall.Kill(p.PGID, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	p.Wait()
	if err = d.Recover(bg); err != nil {
		t.Fatal(err)
	}
	if gone(executor) || recorded().PGID != p.PGID {
		t.Fatalf("a group not known to be the run's: executor gone %v, record %+v", gone(executor), recorded())
	}
	blocked(true)
	if err = syscall.Kill(executor, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	waitGone(t, executor, 5*time.Second)
	if err = d.Recover(bg); err != nil {
		t.Fatal(err)
	}
	blocked(false)

	// The record's process id is another process's now, started at another
	// time: the run's group is gone; that process is not touched.
	other := exec.Command("sleep", "60")
	other.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err = other.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = other.Wait() }()
	t.Cleanup(func() { _ = other.Process.Kill() })
	record(other.Process.Pid, "Thu Jan 1 00:00:00 1970")
	if err = d.Recover(bg); err != nil {
		t.Fatal(err)
	}
	if gone(other.Process.Pid) || recorded().PGID != 0 {
		t.Fatalf("a reused process id: other gone %v, record %+v", gone(other.Process.Pid), recorded())
	}
	blocked(false)

	// A record that does not read blocks the host until someone looks.
	if err = os.WriteFile(filepath.Join(dir, "sitting"), []byte("t_x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = d.Recover(bg); err != nil {
		t.Fatal(err)
	}
	blocked(true)
}
