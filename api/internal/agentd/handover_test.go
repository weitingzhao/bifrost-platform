package agentd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// fakePlatform answers agentd's requests in memory (no port, no database):
// each request is recorded by its action (the last path segment; an
// artifact upload is "put:<kind>", a download "get"), and answered by the
// rule for that action, after its delay, unless the request's context ends
// first.
type fakePlatform struct {
	mu        sync.Mutex
	calls     []string
	bodies    map[string][][]byte
	artifacts map[string][]byte
	delay     map[string]time.Duration
	rule      map[string]func(n int) (status int, body string)
	// before, when set, runs as each request arrives.
	before func(action string)
	// hang holds a request for its time whatever its context does (a peer
	// that keeps the connection open).
	hang map[string]time.Duration
}

func newFake() *fakePlatform {
	return &fakePlatform{bodies: map[string][][]byte{}, artifacts: map[string][]byte{}, delay: map[string]time.Duration{},
		rule: map[string]func(int) (int, string){}, hang: map[string]time.Duration{}}
}

func (f *fakePlatform) set(action string, rule func(n int) (int, string)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rule[action] = rule
}

func always(status int, code string) func(int) (int, string) {
	return func(int) (int, string) { return status, `{"error":"` + code + `","message":"fake"}` }
}

func (f *fakePlatform) count(action string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bodies[action])
}

func (f *fakePlatform) sent(action string) [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.bodies[action])
}

func (f *fakePlatform) log() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakePlatform) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	body := []byte{}
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
	}
	action := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	if f.before != nil {
		f.before(action)
	}
	id := ""
	if strings.Contains(r.URL.Path, "/artifacts/") {
		id = action
		action = "get"
		if r.Method == http.MethodPut {
			action = "put:" + r.Header.Get("X-Artifact-Kind")
		}
	}
	f.mu.Lock()
	n := len(f.bodies[action])
	f.calls = append(f.calls, action)
	f.bodies[action] = append(f.bodies[action], body)
	delay, rule, hang := f.delay[action], f.rule[action], f.hang[action]
	f.mu.Unlock()
	time.Sleep(hang)
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
	status, out := http.StatusOK, "{}"
	if rule != nil {
		status, out = rule(n)
	}
	f.mu.Lock()
	switch {
	case action == "get" && status == http.StatusOK:
		out = string(f.artifacts[id])
	case strings.HasPrefix(action, "put:") && status == http.StatusOK:
		f.artifacts[id] = body
	}
	f.mu.Unlock()
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(out)),
		Header: http.Header{"Content-Type": {"application/json"}}, Request: r}, nil
}

