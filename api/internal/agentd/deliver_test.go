package agentd_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// faults forwards to the platform, but answers a route (by its last path
// element) with a status the test sets, as a platform that is down (503) or
// refuses (409 fenced) would.
type faults struct {
	mu    sync.Mutex
	codes map[string]int
}

func (f *faults) set(route string, code int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes[route] = code
}

func faultProxy(t *testing.T, base string) (string, *faults) {
	t.Helper()
	target, err := url.Parse(base)
	must(t, err)
	rp := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: target.Scheme, Host: target.Host})
	f := &faults{codes: map[string]int{}}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		code := f.codes[r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]]
		f.mu.Unlock()
		if code != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"error":"fenced","message":"test"}`))
			return
		}
		rp.ServeHTTP(w, r)
	}))
	t.Cleanup(s.Close)
	return s.URL + target.Path, f
}

type branchWorld struct {
	pool             *pgxpool.Pool
	task, baseCommit string
	base, bare       string
	env              []string
}

func branchPlatform(t *testing.T) branchWorld {
	t.Helper()
	env := gitEnvT(t.TempDir())
	bare, baseCommit := bareRemote(t, env)
	head := func(_ context.Context, _, ref string) (string, error) {
		out, err := exec.Command("git", "ls-remote", bare, ref).Output()
		first, _, _ := strings.Cut(string(out), "\t")
		return strings.TrimSpace(first), err
	}
	pool, task, base := platformWith(t, store.Task{Effect: "branch", Repo: "bifrost-platform", BaseCommit: baseCommit}, head)
	return branchWorld{pool: pool, task: task, baseCommit: baseCommit, base: base, bare: bare, env: env}
}

// Review 163 A3: commits the executor made itself are delivered, folded into
// one commit with the trailers.
func TestRunFoldsTheExecutorsCommits(t *testing.T) {
	w := branchPlatform(t)
	task, baseCommit, bare, env := w.task, w.baseCommit, w.bare, w.env
	d := daemon(t, w.base, script(`echo one > a.txt; git add a.txt; git commit -q -m "executor's own"; printf '# done\n' > "$OUT/report.md"`), bare, env)
	d.Executors["exec:cursor"] = withEnv(d.Executors["exec:cursor"], env)
	cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
	if err != nil || cl == nil {
		t.Fatalf("claim = %+v %v", cl, err)
	}
	if out, err := d.Run(ctx, cl); err != nil || !out.Submitted {
		t.Fatalf("run = %+v %v", out, err)
	}
	pushed, _, _ := strings.Cut(sh(t, t.TempDir(), env, "git", "ls-remote", bare, cl.PushedRef), "\t")
	msg := sh(t, t.TempDir(), env, "git", "--git-dir", bare, "log", "-1", "--format=%B", pushed)
	files := sh(t, t.TempDir(), env, "git", "--git-dir", bare, "diff", "--name-only", baseCommit, pushed)
	parent := sh(t, t.TempDir(), env, "git", "--git-dir", bare, "rev-parse", pushed+"^")
	if !strings.Contains(msg, "Agentrt-Task: "+task) || files != "a.txt" || parent != baseCommit {
		t.Fatalf("pushed %s on %s with %q:\n%s", pushed, parent, files, msg)
	}
}

func withEnv(e agentd.Executor, env []string) agentd.Executor {
	return func(a *agentd.Attempt) (agentd.Spawn, error) {
		sp, err := e(a)
		sp.Env = append(sp.Env, env...)
		return sp, err
	}
}

// Review 163 A4, A3: a platform that cannot take the result leaves it in the
// journal, the attempt not ended; the next takeover pushes the same commit
// and hands the result over.
func TestHandOverWaitsForThePlatform(t *testing.T) {
	w := branchPlatform(t)
	task, bare, env := w.task, w.bare, w.env
	proxied, f := faultProxy(t, w.base)
	d := daemon(t, proxied, script(`echo x > x.go; printf '# done\n' > "$OUT/report.md"`), bare, env)
	d.Retry = 10 * time.Millisecond
	cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
	if err != nil || cl == nil {
		t.Fatalf("claim = %+v %v", cl, err)
	}
	f.set("persist", http.StatusServiceUnavailable)
	out, err := d.Run(ctx, cl)
	if !errors.Is(err, agentd.ErrPending) || out.Submitted {
		t.Fatalf("run = %+v %v", out, err)
	}
	m, err := d.Registry.Load()
	e := m[cl.AttemptID]
	if err != nil || e.Phase != "computed" || !e.Prepared || len(e.PushedCommit) != 40 {
		t.Fatalf("journal = %+v %v", e, err)
	}
	f.set("persist", 0)
	next := daemon(t, proxied, nil, bare, env)
	next.Registry, next.Root = d.Registry, d.Root
	must(t, next.Recover(ctx))
	pushed, _, _ := strings.Cut(sh(t, t.TempDir(), env, "git", "ls-remote", bare, cl.PushedRef), "\t")
	if state, report := taskState(t, w.pool, task); state != "submitted" || report == "" || pushed != e.PushedCommit {
		t.Fatalf("after the takeover: task %s, report %q, branch %s, journal %s", state, report, pushed, e.PushedCommit)
	}
}

// Review 163 A5: a lease lost during the hand-over goes to salvage, which
// waits while the attempt is in its grace: pending, kept in the journal for
// the next takeover, not retried in a loop that holds up the host (review
// 164 A8).
func TestHandOverLosesItsLease(t *testing.T) {
	_, _, base := platform(t)
	proxied, f := faultProxy(t, base)
	d := daemon(t, proxied, script(`printf '# done\n' > "$OUT/report.md"`), "", nil)
	cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
	if err != nil || cl == nil {
		t.Fatalf("claim = %+v %v", cl, err)
	}
	f.set("computed", http.StatusConflict)
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := d.Run(runCtx, cl)
	m, _ := d.Registry.Load()
	if out.EndHow != "lease_lost" || out.Submitted || !errors.Is(err, agentd.ErrPending) || m[cl.AttemptID].Phase != "lost" ||
		m[cl.AttemptID].Report == "" {
		t.Fatalf("run = %+v %v; journal %+v", out, err, m[cl.AttemptID])
	}
}

// Review 163 A7: an executor is released only once the platform knows it
// started; one whose start was never taken never runs.
func TestStartBeforeRelease(t *testing.T) {
	pool, task, base := platform(t)
	proxied, f := faultProxy(t, base)
	marker := filepath.Join(t.TempDir(), "ran")
	d := daemon(t, proxied, script(`touch `+marker), "", nil)
	d.Retry = 10 * time.Millisecond
	cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
	if err != nil || cl == nil {
		t.Fatalf("claim = %+v %v", cl, err)
	}
	f.set("start", http.StatusServiceUnavailable)
	if _, err := d.Run(ctx, cl); err == nil {
		t.Fatal("ran though its start was never taken")
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the executor ran before the platform knew it started")
	}
	if state, _ := taskState(t, pool, task); state == "running" {
		t.Fatalf("task %s", state)
	}
}

// The platform's stop and the task's hard limit end the executor's group.
func TestStopAndHardLimit(t *testing.T) {
	pool, task, base := platform(t)
	d := daemon(t, base, script(`exec sleep 30`), "", nil)
	cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
	if err != nil || cl == nil {
		t.Fatalf("claim = %+v %v", cl, err)
	}
	time.AfterFunc(300*time.Millisecond, func() {
		_ = store.InTx(ctx, pool, func(tx pgx.Tx) error {
			return store.SetControl(ctx, tx, "owner", "c-stop-1", "task", task, "stop", "test")
		})
	})
	start := time.Now()
	out, err := d.Run(ctx, cl)
	if out.EndHow != "cancelled" || out.Conclusion != "none" || time.Since(start) > 15*time.Second {
		t.Fatalf("stopped = %+v %v after %s", out, err, time.Since(start))
	}

	pool2, task2, base2 := platform(t)
	must(t, store.InTx(ctx, pool2, func(tx pgx.Tx) error {
		if _, aerr := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: a one-second limit"}}); aerr != nil {
			return aerr
		}
		_, xerr := tx.Exec(ctx, `UPDATE agentrt.task SET hard_limit_s = 1 WHERE task_id = $1`, task2)
		return xerr
	}))
	d2 := daemon(t, base2, script(`exec sleep 30`), "", nil)
	cl2, err := d2.Client.Claim(ctx, 1, []agentd.Tool{cursor})
	if err != nil || cl2 == nil || cl2.HardLimitS != 1 {
		t.Fatalf("claim = %+v %v", cl2, err)
	}
	start = time.Now()
	out, err = d2.Run(ctx, cl2)
	if out.EndHow != "hard_limit" || out.Conclusion != "failed" || time.Since(start) > 15*time.Second {
		t.Fatalf("over the limit = %+v %v after %s", out, err, time.Since(start))
	}
}
