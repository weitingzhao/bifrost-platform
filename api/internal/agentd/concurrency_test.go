package agentd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
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

// Review 276 A1: a Run that hands its entry on (exited, the platform away)
// and lets go of its attempt while Recover reads the registry: Recover either
// read the new entry, or still saw the attempt held; it never takes over from
// the old one.
func TestRecoverNeverTakesOverFromAnOldRead(t *testing.T) {
	f := newFake()
	d := fakeDaemon(t, f)
	e, _ := entryFor(t, d, "a_handing", "running")
	e.Group = GroupCleared
	if err := d.Registry.Put(e); err != nil {
		t.Fatal(err)
	}
	d.Hold("a_handing")
	d.afterLoad = func() {
		done := make(chan struct{})
		go func() {
			defer close(done)
			e.Phase = "exited"
			_ = d.Registry.Put(e)
			d.drop("a_handing")
		}()
		select {
		case <-done:
		case <-time.After(200 * time.Millisecond):
		}
	}
	if err := d.Recover(bg); err != nil {
		t.Fatal(err)
	}
	d.afterLoad = nil
	if got, ok := registered(t, d, "a_handing"); ok && got.Phase != "exited" || f.count("ended") != 0 {
		t.Fatalf("entry %+v (%v), calls %v: Recover took over from what it read before the hand-over", got, ok, f.log())
	}
}

// Review 276 A2: confined attempts hold the host together; an unconfined one
// waits for them and has it alone; the confined that come after a waiting
// unconfined one wait behind it; a wait that is given up lets them go.
func TestHostShare(t *testing.T) {
	var h hostShare
	r1, err := h.take(bg, false)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := h.take(bg, false)
	if err != nil {
		t.Fatal("a second confined attempt waited")
	}
	got := make(chan func(), 1)
	go func() { r, _ := h.take(bg, true); got <- r }()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-got:
		t.Fatal("an unconfined attempt went beside confined ones")
	default:
	}
	behind := make(chan func(), 1)
	go func() { r, _ := h.take(bg, false); behind <- r }()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-behind:
		t.Fatal("a confined attempt went past a waiting unconfined one")
	default:
	}
	r1()
	r2()
	alone := <-got
	select {
	case <-behind:
		t.Fatal("a confined attempt went beside an unconfined one")
	case <-time.After(50 * time.Millisecond):
	}
	alone()
	(<-behind)()

	// an unconfined wait given up lets the confined behind it go
	r3, _ := h.take(bg, false)
	ctx, cancel := context.WithCancel(bg)
	gaveUp := make(chan error, 1)
	go func() { _, err := h.take(ctx, true); gaveUp <- err }()
	time.Sleep(50 * time.Millisecond)
	next := make(chan func(), 1)
	go func() { r, _ := h.take(bg, false); next <- r }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-gaveUp; err == nil {
		t.Fatal("a cancelled wait took the host")
	}
	select {
	case r := <-next:
		r()
	case <-time.After(time.Second):
		t.Fatal("the confined attempt behind a cancelled wait still waits")
	}
	r3()
}

func TestConfined(t *testing.T) {
	for _, c := range []struct {
		executor, effect string
		setup            bool
		want             bool
	}{
		{"exec:claude", "none", false, true},
		{"exec:codex", "branch", false, true},
		{"exec:cursor", "none", false, true},
		{"exec:cursor", "branch", false, false},
		{"exec:deterministic", "none", false, false},
		{"exec:codex", "branch", true, false},
		{"exec:other", "none", false, false},
	} {
		spec := TaskSpec{}
		if c.setup {
			spec.Setup = [][]string{{"npm", "ci"}}
		}
		cl := &Claimed{Tool: Tool{Executor: c.executor}, EffectClass: c.effect}
		if got := confined(cl, spec); got != c.want {
			t.Errorf("%s %s setup=%v: confined = %v", c.executor, c.effect, c.setup, got)
		}
	}
}