func fakeDaemon(t *testing.T, f *fakePlatform) *Daemon {
	t.Helper()
	reg, err := OpenRegistry(filepath.Join(t.TempDir(), "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &Daemon{
		Client:   &Client{Base: "http://platform.invalid/api/v1/agentrt", Host: "h1", Token: "fixture", HTTP: &http.Client{Transport: f}},
		Registry: reg, Root: t.TempDir(), Self: os.Args[0], Logf: t.Logf,
		Beat: 10 * time.Millisecond, Retry: 5 * time.Millisecond, KillGrace: 500 * time.Millisecond, SalvageEvery: time.Hour,
	}
}

// entryFor is a registered attempt with its output directory.
func entryFor(t *testing.T, d *Daemon, id, phase string) (Entry, *Attempt) {
	t.Helper()
	dir := filepath.Join(d.Root, "tasks", "t_"+id, "a1")
	if err := os.MkdirAll(dir+".out", 0o700); err != nil {
		t.Fatal(err)
	}
	e := Entry{AttemptID: id, TaskID: "t_" + id, AttemptNo: 1, Epoch: 1, Role: "implement", Effect: "none", Workdir: dir, Phase: phase,
		Group: GroupCleared, EndHow: "exit", Conclusion: "done"}
	if err := d.Registry.Put(e); err != nil {
		t.Fatal(err)
	}
	return e, &Attempt{Claim: &Claimed{TaskID: e.TaskID, AttemptID: id, AttemptNo: 1, Role: "implement", Epoch: 1}, Dir: dir, Out: dir + ".out"}
}

func registered(t *testing.T, d *Daemon, id string) (Entry, bool) {
	t.Helper()
	m, err := d.Registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	e, ok := m[id]
	return e, ok
}

var bg = context.Background()

// Review 164 A3: the lease is renewed while the submit is in flight, and not
// after it is taken (release waits for a heartbeat in flight).
func TestSubmitKeepsTheLease(t *testing.T) {
	f := newFake()
	f.delay["submit"] = 300 * time.Millisecond
	d := fakeDaemon(t, f)
	e, a := entryFor(t, d, "a1", "persisted")
	l := d.keepLease(bg, e.AttemptID, e.Epoch)
	out, err := d.deliver(bg, a, nil, &e, l)
	if err != nil || !out.Submitted {
		t.Fatalf("deliver = %+v %v", out, err)
	}
	calls := f.log()
	submit, ended := slices.Index(calls, "submit"), slices.Index(calls, "ended")
	beats := 0
	for i := submit; i < ended; i++ {
		if calls[i] == "heartbeat" {
			beats++
		}
	}
	if submit < 0 || ended < submit || beats < 3 {
		t.Fatalf("%d heartbeats while the submit was sent: %v", beats, calls)
	}
	if slices.Contains(calls[ended:], "heartbeat") {
		t.Fatalf("a heartbeat after the lease was released: %v", calls)
	}
	if _, ok := registered(t, d, e.AttemptID); ok {
		t.Fatal("a submitted, ended attempt is still registered")
	}
}

// Review 164 A3: a submit taken whose answer was lost is resent, and
// answered again, even once the platform refuses heartbeats of the now
// submitted attempt: never salvaged.
func TestSubmitAnswerLost(t *testing.T) {
	f := newFake()
	f.set("heartbeat", always(http.StatusConflict, "fenced"))
	f.delay["submit"] = 50 * time.Millisecond
	d := fakeDaemon(t, f)
	e, _ := entryFor(t, d, "a1", "submitting")

	// The keeper has seen the refusal before the hand-over goes on.
	a := &Attempt{Claim: &Claimed{TaskID: e.TaskID, AttemptID: e.AttemptID, Epoch: 1}, Dir: e.Workdir, Out: e.Workdir + ".out"}
	l := d.keepLease(bg, e.AttemptID, e.Epoch)
	for deadline := time.Now().Add(5 * time.Second); !l.isLost(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the keeper never saw the refusal")
		}
	}
	out, err := d.deliver(bg, a, nil, &e, l)
	if err != nil || !out.Submitted || f.count("submit") != 1 || f.count("salvage") != 0 {
		t.Fatalf("deliver = %+v %v; calls %v", out, err, f.log())
	}

	// The same through a takeover.
	e2, _ := entryFor(t, d, "a2", "submitting")
	if err = d.Recover(bg); err != nil {
		t.Fatal(err)
	}
	if _, ok := registered(t, d, e2.AttemptID); ok || f.count("submit") != 2 || f.count("salvage") != 0 {
		t.Fatalf("takeover: calls %v", f.log())
	}
}

