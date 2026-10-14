package agentd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The test binary is also the launcher: Launch starts it with LaunchArg and
// this environment variable.
func TestMain(m *testing.M) {
	if os.Getenv("AGENTD_TEST_LAUNCHER") == "1" && len(os.Args) > 2 && os.Args[1] == LaunchArg {
		os.Exit(RunLauncher(os.Args[3:]))
	}
	if os.Getenv("AGENTD_TEST_LAUNCHER") == "1" && len(os.Args) > 1 && os.Args[1] == "verify" {
		os.Exit(RunVerify(os.Args[2:]))
	}
	os.Exit(m.Run())
}

func launcherEnv() []string {
	return append(os.Environ(), "AGENTD_TEST_LAUNCHER=1")
}

func TestHostLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentd.lock")
	first, err := LockHost(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = LockHost(path); !errors.Is(err, ErrHeld) {
		t.Fatalf("a second agentd took the lock: %v", err)
	}
	if err = first.Release(); err != nil {
		t.Fatal(err)
	}
	again, err := LockHost(path)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	_ = again.Release()
}

func TestRegistry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	r, err := OpenRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if m, loadErr := r.Load(); loadErr != nil || len(m) != 0 {
		t.Fatalf("a new registry = %v, %v", m, loadErr)
	}
	for _, e := range []Entry{{AttemptID: "a_2", PGID: 22, Phase: "running"}, {AttemptID: "a_1", PGID: 11, Phase: "computed"}} {
		if err = r.Put(e); err != nil {
			t.Fatal(err)
		}
	}
	if err = r.Put(Entry{AttemptID: "a_2", PGID: 22, Phase: "running", CheckpointSeq: 3}); err != nil {
		t.Fatal(err)
	}
	if err = r.Delete("a_1"); err != nil {
		t.Fatal(err)
	}
	again, _ := OpenRegistry(path)
	m, err := again.Load()
	if err != nil || len(m) != 1 || m["a_2"].CheckpointSeq != 3 {
		t.Fatalf("reopened = %v, %v", m, err)
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatalf("left behind: %v", files)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
}

// R1: the executor runs only after its group is recorded; a group whose
// record failed never runs it.
func TestLaunchRecordsBeforeRelease(t *testing.T) {
	dir := t.TempDir()
	mark := filepath.Join(dir, "ran")
	sp := Spawn{Argv: []string{"touch", mark}, Env: launcherEnv(), Dir: dir}
	var seen bool
	p, err := Launch(os.Args[0], sp, func(pgid int, start string) error {
		_, statErr := os.Stat(mark)
		seen = errors.Is(statErr, os.ErrNotExist) && pgid > 1 && start != ""
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if code := p.Wait(); code != 0 || !seen {
		t.Fatalf("exit %d; the executor had not run when the group was recorded: %v", code, seen)
	}
	if _, err = os.Stat(mark); err != nil {
		t.Fatal("the executor did not run after its release")
	}

	refused := filepath.Join(dir, "refused")
	_, err = Launch(os.Args[0], Spawn{Argv: []string{"touch", refused}, Env: launcherEnv(), Dir: dir},
		func(int, string) error { return errors.New("the disk is full") })
	if err == nil {
		t.Fatal("a failed record still launched")
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(refused); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the executor ran though its group was never recorded")
	}
}

// A launcher whose agentd is gone before the release byte exits without
// running the executor.
func TestLauncherWithoutReleaseExits(t *testing.T) {
	dir := t.TempDir()
	mark := filepath.Join(dir, "ran")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], LaunchArg, "--", "touch", mark)
	cmd.Env = launcherEnv()
	cmd.ExtraFiles = []*os.File{r}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	_ = w.Close() // agentd died before it released the group
	if err := cmd.Wait(); err == nil || cmd.ProcessState.ExitCode() != 125 {
		t.Fatalf("launcher = %v", err)
	}
	if _, err := os.Stat(mark); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the executor ran without its release")
	}
}

// The group is the executor and everything it starts: terminating it ends
// them all, and Alive tells the recorded group from a reused id.
func TestTerminateEndsTheGroup(t *testing.T) {
	sp := Spawn{Argv: []string{"sh", "-c", "sleep 60 & sleep 60"}, Env: launcherEnv()}
	var start string
	p, err := Launch(os.Args[0], sp, func(_ int, s string) error { start = s; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if !Alive(p.PGID, start) {
		t.Fatal("a running group is not alive")
	}
	if Alive(p.PGID, start+" (another)") {
		t.Fatal("a group with another start time counts as ours")
	}
	done := make(chan int, 1)
	go func() { done <- p.Wait() }()
	Terminate(p.PGID, 2*time.Second)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the leader outlived Terminate")
	}
	time.Sleep(100 * time.Millisecond)
	if Alive(p.PGID, start) {
		t.Fatal("the group outlived Terminate")
	}
	out, _ := exec.Command("pgrep", "-g", strconv.Itoa(p.PGID)).Output()
	if len(strings.TrimSpace(string(out))) > 0 {
		t.Fatalf("left in the group: %s", out)
	}
}

// Review 163 A9: the host lock is in one place whatever root agentd uses.
func TestHostLockPathIsFixed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := HostLockPath()
	if err != nil {
		t.Fatal(err)
	}
	first, err := LockHost(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Release() }()
	again, err := HostLockPath()
	if err != nil || again != path {
		t.Fatalf("the lock moved: %s %v", again, err)
	}
	if _, err := LockHost(again); !errors.Is(err, ErrHeld) {
		t.Fatalf("a second agentd = %v", err)
	}
}
