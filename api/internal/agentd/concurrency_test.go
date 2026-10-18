package agentd

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	// No executor is confined until its whole process tree is held to its
	// workspace and that is proven on the host (reviews 277 A2, 278 A2;
	// TD-371): claude's tools and cursor's ask mode do not reach the hooks a
	// configuration runs.
	for _, c := range []struct {
		executor, effect string
		setup            bool
	}{
		{"exec:claude", "none", false},
		{"exec:cursor", "none", false},
		{"exec:cursor", "branch", false},
		{"exec:codex", "branch", false},
		{"exec:codex", "none", false},
		{"exec:deterministic", "none", false},
		{"exec:codex", "branch", true},
		{"exec:other", "none", false},
	} {
		spec := TaskSpec{}
		if c.setup {
			spec.Setup = [][]string{{"npm", "ci"}}
		}
		cl := &Claimed{Tool: Tool{Executor: c.executor}, EffectClass: c.effect}
		if confined(cl, spec) {
			t.Errorf("%s %s setup=%v is confined", c.executor, c.effect, c.setup)
		}
	}
}

// Review 278 B: an attempt that fails after it waited for the host and
// before its executor is released is ended as exit, not left to be ended as a
// restart.
func TestAFailureBeforeTheLaunchEndsAsExit(t *testing.T) {
	f := newFake()
	d := fakeDaemon(t, f)
	spec := []byte("# do it\n")
	f.artifacts[ArtifactID(spec)] = spec
	d.Executors = map[string]Executor{"exec:x": func(a *Attempt) (Spawn, error) { return Spawn{}, errors.New("no such tool here") }}
	cl := &Claimed{TaskID: "t1", AttemptID: "a1", AttemptNo: 1, Role: "implement", SpecArtifact: ArtifactID(spec), EffectClass: "none",
		Epoch: 1, Tool: Tool{Executor: "exec:x"}}
	out, err := d.Run(bg, cl)
	if err == nil || out.EndHow != "exit" {
		t.Fatalf("run = %+v %v", out, err)
	}
	ended := f.sent("ended")
	if len(ended) != 1 || !strings.Contains(string(ended[0]), `"end_how":"exit"`) {
		t.Fatalf("ended %q", ended)
	}
	if _, ok := registered(t, d, "a1"); ok {
		t.Fatal("the entry stays for a takeover")
	}
}

// Review 277 A3: an attempt waiting for the host hears its lease: a stop or
// a lost lease ends the wait (and the attempt, unreleased) while another holds
// the host; a wait cut by agentd's shutdown leaves its entry for Recover.
func TestWaitingForTheHostHearsTheLease(t *testing.T) {
	for _, why := range []string{"cancelled", "lease_lost", "daemon_restart"} {
		t.Run(why, func(t *testing.T) {
			f := newFake()
			f.set("heartbeat", func(n int) (int, string) {
				switch {
				case why == "cancelled" && n >= 2:
					return http.StatusOK, `{"stop":true}`
				case why == "lease_lost" && n >= 2:
					return http.StatusConflict, `{"error":"fenced"}`
				}
				return http.StatusOK, `{"stop":false}`
			})
			d := fakeDaemon(t, f)
			spec := []byte("# do it\n")
			f.artifacts[ArtifactID(spec)] = spec
			marker := filepath.Join(t.TempDir(), "ran")
			d.Executors = map[string]Executor{"exec:x": func(a *Attempt) (Spawn, error) {
				return Spawn{Argv: []string{"sh", "-c", "touch " + marker}, Env: launcherEnv(), Dir: a.Dir}, nil
			}}
			// another attempt has the host to itself
			holder, err := d.share.take(bg, true)
			if err != nil {
				t.Fatal(err)
			}
			defer holder()
			ctx, cancel := context.WithCancel(bg)
			defer cancel()
			if why == "daemon_restart" {
				time.AfterFunc(200*time.Millisecond, cancel)
			}
			cl := &Claimed{TaskID: "t1", AttemptID: "a1", AttemptNo: 1, Role: "implement", SpecArtifact: ArtifactID(spec), EffectClass: "none",
				Epoch: 1, Tool: Tool{Executor: "exec:x"}}
			done := make(chan struct{})
			var out Outcome
			go func() { defer close(done); out, err = d.Run(ctx, cl) }()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("the wait for the host did not hear the lease")
			}
			if _, serr := os.Stat(marker); !errors.Is(serr, os.ErrNotExist) {
				t.Fatal("the executor was released")
			}
			switch why {
			case "daemon_restart":
				if e, ok := registered(t, d, "a1"); !ok || e.Phase != "launching" {
					t.Fatalf("after a shutdown the entry is %+v %v; want it left for Recover", e, ok)
				}
				if err := d.Recover(bg); err != nil || f.count("ended") != 1 {
					t.Fatalf("Recover: %v, calls %v", err, f.log())
				}
			default:
				if out.EndHow != why {
					t.Fatalf("run = %+v %v", out, err)
				}
			}
		})
	}
}

// Review 277 A1: the main checkout refuses a link an earlier attempt left on
// its way, and reads no template or hooks from the user's git configuration.
func TestCheckoutRefusesLinksAndTheUsersGitConfig(t *testing.T) {
	env := gitEnv(t.TempDir())
	bare, base := remote(t, env)
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tasks"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "tasks", "t_next")); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareIn(bg, root, WorkspaceDir(root, "t_next", 1), bare, base, env, Fetch{}); err == nil {
		t.Fatal("a checkout through a link was made")
	}
	if _, err := os.Stat(filepath.Join(outside, "a1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the checkout went out of agentd's directory")
	}
	// a global template whose post-checkout hook leaves a mark
	home := t.TempDir()
	tpl := filepath.Join(home, "tpl", "hooks")
	if err := os.MkdirAll(tpl, 0o700); err != nil {
		t.Fatal(err)
	}
	mark := filepath.Join(home, "hooked")
	if err := os.WriteFile(filepath.Join(tpl, "post-checkout"), []byte("#!/bin/sh\ntouch "+mark+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[init]\n\ttemplateDir = "+filepath.Join(home, "tpl")+"\n[core]\n\thooksPath = "+tpl+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	homed := append(append([]string{}, env...), "HOME="+home, "XDG_CONFIG_HOME="+home)
	if _, err := prepareIn(bg, root, WorkspaceDir(root, "t_ok", 1), bare, base, homed, Fetch{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mark); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the checkout ran a hook from the user's git configuration")
	}
}
