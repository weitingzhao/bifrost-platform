package agentd_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func gitEnvT(home string) []string {
	return append(os.Environ(), "HOME="+home, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=agentd", "GIT_AUTHOR_EMAIL=agentd@h1", "GIT_COMMITTER_NAME=agentd", "GIT_COMMITTER_EMAIL=agentd@h1")
}

func sh(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// bareRemote is a bare repository with one commit; it returns its path and
// that commit.
func bareRemote(t *testing.T, env []string) (string, string) {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	sh(t, root, env, "git", "init", "--quiet", "--bare", "--initial-branch=main", bare)
	seed := filepath.Join(root, "seed")
	sh(t, root, env, "git", "clone", "--quiet", bare, seed)
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("# repo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sh(t, seed, env, "git", "add", "README.md")
	sh(t, seed, env, "git", "commit", "--quiet", "-m", "first")
	sh(t, seed, env, "git", "push", "--quiet", "origin", "HEAD:refs/heads/main")
	return bare, sh(t, seed, env, "git", "rev-parse", "HEAD")
}

// script is an executor that runs a shell script in the workspace with OUT
// set to where results go (and the test binary as the launcher).
func script(body string) agentd.Executor {
	return func(a *agentd.Attempt) (agentd.Spawn, error) {
		return agentd.Spawn{Argv: []string{"sh", "-c", body}, Dir: a.Dir,
			Env: append(os.Environ(), "OUT="+a.Out, "AGENTD_TEST_LAUNCHER=1")}, nil
	}
}

func daemon(t *testing.T, base string, exec agentd.Executor, remote string, env []string) *agentd.Daemon {
	t.Helper()
	root := t.TempDir()
	reg, err := agentd.OpenRegistry(filepath.Join(root, "registry.json"))
	must(t, err)
	return &agentd.Daemon{
		Client:    &agentd.Client{Base: base, Host: "h1", Token: "fixture-agentd-h1"},
		Registry:  reg,
		Root:      root,
		Self:      os.Args[0],
		Executors: map[string]agentd.Executor{"exec:cursor": exec},
		Remote:    func(string) string { return remote },
		GitEnv:    env,
		Beat:      100 * time.Millisecond, Retry: 50 * time.Millisecond, SalvageEvery: 50 * time.Millisecond, KillGrace: time.Second,
		Logf: t.Logf,
	}
}

func taskState(t *testing.T, pool *pgxpool.Pool, task string) (state, report string) {
	t.Helper()
	must(t, pool.QueryRow(ctx, `SELECT t.state, COALESCE(a.report_artifact_id, '') FROM agentrt.task t
	  JOIN agentrt.attempt a ON a.task_id = t.task_id WHERE t.task_id = $1 ORDER BY a.attempt_no DESC LIMIT 1`, task).Scan(&state, &report))
	return state, report
}

// A task that pushes nothing: claimed, run, its report and log handed over,
// submitted, and forgotten on the host.
func TestRunAnAttempt(t *testing.T) {
	pool, task, base := platform(t)
	d := daemon(t, base, script(`echo compiling; printf '# done\nall good\n' > "$OUT/report.md"`), "", nil)
	cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
	if err != nil || cl == nil {
		t.Fatalf("claim = %+v %v", cl, err)
	}
	out, err := d.Run(ctx, cl)
	if err != nil || out.EndHow != "exit" || out.Conclusion != "done" || !out.Submitted {
		t.Fatalf("run = %+v %v", out, err)
	}
	if state, report := taskState(t, pool, task); state != "submitted" || report == "" {
		t.Fatalf("task %s, report %q", state, report)
	}
	var logs int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.artifact WHERE artifact_kind = 'log_tail'`).Scan(&logs))
	if logs != 1 {
		t.Fatalf("%d log tails", logs)
	}
	if m, err := d.Registry.Load(); err != nil || len(m) != 0 {
		t.Fatalf("registry after the end = %v %v", m, err)
	}
	if _, err := d.Run(ctx, cl); !errors.Is(err, agentd.ErrWorkspaceExists) {
		t.Fatalf("the same attempt ran twice: %v", err)
	}
}

// A branch task: the change is committed with its lineage trailers and
// pushed to the attempt's own branch, and the platform checks the head.
func TestRunPushesTheAttemptBranch(t *testing.T) {
	env := gitEnvT(t.TempDir())
	bare, baseCommit := bareRemote(t, env)
	head := func(_ context.Context, repo, ref string) (string, error) {
		if repo != "bifrost-platform" {
			return "", errors.New("another repository")
		}
		out, err := exec.Command("git", "ls-remote", bare, ref).Output()
		first, _, _ := strings.Cut(string(out), "\t")
		return strings.TrimSpace(first), err
	}
	pool, task, base := platformWith(t, store.Task{Effect: "branch", Repo: "bifrost-platform", BaseCommit: baseCommit}, head)
	d := daemon(t, base, script(`echo 'package x' > x.go; printf '# done\n' > "$OUT/report.md"`), bare, env)
	cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
	if err != nil || cl == nil || cl.WorkID != "W-31" {
		t.Fatalf("claim = %+v %v", cl, err)
	}
	out, err := d.Run(ctx, cl)
	if err != nil || !out.Submitted {
		t.Fatalf("run = %+v %v", out, err)
	}
	pushed := sh(t, t.TempDir(), env, "git", "ls-remote", bare, cl.PushedRef)
	commit, _, _ := strings.Cut(pushed, "\t")
	msg := sh(t, t.TempDir(), env, "git", "--git-dir", bare, "log", "-1", "--format=%B", commit)
	for _, want := range []string{"Agentrt-Task: " + task, "Change-Id: I", "Work: W-31"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("pushed commit lacks %q:\n%s", want, msg)
		}
	}
	var recorded string
	must(t, pool.QueryRow(ctx, `SELECT COALESCE(pushed_commit, '') FROM agentrt.attempt WHERE attempt_id = $1`, cl.AttemptID).Scan(&recorded))
	if recorded != commit {
		t.Fatalf("the platform recorded %q, the branch is at %q", recorded, commit)
	}
	if main := sh(t, t.TempDir(), env, "git", "ls-remote", bare, "refs/heads/main"); !strings.HasPrefix(main, baseCommit) {
		t.Fatalf("main moved: %s", main)
	}
}

// fencingProxy forwards to the platform until told to answer heartbeats
// itself: 409 fenced, or 503 as an unreachable platform would.
func fencingProxy(t *testing.T, base string) (string, *atomic.Int32) {
	t.Helper()
	target, err := url.Parse(base)
	must(t, err)
	rp := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: target.Scheme, Host: target.Host})
	var mode atomic.Int32 // 0 forward, 409 or 503
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if code := int(mode.Load()); code != 0 && strings.HasSuffix(r.URL.Path, "/heartbeat") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"error":"fenced","message":"test"}`))
			return
		}
		rp.ServeHTTP(w, r)
	}))
	t.Cleanup(s.Close)
	return s.URL + target.Path, &mode
}

