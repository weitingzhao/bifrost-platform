package agentd

import (
	"os"
	"path/filepath"
	"testing"
)

// Several attempts at once (W-31 2.4 slice ③): Recover, which runs between
// claims, ends the groups a previous process left and leaves the attempts
// this process runs to their Run, the running and the handing over alike.
func TestRecoverLeavesWhatThisProcessRuns(t *testing.T) {
	f := newFake()
	d := fakeDaemon(t, f)
	running, ra := entryFor(t, d, "a_run", "running")
	pidPath := filepath.Join(ra.Out, "executor.pid")
	p, err := Launch(os.Args[0], Spawn{Argv: []string{"sh", "-c", "echo $$ > " + pidPath + "; exec sleep 60"}, Env: launcherEnv()},
		func(pgid int, start string) error { running.PGID, running.Start = pgid, start; return nil })
	if err != nil {
		t.Fatal(err)
	}
	go p.Wait()
	executor := waitPID(t, pidPath)
	running.Group = ""
	if err = d.Registry.Put(running); err != nil {
		t.Fatal(err)
	}
	entryFor(t, d, "a_exited", "exited")
	d.Hold("a_run")
	d.Hold("a_exited")
	if d.Running() != 2 {
		t.Fatalf("running = %d", d.Running())
	}
	if err = d.Recover(bg); err != nil {
		t.Fatal(err)
	}
	if gone(executor) {
		t.Fatal("Recover ended the group of an attempt this process runs")
	}
	if e, ok := registered(t, d, "a_run"); !ok || e.Phase != "running" {
		t.Fatalf("the running attempt's entry = %+v %v", e, ok)
	}
	if calls := f.log(); len(calls) != 0 {
		t.Fatalf("Recover sent %v for attempts this process runs", calls)
	}
	// Once its Run is over, what it left is a previous run's to take over.
	d.drop("a_run")
	if err = d.Recover(bg); err != nil {
		t.Fatal(err)
	}
	waitGone(t, executor, 10e9)
	if f.count("computed") != 0 {
		t.Fatalf("the attempt still held was handed over: %v", f.log())
	}
}
