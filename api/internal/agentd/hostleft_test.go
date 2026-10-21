package agentd

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Review 487 A2: a bench command's process group is recorded in the run's
// directory while it runs, and the record goes once the group is gone. The
// test reads the record as a file (pgid, start), as the next holder of the
// host does, so it runs against the code before the fix and fails there.
func TestABenchCommandRecordsItsGroup(t *testing.T) {
	b := &Bench{Root: t.TempDir(), Self: os.Args[0], KillGrace: time.Second}
	// The command copies the record it runs under, which names its group.
	code, timedOut, err := b.launch(bg, Spawn{Argv: []string{"sh", "-c", "cp running seen"}, Env: launcherEnv(), Dir: b.Root},
		time.Minute)
	if err != nil || timedOut || code != 0 {
		t.Fatalf("launch: %d %v %v (no record while it ran)", code, timedOut, err)
	}
	var seen struct {
		PGID  int    `json:"pgid"`
		Start string `json:"start"`
	}
	raw, err := os.ReadFile(filepath.Join(b.Root, "seen"))
	if err != nil || json.Unmarshal(raw, &seen) != nil || seen.PGID <= 1 || seen.Start == "" {
		t.Fatalf("the record while it ran: %s %v", raw, err)
	}
	if _, err = os.Stat(filepath.Join(b.Root, "running")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the record is left after the group went: %v", err)
	}
}

// Review 487 A2: a group still there after its command ended keeps its
// record, and the run launches nothing more.
func TestABenchLaunchesNothingOnceAGroupIsLeft(t *testing.T) {
	b := &Bench{Root: t.TempDir(), Self: os.Args[0], KillGrace: time.Second}
	record := filepath.Join(b.Root, benchRunningName)
	b.groupAlive = func(int) bool { return true }
	if _, _, err := b.launch(bg, Spawn{Argv: []string{"true"}, Env: launcherEnv(), Dir: b.Root}, time.Minute); err == nil ||
		!strings.Contains(err.Error(), "is not gone after it ended") {
		t.Fatalf("a group left: %v", err)
	}
	if _, err := readBenchRunning(record); err != nil {
		t.Fatalf("the record of a group left: %v", err)
	}
	b.groupAlive = nil
	marker := filepath.Join(b.Root, "ran")
	if _, _, err := b.launch(bg, Spawn{Argv: []string{"touch", marker}, Env: launcherEnv(), Dir: b.Root}, time.Minute); err == nil {
		t.Fatal("a command launched after a group was left")
	}
	if _, err := os.Stat(marker); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the command ran: %v", err)
	}
}

// Review 487 A2: the record a killed bench left (written here as a file, as
// Bench.launch writes it) blocks agentd until its group is ended, and agentd
// ends it at start and before each claim (Recover). Only the daemon's API is
// used, so the test runs against the code before the fix and fails there.
func TestABenchRunLeftBlocksTheDaemon(t *testing.T) {
	d := fakeDaemon(t, newFake())
	dir := filepath.Join(d.Root, "bench", "bench-20261021T000000-codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	pidPath := filepath.Join(t.TempDir(), "executor.pid")
	p, err := Launch(os.Args[0], Spawn{Argv: []string{"sh", "-c", "echo $$ > " + pidPath + "; exec sleep 60"}, Env: launcherEnv()},
		func(pgid int, start string) error {
			id, _ := procIdentity(pgid)
			b, err := json.Marshal(map[string]any{"pgid": pgid, "start": start, "id": id})
			if err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, "running"), b, 0o600)
		})
	if err != nil {
		t.Fatal(err)
	}
	go p.Wait()
	executor := waitPID(t, pidPath)
	t.Cleanup(func() { _ = syscall.Kill(executor, syscall.SIGKILL) })
	if id, err := d.Blocked(); err != nil || !strings.Contains(id, "bench run recorded in "+dir) {
		t.Fatalf("blocked %q, %v: agentd would take work beside the bench's executor", id, err)
	}
	if err = d.Recover(bg); err != nil {
		t.Fatal(err)
	}
	waitGone(t, executor, 5*time.Second)
	if _, err = os.Stat(filepath.Join(dir, "running")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the record is left after agentd ended its group: %v", err)
	}
	if id, err := d.Blocked(); err != nil || id != "" {
		t.Fatalf("blocked %q, %v after the group went", id, err)
	}
}

