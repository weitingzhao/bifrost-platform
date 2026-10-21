package agentd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// stopsSeen is stopSeen by attempt: the attempts a heartbeat answer of stop
// was written whole to (review 491 B1: each, not any).
func stopsSeen(t *testing.T, base string) (string, func(attempt string) bool) {
	t.Helper()
	u, err := url.Parse(base)
	must(t, err)
	var mu sync.Mutex
	seen := map[string]bool{}
	rp := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: u.Scheme, Host: u.Host})
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &teeWriter{ResponseWriter: w}
		rp.ServeHTTP(rec, r)
		path := strings.TrimSuffix(r.URL.Path, "/heartbeat")
		if path == r.URL.Path {
			return
		}
		var out map[string]any
		if json.Unmarshal(rec.body.Bytes(), &out) == nil && out["stop"] == true {
			mu.Lock()
			seen[path[strings.LastIndex(path, "/")+1:]] = true
			mu.Unlock()
		}
	}))
	t.Cleanup(proxy.Close)
	return proxy.URL + u.Path, func(attempt string) bool { mu.Lock(); defer mu.Unlock(); return seen[attempt] }
}

// Review 489 B1: T09 and T07 with two attempts on one host (2.4 slice ③), at
// production's pace (a heartbeat every 30 seconds, 10 seconds' grace). One
// attempt is pushing its branch, held by the remote's hook; the other runs.
// A global stop set through POST /controls: within 60 seconds the running
// one's child is dead and its attempt cancelled, both attempts are answered
// stop, no third attempt is leased; the push goes on and, let go, is
// recorded whole, or, refused, leaves nothing (the remote's branch and the
// platform's commit the same, or both absent; review 491 B1). The attempts
// are classed confined as the older concurrency test classes them: the wall
// and its pins under concurrency are 06's walk, not this Mac's.
func TestAGlobalStopOnAHostOfTwo(t *testing.T) {
	if testing.Short() {
		t.Skip("runs at production's heartbeat pace")
	}
	for _, refuse := range []bool{false, true} {
		name := "pushed"
		if refuse {
			name = "refused"
		}
		t.Run(name, func(t *testing.T) { stopOnAHostOfTwo(t, refuse) })
	}
}

