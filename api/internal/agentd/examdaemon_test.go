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

// Review 417 A1, B2, 419 B2, C2, C3: an exam sitting cut short while its
// executor ran (the exam process killed, the launcher and claude left) is
// agentd's to end too: at start and before each claim it ends the group the
// run's record names while its leader is the recorded one (by its identity,
// not ps's start time), then the run's directories and the record, and
// takes no new work while a group is left that cannot be shown to be the
// run's, a record does not read, or the papers' directory cannot be listed.
// A record whose process id another process has now names a group that is
// gone, even when that process started in the same second: nothing is
// killed.
func TestExamRunLeftBlocksTheDaemon(t *testing.T) {
	d := fakeDaemon(t, newFake())
	dir := filepath.Join(d.Root, "exam", "chief_of_staff-claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	const task = "t_01M5BD0F29Z47H2FH8RN5V2V6D"
	taskDir := filepath.Join(d.Root, "tasks", task, "a1.out")
	record := func(pgid int, start, id string) {
		t.Helper()
		if err := os.MkdirAll(taskDir, 0o700); err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(examSitting{Task: task, PGID: pgid, Start: start, ID: id})
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
	ended := func() bool {
		_, rerr := os.Stat(filepath.Join(dir, "sitting"))
		_, derr := os.Stat(filepath.Dir(taskDir))
		return os.IsNotExist(rerr) && os.IsNotExist(derr)
	}
	identity := func(pid int) string {
		t.Helper()
		id, err := procIdentity(pid)
		if err != nil {
			t.Fatal(err)
		}
		return id
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
		func(pgid int, start string) error { record(pgid, start, identity(pgid)); return nil })
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
	if !ended() {
		t.Fatal("the run's record or directories are left after its group was ended")
	}
	blocked(false)

	// Its leader gone, its executor left: not known to be the run's, so
	// nothing is killed and no new work is taken, until it is gone.
	pidPath = filepath.Join(t.TempDir(), "executor.pid")
	if p, err = Launch(os.Args[0], Spawn{Argv: []string{"sh", "-c", "echo $$ > " + pidPath + "; exec sleep 60"}, Env: launcherEnv()},
		func(pgid int, start string) error { record(pgid, start, identity(pgid)); return nil }); err != nil {
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
	if !ended() {
		t.Fatal("the run's record or directories are left after its group went")
	}
	blocked(false)

	// The record's process id is another process's now, which started in the
	// same second as the recorded leader (ps's start times equal): its
	// identity is another, so the run's group is gone; it is not touched.
	other := exec.Command("sleep", "60")
	other.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err = other.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = other.Wait() }()
	t.Cleanup(func() { _ = other.Process.Kill() })
	start, err := StartTime(other.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	record(other.Process.Pid, start, "linux:another-boot/1")
	if err = d.Recover(bg); err != nil {
		t.Fatal(err)
	}
	if gone(other.Process.Pid) || !ended() {
		t.Fatalf("a reused process id: other gone %v, record and directories ended %v", gone(other.Process.Pid), ended())
	}
	blocked(false)

	// A record without the leader's identity (a system that gives none)
	// never ends a group by ps's start time alone: while any of it runs, the
	// host is blocked and nothing is signalled.
	record(other.Process.Pid, start, "")
	if err = d.Recover(bg); err != nil {
		t.Fatal(err)
	}
	if gone(other.Process.Pid) || recorded().PGID != other.Process.Pid {
		t.Fatalf("a record without identity: other gone %v, record %+v", gone(other.Process.Pid), recorded())
	}
	blocked(true)
	if err = other.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	waitGone(t, other.Process.Pid, 5*time.Second)
	if err = d.Recover(bg); err != nil {
		t.Fatal(err)
	}
	if !ended() {
		t.Fatal("a record without identity is left after its group went")
	}
	blocked(false)

	// The papers' directory that cannot be listed is not "nothing left".
	if os.Geteuid() != 0 {
		exam := filepath.Join(d.Root, "exam")
		if err = os.Chmod(exam, 0); err != nil {
			t.Fatal(err)
		}
		if id, berr := d.Blocked(); berr != nil || id != "the exam run recorded in "+exam {
			t.Fatalf("an unlistable exam directory: blocked %q, %v", id, berr)
		}
		if err = os.Chmod(exam, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	// A record that does not read blocks the host until someone looks.
	if err = os.WriteFile(filepath.Join(dir, "sitting"), []byte("t_x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = d.Recover(bg); err != nil {
		t.Fatal(err)
	}
	blocked(true)
}

// Review 419 B1: the answer streamResultReport skipped is the last result
// event, read whole with its fields wherever they are: one over 1 MiB that is
// not an error. An error result, a result field nested in another event, a
// later result, or an event too long to read is not.
func TestResultTooLong(t *testing.T) {
	big := strings.Repeat("x", maxBody+1)
	pad := strings.Repeat("y", 1024)
	for _, c := range []struct {
		name  string
		lines []string
		want  bool
	}{
		{"the result's fields in the middle", []string{`{"result":"` + big + `","type":"result","is_error":false,"permission_denials":[{"tool_input":{"data":"` + pad + `"}}]}`}, true},
		{"an error result", []string{`{"type":"result","result":"` + big + `","is_error":true,"errors":["` + pad + `"]}`}, false},
		{"a result field in a tool's input", []string{`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Example","input":{"type":"result","result":"` + big + `"}}]}}`}, false},
		{"a short result after it", []string{`{"type":"result","result":"` + big + `"}`, `{"type":"result","result":"Fine."}`}, false},
		{"a short error after it", []string{`{"type":"result","result":"` + big + `"}`, `{"type":"result","result":"Failed","is_error":true}`}, false},
		{"over what is read", []string{`{"type":"result","result":"` + strings.Repeat("x", maxExamEvent) + `"}`}, false},
		{"no result", []string{`{"type":"system","subtype":"init"}`}, false},
	} {
		log := filepath.Join(t.TempDir(), "log.txt")
		if err := os.WriteFile(log, []byte(strings.Join(c.lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := resultTooLong(log); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}