// Review 487 A2: a bench killed while its executor ran leaves the record of
// the executor's group, and whichever holds the host next ends it first: an
// exam or a bench (LeftOnHost) or agentd (Recover); while it cannot be
// ended, or the record does not read, agentd takes no new work (Blocked) and
// LeftOnHost says why.
func TestABenchRunLeftIsEndedByTheNextHolder(t *testing.T) {
	d := fakeDaemon(t, newFake())
	dir := filepath.Join(d.Root, "bench", "bench-20261021T000000-codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	blocked := func(want bool) {
		t.Helper()
		id, err := d.Blocked()
		if err != nil || (id != "") != want || (want && !strings.Contains(id, "the bench run recorded in "+dir)) {
			t.Fatalf("blocked %q, %v; want blocked %v", id, err, want)
		}
	}
	// left launches what a killed bench left: its launcher leads the
	// group, recorded as Bench.launch records it, and its executor runs on.
	left := func() int {
		t.Helper()
		pidPath := filepath.Join(t.TempDir(), "executor.pid")
		p, err := Launch(os.Args[0], Spawn{Argv: []string{"sh", "-c", "echo $$ > " + pidPath + "; exec sleep 60"}, Env: launcherEnv()},
			func(pgid int, start string) error {
				id, _ := procIdentity(pgid)
				return keepBenchRunning(dir, benchRunning{PGID: pgid, Start: start, ID: id})
			})
		if err != nil {
			t.Fatal(err)
		}
		go p.Wait()
		executor := waitPID(t, pidPath)
		t.Cleanup(func() { _ = syscall.Kill(executor, syscall.SIGKILL) })
		return executor
	}
	recordGone := func() bool {
		_, err := os.Stat(filepath.Join(dir, benchRunningName))
		return errors.Is(err, fs.ErrNotExist)
	}

	// An exam or a bench that takes the host ends it before anything runs.
	executor := left()
	blocked(true)
	if err := LeftOnHost(d.Root, time.Second); err != nil {
		t.Fatal(err)
	}
	waitGone(t, executor, 5*time.Second)
	if !recordGone() {
		t.Fatal("the record is left after its group was ended")
	}
	blocked(false)

	// So does agentd, at start and before each claim.
	executor = left()
	if err := d.Recover(bg); err != nil {
		t.Fatal(err)
	}
	waitGone(t, executor, 5*time.Second)
	if !recordGone() {
		t.Fatal("the record is left after agentd ended its group")
	}
	blocked(false)

	// A record that does not read names a run whose group cannot be found:
	// nothing runs, and agentd takes nothing.
	if err := os.WriteFile(filepath.Join(dir, benchRunningName), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LeftOnHost(d.Root, time.Second); err == nil || !strings.Contains(err.Error(), "not a bench run's record") {
		t.Fatalf("a record that does not read: %v", err)
	}
	blocked(true)
}

// Review 487 A2: an attempt of agentd's registry whose group still runs
// (agentd killed while it ran) is agentd's to end and hand over: an exam or a
// bench that takes the host runs nothing beside it, and touches neither it
// nor its entry; once its group is gone it is no obstacle, though its entry
// waits for agentd.
func TestOnlyAgentdEndsItsAttemptsLeft(t *testing.T) {
	d := fakeDaemon(t, newFake())
	pidPath := filepath.Join(t.TempDir(), "executor.pid")
	var pgid int
	var start string
	p, err := Launch(os.Args[0], Spawn{Argv: []string{"sh", "-c", "echo $$ > " + pidPath + "; exec sleep 60"}, Env: launcherEnv()},
		func(g int, s string) error { pgid, start = g, s; return nil })
	if err != nil {
		t.Fatal(err)
	}
	go p.Wait()
	executor := waitPID(t, pidPath)
	t.Cleanup(func() { _ = syscall.Kill(executor, syscall.SIGKILL) })
	const attempt = "a_01M5H0000000000000000000AA"
	reg, err := OpenRegistry(RegistryPath(d.Root)) // where agentd keeps it (cmd/agentd)
	if err != nil {
		t.Fatal(err)
	}
	if err = reg.Put(Entry{AttemptID: attempt, TaskID: "t_01M5H0000000000000000000AA", AttemptNo: 1, Phase: "running",
		PGID: pgid, Start: start, Workdir: filepath.Join(d.Root, "tasks", "t_x", "a1")}); err != nil {
		t.Fatal(err)
	}
	if err = LeftOnHost(d.Root, time.Second); err == nil || !strings.Contains(err.Error(), "attempt "+attempt+" of agentd's registry") {
		t.Fatalf("an attempt whose group runs: %v", err)
	}
	if gone(executor) {
		t.Fatal("the attempt's executor was ended by another than agentd")
	}
	if err = syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	waitGone(t, executor, 5*time.Second)
	for deadline := time.Now().Add(5 * time.Second); GroupAlive(pgid); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the group outlived KILL")
		}
	}
	if err = LeftOnHost(d.Root, time.Second); err != nil {
		t.Fatalf("an attempt whose group is gone: %v", err)
	}
	entries, err := reg.Load()
	if err != nil || entries[attempt].Phase != "running" {
		b, _ := json.Marshal(entries)
		t.Fatalf("the registry was changed: %s %v", b, err)
	}
}