// The dead man's switch: once the platform says the lease is not ours, or
// heartbeats fail three times running, the process group is ended and what
// it left goes only to salvage, which the platform refuses while the
// lease's grace runs.
func TestRunLosesItsLease(t *testing.T) {
	for _, code := range []int32{http.StatusConflict, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			pool, task, base := platform(t)
			proxied, mode := fencingProxy(t, base)
			d := daemon(t, proxied, script(`printf '# partial\n' > "$OUT/report.md"; exec sleep 30`), "", nil)
			cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
			if err != nil || cl == nil {
				t.Fatalf("claim = %+v %v", cl, err)
			}
			time.AfterFunc(300*time.Millisecond, func() { mode.Store(code) })
			runCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			start := time.Now()
			out, err := d.Run(runCtx, cl)
			if out.EndHow != "lease_lost" || out.Submitted || out.Salvaged || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("run = %+v %v", out, err)
			}
			if time.Since(start) > 10*time.Second {
				t.Fatalf("the executor outlived its lease: %s", time.Since(start))
			}
			m, err := d.Registry.Load()
			e := m[cl.AttemptID]
			if err != nil || e.Phase != "lost" || e.PGID == 0 || agentd.Alive(e.PGID, e.Start) {
				t.Fatalf("registry = %+v %v", e, err)
			}
			if state, _ := taskState(t, pool, task); state == "submitted" {
				t.Fatal("a lost attempt submitted")
			}
		})
	}
}