// Review 164 A4: an upload cancelled or not taken now leaves the result
// unprepared, to be uploaded later; only a refusal for good is no report.
func TestPrepareAfterACancel(t *testing.T) {
	f := newFake()
	d := fakeDaemon(t, f)
	e, a := entryFor(t, d, "a1", "computed")
	report := []byte("# done\n")
	if err := os.WriteFile(filepath.Join(a.Out, "report.md"), report, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.Out, "log.txt"), []byte("log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(bg)
	cancel()
	if err := d.prepare(cancelled, a, nil, &e); err == nil || e.Prepared || e.Report != "" || e.LogTailDone {
		t.Fatalf("cancelled prepare = %v; %+v", err, e)
	}
	if got, _ := registered(t, d, e.AttemptID); got.Prepared || got.LogTailDone {
		t.Fatalf("journal = %+v", got)
	}
	f.set("put:report", always(http.StatusServiceUnavailable, "down"))
	if err := d.prepare(bg, a, nil, &e); err == nil || e.Prepared || !e.LogTailDone || e.Report != "" {
		t.Fatalf("report not taken now = %v; %+v", err, e)
	}
	f.set("put:report", nil)
	if err := d.prepare(bg, a, nil, &e); err != nil || !e.Prepared || e.Report != ArtifactID(report) || len(e.Artifacts) != 1 {
		t.Fatalf("prepare = %v; %+v", err, e)
	}

	e2, a2 := entryFor(t, d, "a2", "computed")
	if err := os.WriteFile(filepath.Join(a2.Out, "report.md"), report, 0o600); err != nil {
		t.Fatal(err)
	}
	f.set("put:report", always(http.StatusUnprocessableEntity, "local_path"))
	if err := d.prepare(bg, a2, nil, &e2); err != nil || !e2.Prepared || e2.Report != "" || !e2.ReportDone {
		t.Fatalf("a report refused for good = %v; %+v", err, e2)
	}
}

// Review 164 A5: an attempt whose lease is lost is salvaged only once its
// branch is pushed; a push that fails leaves it pending, and the next try
// pushes the same commit and salvages it.
func TestLostPushesBeforeSalvage(t *testing.T) {
	env := gitEnv(t.TempDir())
	bare, base := remote(t, env)
	f := newFake()
	d := fakeDaemon(t, f)
	d.GitEnv = env
	e, a := entryFor(t, d, "a1", "lost")
	e.Effect, e.Repo, e.WorkID, e.PushedRef, e.Base = "branch", "r", "W-31", "refs/heads/agent/t_a1/a1", base
	ws, err := Prepare(bg, e.Workdir, bare, base, env)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(e.Workdir, "x.go"), []byte("package x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d.Remote = func(string) string { return filepath.Join(t.TempDir(), "missing.git") }
	if _, err = d.lost(bg, a, ws, &e); !errors.Is(err, ErrPending) || f.count("salvage") != 0 {
		t.Fatalf("a failed push = %v; calls %v", err, f.log())
	}
	got, ok := registered(t, d, e.AttemptID)
	if !ok || got.Phase != "lost" || !got.Prepared || len(got.PushedCommit) != 40 {
		t.Fatalf("journal = %+v", got)
	}
	d.Remote = func(string) string { return bare }
	out, err := d.lost(bg, a, ws, &got)
	if err != nil || !out.Salvaged {
		t.Fatalf("lost = %+v %v", out, err)
	}
	var body struct {
		Commit string `json:"pushed_commit"`
	}
	if jerr := json.Unmarshal(f.sent("salvage")[0], &body); jerr != nil || body.Commit != got.PushedCommit {
		t.Fatalf("salvaged %s, journal %s", body.Commit, got.PushedCommit)
	}
	if head := run(t, t.TempDir(), env, "git", "ls-remote", bare, e.PushedRef); !strings.HasPrefix(head, got.PushedCommit) {
		t.Fatalf("branch = %s", head)
	}
}

// Review 164 A6: a checkpoint's request is resent as it is until the
// platform confirms it, even when the workspace changed meanwhile; only then
// is the next sample taken. The patch is the net change against the base,
// commits the executor made included.
func TestCheckpointResendsItsRequest(t *testing.T) {
	env := gitEnv(t.TempDir())
	bare, base := remote(t, env)
	f := newFake()
	d := fakeDaemon(t, f)
	e, a := entryFor(t, d, "a1", "running")
	ws, err := Prepare(bg, e.Workdir, bare, base, env)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string) {
		if werr := os.WriteFile(filepath.Join(e.Workdir, name), []byte("package x // "+name+"\n"), 0o600); werr != nil {
			t.Fatal(werr)
		}
	}
	if err = os.WriteFile(filepath.Join(a.Out, "log.txt"), []byte("working\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	write("x.go")
	f.set("checkpoint", func(n int) (int, string) {
		if n == 0 {
			return http.StatusServiceUnavailable, `{"error":"down"}`
		}
		return http.StatusOK, "{}"
	})
	d.checkpoint(bg, a, ws, &e)
	if got, _ := registered(t, d, e.AttemptID); got.Pending == nil || got.Pending.Seq != 1 || got.Confirmed != 0 {
		t.Fatalf("after a lost answer: %+v", got)
	}
	write("y.go")
	d.checkpoint(bg, a, ws, &e)
	sent := f.sent("checkpoint")
	if len(sent) != 2 || !bytes.Equal(sent[0], sent[1]) || e.Pending != nil || e.Confirmed != 1 {
		t.Fatalf("resent %q / %q; entry %+v", sent, sent[len(sent)-1], e)
	}

	run(t, e.Workdir, env, "git", "add", "--all")
	run(t, e.Workdir, env, "git", "commit", "--quiet", "--no-verify", "-m", "the executor's own")
	if dirty, _ := ws.Dirty(bg); dirty {
		t.Fatal("the workspace should be clean")
	}
	d.checkpoint(bg, a, ws, &e)
	sent = f.sent("checkpoint")
	var third struct {
		Seq       int      `json:"checkpoint_seq"`
		Artifacts []string `json:"artifacts"`
	}
	if jerr := json.Unmarshal(sent[len(sent)-1], &third); jerr != nil || third.Seq != 2 || len(third.Artifacts) != 2 || e.Confirmed != 2 {
		t.Fatalf("third = %s; entry %+v", sent[len(sent)-1], e)
	}
	patches := f.sent("put:checkpoint_patch")
	if last := string(patches[len(patches)-1]); !strings.Contains(last, "y.go") || !strings.Contains(last, "x.go") {
		t.Fatalf("the patch of a clean workspace whose HEAD is past its base:\n%s", last)
	}
}

// Review 164 A7: a checkpoint that hangs holds up neither a stop nor the
// hard limit: the group ends within its grace.
func TestEndsWhileACheckpointHangs(t *testing.T) {
	for _, why := range []string{"cancelled", "hard_limit"} {
		t.Run(why, func(t *testing.T) {
			f := newFake()
			f.delay["put:log_tail"] = time.Hour
			d := fakeDaemon(t, f)
			d.CheckpointEvery, d.SampleLimit = 20*time.Millisecond, time.Hour
			e, a := entryFor(t, d, "a1", "running")
			if why == "hard_limit" {
				a.Claim.HardLimitS = 1
			}
			if err := os.WriteFile(filepath.Join(a.Out, "log.txt"), []byte("working\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			pidPath := filepath.Join(a.Out, "executor.pid")
			p, err := Launch(os.Args[0], Spawn{Argv: []string{"sh", "-c", "echo $$ > " + pidPath + "; exec sleep 60"}, Env: launcherEnv()},
				func(pgid int, start string) error { e.PGID, e.Start = pgid, start; return nil })
			if err != nil {
				t.Fatal(err)
			}
			executor := waitPID(t, pidPath)
			l := &lease{lost: make(chan struct{}), stop: make(chan struct{}), cancel: func() {}, done: make(chan struct{})}
			type ended struct {
				how  string
				code int
			}
			res := make(chan ended, 1)
			start := time.Now()
			go func() { how, code := d.supervise(bg, a, p, nil, &e, l); res <- ended{how, code} }()
			for f.count("put:log_tail") == 0 { // a checkpoint is in flight, and hangs
				if time.Since(start) > 5*time.Second {
					t.Fatal("no checkpoint started")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if why == "cancelled" {
				l.markStop()
			}
			select {
			case r := <-res:
				if r.how != why || e.Group != GroupCleared {
					t.Fatalf("ended %s, group %s", r.how, e.Group)
				}
			case <-time.After(8 * time.Second):
				t.Fatalf("still supervising after %s", time.Since(start))
			}
			waitGone(t, executor, time.Second)
		})
	}
}

// Review 164 A7: a lost lease or a stop that comes while the start is being
// reported cuts it short: the executor is never released.
func TestStartCutShort(t *testing.T) {
	for _, why := range []string{"lease_lost", "cancelled"} {
		t.Run(why, func(t *testing.T) {
			f := newFake()
			f.delay["start"] = time.Hour
			f.set("heartbeat", func(int) (int, string) {
				if f.count("start") == 0 {
					return http.StatusOK, `{"stop":false}`
				}
				if why == "cancelled" {
					return http.StatusOK, `{"stop":true}`
				}
				return http.StatusConflict, `{"error":"fenced"}`
			})
			d := fakeDaemon(t, f)
			spec := []byte("# do it\n")
			f.artifacts[ArtifactID(spec)] = spec
			marker := filepath.Join(t.TempDir(), "ran")
			d.Executors = map[string]Executor{"exec:x": func(a *Attempt) (Spawn, error) {
				return Spawn{Argv: []string{"sh", "-c", "touch " + marker + "; exec sleep 30"}, Env: launcherEnv(), Dir: a.Dir}, nil
			}}
			cl := &Claimed{TaskID: "t1", AttemptID: "a1", AttemptNo: 1, Role: "implement", SpecArtifact: ArtifactID(spec), EffectClass: "none",
				Epoch: 1, Tool: Tool{Executor: "exec:x"}}
			start := time.Now()
			out, err := d.Run(bg, cl)
			if time.Since(start) > 10*time.Second || out.EndHow != why {
				t.Fatalf("run = %+v %v after %s", out, err, time.Since(start))
			}
			time.Sleep(200 * time.Millisecond)
			if _, serr := os.Stat(marker); !errors.Is(serr, os.ErrNotExist) {
				t.Fatal("the executor was released")
			}
		})
	}
}

// Review 164 A8: a takeover ends every group a previous run left before it
// sends anything, and a hand-over that waits for the platform (salvage in
// grace) neither loops nor holds up the others; it is tried again only
// after SalvageEvery.
func TestRecoverEndsGroupsFirstAndIsBounded(t *testing.T) {
	f := newFake()
	f.set("salvage", always(http.StatusConflict, "in_grace"))
	d := fakeDaemon(t, f)
	entryFor(t, d, "a_lost", "lost")
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
	var once sync.Once
	goneAtFirst := false
	f.before = func(string) { once.Do(func() { goneAtFirst = gone(executor) }) }
	start := time.Now()
	if err = d.Recover(bg); err != nil || time.Since(start) > 5*time.Second {
		t.Fatalf("recover = %v after %s", err, time.Since(start))
	}
	if !gone(executor) || !goneAtFirst {
		t.Fatalf("the group was not ended before the first request (gone then: %v, now: %v)", goneAtFirst, gone(executor))
	}
	if first := f.log(); len(first) == 0 || f.count("ended") != 1 || f.count("salvage") != 1 {
		t.Fatalf("calls %v", first)
	}
	if e, ok := registered(t, d, "a_lost"); !ok || e.Phase != "lost" {
		t.Fatalf("the pending salvage = %+v %v", e, ok)
	}
	if _, ok := registered(t, d, "a_run"); ok {
		t.Fatal("the ended group is still registered")
	}
	if err = d.Recover(bg); err != nil || f.count("salvage") != 1 {
		t.Fatalf("tried again before SalvageEvery: %v %v", err, f.log())
	}
}

// Review 164 A2: an ended attempt whose group is not gone is kept, the host
// takes no new work, and the ended request is not sent again; once the
// group is gone the entry is forgotten.
func TestUnclearedBlocksTheHost(t *testing.T) {
	f := newFake()
	d := fakeDaemon(t, f)
	e, _ := entryFor(t, d, "a1", "submitted")
	pidPath := filepath.Join(e.Workdir+".out", "executor.pid")
	p, err := Launch(os.Args[0], Spawn{Argv: []string{"sh", "-c", "echo $$ > " + pidPath + "; exec sleep 60"}, Env: launcherEnv()},
		func(pgid int, start string) error { e.PGID, e.Start = pgid, start; return nil })
	if err != nil {
		t.Fatal(err)
	}
	executor := waitPID(t, pidPath)
	t.Cleanup(func() { _ = syscall.Kill(executor, syscall.SIGKILL) })
	if err = syscall.Kill(p.PGID, syscall.SIGKILL); err != nil { // the leader gone, the executor left
		t.Fatal(err)
	}
	p.Wait()
	e.Group = GroupUnknown
	if err = d.end(bg, &e, "exit"); err != nil {
		t.Fatal(err)
	}
	if got, ok := registered(t, d, "a1"); !ok || got.Phase != "uncleared" || got.EndRequest == nil || got.EndRequest.Cleared {
		t.Fatalf("entry = %+v %v", got, ok)
	}
	if id, berr := d.Blocked(); berr != nil || id != "a1" {
		t.Fatalf("blocked = %q %v", id, berr)
	}
	if err = syscall.Kill(executor, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	waitGone(t, executor, time.Second)
	if err = d.Recover(bg); err != nil {
		t.Fatal(err)
	}
	if _, ok := registered(t, d, "a1"); ok || f.count("ended") != 1 {
		t.Fatalf("after the group went: registered %v, ended sent %d times", ok, f.count("ended"))
	}
	if id, _ := d.Blocked(); id != "" {
		t.Fatalf("still blocked by %s", id)
	}
}

// Review 165 A1: a checkpoint's request is sent only once it is on disk: a
// first write that failed is written again before anything is sent.
func TestCheckpointWrittenBeforeSent(t *testing.T) {
	f := newFake()
	d := fakeDaemon(t, f)
	e, a := entryFor(t, d, "a1", "running")
	if err := os.WriteFile(filepath.Join(a.Out, "log.txt"), []byte("working\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A write that fails (not a mode: CI runs as root, which no mode stops).
	d.Registry.failWrite = func() error { return errors.New("disk full") }
	d.checkpoint(bg, a, nil, &e)
	if f.count("checkpoint") != 0 || e.Pending == nil {
		t.Fatalf("sent with the registry unwritable: %v; pending %+v", f.log(), e.Pending)
	}
	d.Registry.failWrite = nil
	onDisk := false
	f.before = func(action string) {
		if action == "checkpoint" {
			got, _ := registered(t, d, e.AttemptID)
			onDisk = got.Pending != nil && got.Pending.Seq == 1
		}
	}
	d.checkpoint(bg, a, nil, &e)
	if f.count("checkpoint") != 1 || !onDisk || e.Confirmed != 1 {
		t.Fatalf("sent %d, on disk when sent %v, confirmed %d", f.count("checkpoint"), onDisk, e.Confirmed)
	}
}

// Review 165 A2: once the leader is gone the group is cleared at once, even
// while a sample is still finishing (one that ignores its cancellation).
func TestGroupClearedBeforeASlowSample(t *testing.T) {
	f := newFake()
	f.hang["put:log_tail"] = 6 * time.Second
	d := fakeDaemon(t, f)
	d.CheckpointEvery, d.SampleLimit = 20*time.Millisecond, time.Hour
	e, a := entryFor(t, d, "a1", "running")
	if err := os.WriteFile(filepath.Join(a.Out, "log.txt"), []byte("working\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pidPath := filepath.Join(a.Out, "executor.pid")
	p, err := Launch(os.Args[0], Spawn{Argv: []string{"sh", "-c", "echo $$ > " + pidPath + "; exec sleep 60"}, Env: launcherEnv()},
		func(pgid int, start string) error { e.PGID, e.Start = pgid, start; return nil })
	if err != nil {
		t.Fatal(err)
	}
	executor := waitPID(t, pidPath)
	t.Cleanup(func() { _ = syscall.Kill(executor, syscall.SIGKILL) })
	l := &lease{lost: make(chan struct{}), stop: make(chan struct{}), cancel: func() {}, done: make(chan struct{})}
	res := make(chan string, 1)
	go func() { how, _ := d.supervise(bg, a, p, nil, &e, l); res <- how }()
	for start := time.Now(); f.count("put:log_tail") == 0; time.Sleep(10 * time.Millisecond) {
		if time.Since(start) > 5*time.Second {
			t.Fatal("no sample started")
		}
	}
	time.Sleep(1100 * time.Millisecond)                          // the executor's start, in seconds, before the leader's death
	if err = syscall.Kill(p.PGID, syscall.SIGKILL); err != nil { // the leader alone
		t.Fatal(err)
	}
	waitGone(t, executor, 1500*time.Millisecond)
	select {
	case how := <-res:
		if e.Group != GroupCleared {
			t.Fatalf("ended %s, group %s", how, e.Group)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("still supervising")
	}
}

// Review 165 A3: a takeover step that never ends (a push to a peer that
// keeps the connection open) is cut off within TakeOverLimit; the attempt
// stays pending and is not tried again before SalvageEvery.
func TestTakeOverIsBounded(t *testing.T) {
	env := gitEnv(t.TempDir())
	bare, base := remote(t, env)
	f := newFake()
	d := fakeDaemon(t, f)
	d.GitEnv = append(env, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=protocol.ext.allow", "GIT_CONFIG_VALUE_0=always")
	d.TakeOverLimit, d.Retry = 500*time.Millisecond, 50*time.Millisecond
	e, _ := entryFor(t, d, "a1", "lost")
	e.Effect, e.Repo, e.WorkID, e.PushedRef, e.Base = "branch", "r", "W-31", "refs/heads/agent/t_a1/a1", base
	if _, err := Prepare(bg, e.Workdir, bare, base, env); err != nil {
		t.Fatal(err)
	}
	// A real change: an attempt that changed nothing is never pushed (TD-360).
	if err := os.WriteFile(filepath.Join(e.Workdir, "x.go"), []byte("package x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := d.Registry.Put(e); err != nil {
		t.Fatal(err)
	}
	d.Remote = func(string) string { return "ext::sleep 30" }
	start := time.Now()
	if err := d.Recover(bg); err != nil || time.Since(start) > 6*time.Second {
		t.Fatalf("recover = %v after %s", err, time.Since(start))
	}
	got, ok := registered(t, d, "a1")
	if !ok || got.Phase != "lost" || !got.Prepared || f.count("salvage") != 0 {
		t.Fatalf("after a cut-off push: %+v %v; calls %v", got, ok, f.log())
	}
	start = time.Now()
	if err := d.Recover(bg); err != nil || time.Since(start) > time.Second {
		t.Fatalf("tried again before SalvageEvery: %v after %s", err, time.Since(start))
	}
}

// Review 165 B1: a takeover that cannot write the registry says so.
func TestRecoverReportsTheRegistry(t *testing.T) {
	f := newFake()
	d := fakeDaemon(t, f)
	entryFor(t, d, "a1", "exited")
	d.Registry.failWrite = func() error { return errors.New("disk full") }
	if err := d.Recover(bg); !errors.Is(err, ErrRegistry) {
		t.Fatalf("recover = %v", err)
	}
}

// Review 165 A3: git cut short by the step's deadline (here: a context
// already ended when the attempt's commit is made) is a step to try again:
// the hand-over stays where it was, never ended.
func TestGitCutShortIsPending(t *testing.T) {
	env := gitEnv(t.TempDir())
	bare, base := remote(t, env)
	f := newFake()
	d := fakeDaemon(t, f)
	d.GitEnv = env
	e, a := entryFor(t, d, "a1", "computed")
	e.Effect, e.Repo, e.WorkID, e.PushedRef, e.Base, e.LogTailDone, e.ReportDone = "branch", "r", "W-31", "refs/heads/agent/t_a1/a1", base, true, true
	ws, err := Prepare(bg, e.Workdir, bare, base, env)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(e.Workdir, "x.go"), []byte("package x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ended, cancel := context.WithCancel(bg)
	cancel()
	if _, err = d.deliver(ended, a, ws, &e, nil); !errors.Is(err, ErrPending) {
		t.Fatalf("deliver = %v", err)
	}
	if got, ok := registered(t, d, "a1"); !ok || got.Phase != "computed" || got.Prepared || f.count("ended") != 0 {
		t.Fatalf("after git was cut short: %+v %v; calls %v", got, ok, f.log())
	}
}

// fakeGit puts a git first on PATH whose diff leaves a helper holding its
// stdout (and only its stdout) for 30 s after git itself has exited; the
// bounds below are far under that and far over gitWaitDelay, so a loaded
// machine does not turn them red.
func fakeGit(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\ncase \"$1\" in\n  diff) sleep 30 2>/dev/null & echo patch; exit 0 ;;\n  *) exit 0 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// Review 171 A1: reading the patch ends within gitWaitDelay of git's end,
// though a helper git started still holds stdout; so does a takeover that
// makes a restarted attempt's last checkpoint.
func TestDiffBoundedWhenAHelperHoldsStdout(t *testing.T) {
	fakeGit(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	ws := &Workspace{Dir: dir, Base: "base", env: os.Environ()}
	start := time.Now()
	if _, _, err := ws.Diff(bg, patchLimit); time.Since(start) > gitWaitDelay+8*time.Second {
		t.Fatalf("diff returned after %s (%v)", time.Since(start), err)
	}

	f := newFake()
	d := fakeDaemon(t, f)
	d.SampleLimit = time.Second
	e, _ := entryFor(t, d, "a1", "running")
	e.Repo, e.Base, e.Group = "r", "base", ""
	if err := os.MkdirAll(filepath.Join(e.Workdir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := d.Registry.Put(e); err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	if err := d.Recover(bg); err != nil || time.Since(start) > gitWaitDelay+10*time.Second {
		t.Fatalf("recover = %v after %s", err, time.Since(start))
	}
	if _, ok := registered(t, d, "a1"); ok || f.count("ended") != 1 {
		t.Fatalf("the restarted attempt was not ended: calls %v", f.log())
	}
}

// Review 171 B1: a registry that cannot be written while a result is
// prepared is reported, from a lost attempt's takeover and from a hand-over,
// and never ends the attempt as refused.
func TestPrepareKeepsTheRegistryError(t *testing.T) {
	f := newFake()
	d := fakeDaemon(t, f)
	entryFor(t, d, "a1", "lost")
	d.Registry.failWrite = func() error { return errors.New("disk full") }
	if err := d.Recover(bg); !errors.Is(err, ErrRegistry) {
		t.Fatalf("recover of a lost attempt = %v", err)
	}
	d.Registry.failWrite = nil
	// One write fails, the next would work: the hand-over stops on the
	// failure and is not ended (an end would be written and sent).
	e, a := entryFor(t, d, "a2", "computed")
	failed := false
	d.Registry.failWrite = func() error {
		if failed {
			return nil
		}
		failed = true
		return errors.New("disk full")
	}
	if _, err := d.deliver(bg, a, nil, &e, nil); !errors.Is(err, ErrRegistry) || f.count("ended") != 0 {
		t.Fatalf("deliver = %v; calls %v", err, f.log())
	}
	if got, ok := registered(t, d, "a2"); !ok || got.Phase != "computed" || got.EndRequest != nil {
		t.Fatalf("journal = %+v %v", got, ok)
	}
}