func stopOnAHostOfTwo(t *testing.T, refuse bool) {
	env := gitEnvT(t.TempDir())
	bare, baseCommit := bareRemote(t, env)
	sig := t.TempDir()
	mark := func(f string) string { return filepath.Join(sig, f) }
	hook := "#!/bin/sh\ntouch '" + mark("pushing") + "'\ni=0\n" +
		"while [ ! -f '" + mark("go") + "' ]; do\n" +
		"  i=$((i+1)); if [ $i -gt 2400 ]; then touch '" + mark("timedout") + "'; exit 1; fi\n" +
		"  sleep 0.05\ndone\n"
	if refuse {
		hook += "exit 1\n"
	}
	must(t, os.WriteFile(filepath.Join(bare, "hooks", "pre-receive"), []byte(hook), 0o755))
	head := func(_ context.Context, _, ref string) (string, error) {
		out, err := exec.Command("git", "ls-remote", bare, ref).Output()
		first, _, _ := strings.Cut(string(out), "\t")
		return strings.TrimSpace(first), err
	}
	pool, pushTask, base := platformWith(t, store.Task{Effect: "branch", Repo: "bifrost-platform", BaseCommit: baseCommit}, head)
	tx := func(fn func(pgx.Tx) error) { t.Helper(); must(t, store.InTx(ctx, pool, fn)) }
	tx(func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: per_host 2"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, concurrencyTwo)
		return err
	})
	runTask := readyTask(t, tx)
	readyTask(t, tx) // a third, which the stop keeps off the host
	viaProxy, seen := stopsSeen(t, base)
	push := script(`echo 'package x' > x.go; printf '# done\n' > "$OUT/report.md"`)
	run := script(`echo $$ > "$OUT/pid"; exec sleep 300`)
	d := daemon(t, viaProxy, func(a *agentd.Attempt) (agentd.Spawn, error) {
		if a.Claim.TaskID == pushTask {
			return push(a)
		}
		return run(a)
	}, bare, env)
	d.Confined = func(*agentd.Claimed, agentd.TaskSpec) bool { return true }
	d.Beat, d.KillGrace = 30*time.Second, 10*time.Second
	byTask := map[string]*agentd.Claimed{}
	for i := 0; i < 2; i++ {
		cl, err := d.Client.Claim(ctx, agentd.MaxSlots-i, []agentd.Tool{cursor})
		if err != nil || cl == nil {
			t.Fatalf("claim %d = %+v %v", i, cl, err)
		}
		byTask[cl.TaskID] = cl
	}
	if byTask[pushTask] == nil || byTask[runTask] == nil {
		t.Fatalf("claimed %v, want %s and %s", byTask, pushTask, runTask)
	}
	type ran struct {
		out agentd.Outcome
		err error
	}
	done := map[string]chan ran{pushTask: make(chan ran, 1), runTask: make(chan ran, 1)}
	runCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	for task, cl := range byTask {
		d.Hold(cl.AttemptID)
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := d.Run(runCtx, cl)
			done[task] <- ran{out, err}
		}()
	}
	t.Cleanup(func() {
		_ = os.WriteFile(mark("go"), nil, 0o600)
		cancel()
		wg.Wait()
	})
	waitFile(t, mark("pushing"), 60*time.Second)
	runOut := agentd.WorkspaceDir(d.Root, runTask, byTask[runTask].AttemptNo) + ".out"
	waitFile(t, filepath.Join(runOut, "pid"), 60*time.Second)
	b, err := os.ReadFile(filepath.Join(runOut, "pid"))
	must(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	must(t, err)

	// The stop, as the Owner's Work page sets it.
	set := time.Now()
	body, _ := json.Marshal(map[string]any{"idempotency_key": "b1-global-stop", "scope": "global", "kind": "stop", "reason": "T09 with two attempts"})
	req, err := http.NewRequest(http.MethodPost, base+"/controls", bytes.NewReader(body))
	must(t, err)
	req.Header.Set("Authorization", "Bearer fixture-agentd-h1")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	must(t, err)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /controls = %d", resp.StatusCode)
	}

	var r ran
	select {
	case r = <-done[runTask]:
	case <-time.After(60 * time.Second):
		t.Fatalf("the running attempt had not ended %s after the stop", time.Since(set).Round(time.Second))
	}
	took := time.Since(set)
	if r.out.EndHow != "cancelled" || r.out.Submitted {
		t.Fatalf("the running attempt: %+v %v", r.out, r.err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("its child %d is still alive %s after the stop (%v)", pid, took.Round(time.Second), err)
	}
	if took > 60*time.Second {
		t.Fatalf("the child died %s after the stop, over T09's 60 seconds", took.Round(time.Second))
	}
	t.Logf("T09 with two attempts: the running one's child dead %s after POST /controls", took.Round(100*time.Millisecond))
	if cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor}); err != nil || cl != nil {
		t.Fatalf("a third attempt was leased under the stop: %+v %v", cl, err)
	}
	// Both attempts are answered stop before the push is let go: the
	// pushing one's heartbeats go on through its hand-over.
	for deadline := set.Add(45 * time.Second); !seen(byTask[pushTask].AttemptID) || !seen(byTask[runTask].AttemptID); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("not each attempt was answered stop: pushing %v, running %v", seen(byTask[pushTask].AttemptID), seen(byTask[runTask].AttemptID))
		}
	}

	// The push goes on; let go, it is recorded whole.
	select {
	case r = <-done[pushTask]:
		t.Fatalf("the pushing attempt ended before its push was let go: %+v %v", r.out, r.err)
	default:
	}
	must(t, os.WriteFile(mark("go"), nil, 0o600))
	select {
	case r = <-done[pushTask]:
	case <-time.After(90 * time.Second):
		t.Fatal("the pushing attempt did not end once its push was let go")
	}
	if _, err := os.Stat(mark("timedout")); err == nil {
		t.Fatal("the hook stopped holding the push on its own")
	}
	cl := byTask[pushTask]
	remote, _, _ := strings.Cut(sh(t, t.TempDir(), env, "git", "ls-remote", bare, cl.PushedRef), "\t")
	var recorded, state string
	must(t, pool.QueryRow(ctx, `SELECT COALESCE(pushed_commit, ''), state FROM agentrt.attempt WHERE attempt_id = $1`, cl.AttemptID).Scan(&recorded, &state))
	if remote != recorded {
		t.Fatalf("half a push under the stop: the remote's branch is %q, the platform holds %q (run %+v %v)", remote, recorded, r.out, r.err)
	}
	switch {
	case !refuse && (remote == "" || !r.out.Submitted || state != "submitted"):
		t.Fatalf("not pushed and recorded under the stop: remote %q, attempt %s, run %+v %v", remote, state, r.out, r.err)
	case refuse && (recorded != "" || r.out.Submitted):
		t.Fatalf("a refused push under the stop left commit %q, run %+v", recorded, r.out)
	}
}
