package agentd_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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

// specRun is a branch task with spec as its spec, run by an executor running
// body; it returns the outcome, the attempt's output directory, the task's
// directory and the run's error (CUT-agentd-console-checks-2026-10-16).
func specRun(t *testing.T, spec, body string) (agentd.Outcome, string, string, error) {
	t.Helper()
	env := gitEnvT(t.TempDir())
	bare, baseCommit := bareRemote(t, env)
	head := func(_ context.Context, _, ref string) (string, error) {
		out, err := exec.Command("git", "ls-remote", bare, ref).Output()
		first, _, _ := strings.Cut(string(out), "\t")
		return strings.TrimSpace(first), err
	}
	_, _, base := platformSpec(t, store.Task{Effect: "branch", Repo: "bifrost-platform", BaseCommit: baseCommit}, head, "application/json", []byte(spec))
	d := daemon(t, base, script(body), bare, env)
	cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
	if err != nil || cl == nil {
		t.Fatalf("claim = %+v %v", cl, err)
	}
	out, err := d.Run(ctx, cl)
	dir := agentd.WorkspaceDir(d.Root, cl.TaskID, cl.AttemptNo)
	return out, dir + ".out", filepath.Dir(dir), err
}

// The sibling is beside the checkout and the setup has run before the
// executor starts: the executor finds both, and the attempt records them.
func TestRunPreparesSiblingsAndSetup(t *testing.T) {
	spec := `{"text":"t","siblings":[{"repo":"ui","ref":"main"}],` +
		`"setup":[["sh","-c","test -f ../ui/README.md && touch ../setup-ran"]]}`
	out, outDir, taskDir, err := specRun(t, spec,
		`test -f ../setup-ran && test -f ../ui/README.md && echo 'package x' > x.go && printf '# done\n' > "$OUT/report.md"`)
	if err != nil || out.Conclusion != "done" || !out.Submitted {
		t.Fatalf("run = %+v %v", out, err)
	}
	if _, err = os.Stat(filepath.Join(taskDir, "ui", "README.md")); err != nil {
		t.Fatalf("no sibling beside the checkout: %v", err)
	}
	for _, f := range []string{"siblings.json", "setup.json"} {
		if b, rerr := os.ReadFile(filepath.Join(outDir, f)); rerr != nil || len(b) == 0 {
			t.Fatalf("%s not recorded: %v", f, rerr)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(outDir, "setup.json")); !strings.Contains(string(b), `"pass": true`) {
		t.Fatalf("setup.json = %s", b)
	}
}

// A setup that fails ends the attempt before the executor starts: failed, as
// an executor that handed in nothing.
func TestRunEndsAnAttemptWhoseSetupFails(t *testing.T) {
	spec := `{"text":"t","setup":[["sh","-c","echo registry down >&2; exit 9"]]}`
	out, outDir, _, err := specRun(t, spec, `touch "$OUT/executor-ran"; printf '# done\n' > "$OUT/report.md"`)
	if err != nil || out.Conclusion != "failed" || out.ExitCode != 3 {
		t.Fatalf("run = %+v %v", out, err)
	}
	if _, err = os.Stat(filepath.Join(outDir, "executor-ran")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the executor ran after a failed setup: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(outDir, "log.txt")); !strings.Contains(string(b), "registry down") {
		t.Fatalf("the log lacks the setup's output:\n%s", b)
	}
}

// A sibling that cannot be had (no such branch) stops the run before any
// executor: as a checkout that fails.
func TestRunStopsWhenASiblingCannotBeHad(t *testing.T) {
	spec := `{"text":"t","siblings":[{"repo":"ui","ref":"no-such-branch"}]}`
	_, outDir, _, err := specRun(t, spec, `touch "$OUT/executor-ran"`)
	if err == nil || !strings.Contains(err.Error(), "sibling ui") {
		t.Fatalf("run err = %v", err)
	}
	if _, serr := os.Stat(filepath.Join(outDir, "executor-ran")); !errors.Is(serr, os.ErrNotExist) {
		t.Fatalf("the executor ran without its sibling: %v", serr)
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
// lease's grace runs: the attempt is then pending in the registry.
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
			if out.EndHow != "lease_lost" || out.Submitted || out.Salvaged || !errors.Is(err, agentd.ErrPending) {
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

// T01, takeover: what a previous agentd left is taken over before the first
// claim: a group still running is ended and reported, never run on; an
// attempt whose executor had finished hands its result over.
func TestRecover(t *testing.T) {
	pool, task, base := platform(t)
	d := daemon(t, base, nil, "", nil)
	cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
	if err != nil || cl == nil {
		t.Fatalf("claim = %+v %v", cl, err)
	}
	must(t, d.Client.Start(ctx, cl.AttemptID, cl.Epoch, "", cursor.Provider))
	dir := agentd.WorkspaceDir(d.Root, cl.TaskID, cl.AttemptNo)
	entry := agentd.Entry{AttemptID: cl.AttemptID, TaskID: cl.TaskID, AttemptNo: cl.AttemptNo, Epoch: cl.Epoch, Role: cl.Role,
		Effect: cl.EffectClass, Workdir: dir, Phase: "running"}
	p, err := agentd.Launch(os.Args[0], agentd.Spawn{Argv: []string{"sleep", "30"}, Env: append(os.Environ(), "AGENTD_TEST_LAUNCHER=1")},
		func(pgid int, start string) error {
			entry.PGID, entry.Start = pgid, start
			return d.Registry.Put(entry)
		})
	must(t, err)
	exited := make(chan int, 1)
	go func() { exited <- p.Wait() }()

	// the previous agentd is gone; a new one starts on the same registry
	next := daemon(t, base, nil, "", nil)
	next.Registry, next.Root = d.Registry, d.Root
	start := time.Now()
	must(t, next.Recover(ctx))
	select {
	case <-exited:
	case <-time.After(15 * time.Second):
		t.Fatal("the left process group outlived the takeover")
	}
	if time.Since(start) > 30*time.Second || agentd.Alive(entry.PGID, entry.Start) {
		t.Fatal("the group is still there")
	}
	var astate, endHow string
	must(t, pool.QueryRow(ctx, `SELECT state, COALESCE(end_how, '') FROM agentrt.attempt WHERE attempt_id = $1`, cl.AttemptID).Scan(&astate, &endHow))
	if state, _ := taskState(t, pool, task); state != "ready" || astate == "running" {
		t.Fatalf("task %s, attempt %s %s", state, astate, endHow)
	}
	if m, loadErr := next.Registry.Load(); loadErr != nil || len(m) != 0 {
		t.Fatalf("registry = %v %v", m, loadErr)
	}

	// the task is claimed again; this agentd's executor finishes and it dies
	// before handing over: the next one hands the result over
	cl2, err := next.Client.Claim(ctx, 1, []agentd.Tool{cursor})
	if err != nil || cl2 == nil || cl2.TaskID != task || cl2.AttemptNo != 2 {
		t.Fatalf("second claim = %+v %v", cl2, err)
	}
	must(t, next.Client.Start(ctx, cl2.AttemptID, cl2.Epoch, "", cursor.Provider))
	exit := 0
	_, err = next.Client.Computed(ctx, cl2.AttemptID, cl2.Epoch, &exit, "exit", "done", agentd.Measured{})
	must(t, err)
	dir2 := agentd.WorkspaceDir(next.Root, cl2.TaskID, cl2.AttemptNo)
	must(t, os.MkdirAll(dir2+".out", 0o700))
	must(t, os.WriteFile(filepath.Join(dir2+".out", "report.md"), []byte("# done before the restart\n"), 0o600))
	must(t, next.Registry.Put(agentd.Entry{AttemptID: cl2.AttemptID, TaskID: cl2.TaskID, AttemptNo: cl2.AttemptNo, Epoch: cl2.Epoch,
		Role: cl2.Role, Effect: cl2.EffectClass, Workdir: dir2, Phase: "computed", EndHow: "exit", ExitCode: &exit}))
	third := daemon(t, base, nil, "", nil)
	third.Registry, third.Root = next.Registry, next.Root
	must(t, third.Recover(ctx))
	if state, report := taskState(t, pool, task); state != "submitted" || report == "" {
		t.Fatalf("after the takeover: task %s, report %q", state, report)
	}
	if m, loadErr := third.Registry.Load(); loadErr != nil || len(m) != 0 {
		t.Fatalf("registry = %v %v", m, loadErr)
	}
}

// Checkpoints while the executor runs (design v5 §5.2 检查点): the log's tail
// and the workspace's patch, numbered, confirmed by the platform; a patch
// over 1 MiB is recorded as too large, not kept.
func TestRunTakesCheckpoints(t *testing.T) {
	env := gitEnvT(t.TempDir())
	bare, baseCommit := bareRemote(t, env)
	head := func(_ context.Context, _, ref string) (string, error) {
		out, err := exec.Command("git", "ls-remote", bare, ref).Output()
		first, _, _ := strings.Cut(string(out), "\t")
		return strings.TrimSpace(first), err
	}
	pool, _, base := platformWith(t, store.Task{Effect: "branch", Repo: "bifrost-platform", BaseCommit: baseCommit}, head)
	d := daemon(t, base, script(`echo 'package x' > x.go; echo working; sleep 1; head -c 2097152 /dev/urandom > big.bin; sleep 1; rm big.bin; printf '# done\n' > "$OUT/report.md"`), bare, env)
	d.CheckpointEvery = 300 * time.Millisecond
	cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
	if err != nil || cl == nil {
		t.Fatalf("claim = %+v %v", cl, err)
	}
	out, err := d.Run(ctx, cl)
	if err != nil || !out.Submitted {
		t.Fatalf("run = %+v %v", out, err)
	}
	var samples, patches, tooLarge int
	must(t, pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE (body_json->>'too_large')::boolean)
	  FROM agentrt.event WHERE event_type = 'attempt_checkpointed' AND attempt_id = $1`, cl.AttemptID).Scan(&samples, &tooLarge))
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.artifact a JOIN agentrt.artifact_blob b USING (artifact_id)
	  WHERE a.artifact_kind = 'checkpoint_patch' AND position('x.go' in convert_from(b.body, 'UTF8')) > 0`).Scan(&patches))
	if samples < 3 || patches < 1 || tooLarge < 1 {
		t.Fatalf("%d checkpoints, %d patches with x.go, %d too large", samples, patches, tooLarge)
	}
	var seqs []int
	rows, err := pool.Query(ctx, `SELECT (body_json->>'checkpoint_seq')::int FROM agentrt.event
	  WHERE event_type = 'attempt_checkpointed' AND attempt_id = $1 ORDER BY event_id`, cl.AttemptID)
	must(t, err)
	for rows.Next() {
		var n int
		must(t, rows.Scan(&n))
		seqs = append(seqs, n)
	}
	for i, n := range seqs {
		if n != i+1 {
			t.Fatalf("checkpoint numbers %v", seqs)
		}
	}
}

// Review 163 A6: a process the executor leaves behind in its group is ended
// with it, and the platform is told the group is gone only when it is.
func TestRunClearsTheGroup(t *testing.T) {
	pool, _, base := platform(t)
	d := daemon(t, base, script(`sleep 30 >/dev/null 2>&1 & echo $! > "$OUT/bg.pid"; printf '# done\n' > "$OUT/report.md"`), "", nil)
	cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
	if err != nil || cl == nil {
		t.Fatalf("claim = %+v %v", cl, err)
	}
	out, err := d.Run(ctx, cl)
	if err != nil || !out.Submitted {
		t.Fatalf("run = %+v %v", out, err)
	}
	b, err := os.ReadFile(filepath.Join(agentd.WorkspaceDir(d.Root, cl.TaskID, cl.AttemptNo)+".out", "bg.pid"))
	must(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	must(t, err)
	if len(agentd.Running([]int{pid})) != 0 {
		_ = exec.Command("kill", "-9", strconv.Itoa(pid)).Run()
		t.Fatalf("the executor's background process %d outlived its attempt", pid)
	}
	var cleared string
	must(t, pool.QueryRow(ctx, `SELECT COALESCE(body_json->>'group_cleared', '') FROM agentrt.event
	  WHERE event_type = 'attempt_ended' AND attempt_id = $1`, cl.AttemptID).Scan(&cleared))
	if cleared != "true" {
		t.Fatalf("group_cleared = %q", cleared)
	}
}

// A check prepares its subject's siblings and setup, not its own (it has none),
// and checks the subject's commit: the executor finds the sibling and the
// setup's mark, for a verify and for a review (review 227 C1).
func TestRunAChecksPreparationIsItsSubjects(t *testing.T) {
	for _, kind := range []string{"verify", "review"} {
		t.Run(kind, func(t *testing.T) {
			env := gitEnvT(t.TempDir())
			bare, baseCommit := bareRemote(t, env)
			subject := []byte(`{"text":"t","verify":[["true"]],"siblings":[{"repo":"ui","ref":"main"}],` +
				`"setup":[["sh","-c","test -f ../ui/README.md && touch ../setup-ran"]]}`)
			sum := sha256.Sum256(subject)
			subjectID := "sha256:" + hex.EncodeToString(sum[:])
			check := fmt.Sprintf(`{"text":"","check":%q,"subject_task":"t_subject","attempt":"a_subject","round":1,"task_spec":%q,"commit":%q,"repo":"bifrost-platform"}`,
				kind, subjectID, baseCommit)
			head := func(_ context.Context, _, ref string) (string, error) {
				out, err := exec.Command("git", "ls-remote", bare, ref).Output()
				first, _, _ := strings.Cut(string(out), "\t")
				return strings.TrimSpace(first), err
			}
			pool, _, base := platformSpec(t, store.Task{Effect: "branch", Repo: "bifrost-platform", BaseCommit: baseCommit}, head, "application/json", []byte(check))
			must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
				id, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "application/json", subject)
				if err == nil && id != subjectID {
					err = fmt.Errorf("subject spec stored as %s, not %s", id, subjectID)
				}
				return err
			}))
			d := daemon(t, base, script(`test "$(git rev-parse HEAD)" = "`+baseCommit+`" && test -f ../setup-ran && test -f ../ui/README.md && `+
				`echo 'package x' > x.go && printf '# done\n' > "$OUT/report.md"`), bare, env)
			cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
			if err != nil || cl == nil {
				t.Fatalf("claim = %+v %v", cl, err)
			}
			out, err := d.Run(ctx, cl)
			if err != nil || out.Conclusion != "done" {
				t.Fatalf("run = %+v %v", out, err)
			}
		})
	}
}

// What the platform writes for an implementation is what agentd reads: the
// platform repository's checks, setup and sibling all come through ParseSpec
// (agentd ships first; CUT-agentd-console-checks-2026-10-16).
func TestPlatformImplementSpecParses(t *testing.T) {
	repo := store.Repos["bifrost-platform"]
	body, err := store.ImplementSpec("title", "text", repo, repo.Verify)
	must(t, err)
	s, err := agentd.ParseSpec(body)
	if err != nil {
		t.Fatalf("agentd refuses the platform's spec: %v\n%s", err, body)
	}
	// The commands themselves, not only their count (review 227 C1): CI's checks.
	want := [][]string{
		{"go", "-C", "api", "build", "./..."},
		{"go", "-C", "api", "vet", "./..."},
		{"npm", "--prefix", "console", "run", "lint"},
		{"npm", "--prefix", "console", "test"},
		{"npm", "--prefix", "console", "run", "build"},
	}
	if fmt.Sprint(s.Verify) != fmt.Sprint(want) {
		t.Fatalf("verify = %q, want %q", s.Verify, want)
	}
	wantSetup := [][]string{
		{"npm", "--prefix", "../bifrost-ui", "ci", "--no-audit", "--no-fund"},
		{"npm", "--prefix", "console", "ci", "--no-audit", "--no-fund"},
	}
	if fmt.Sprint(s.Setup) != fmt.Sprint(wantSetup) {
		t.Fatalf("setup = %q, want %q", s.Setup, wantSetup)
	}
	if len(s.Verify) != len(repo.Verify) || len(s.Setup) != len(repo.Setup) || len(s.Siblings) != 1 ||
		s.Siblings[0] != (agentd.Sibling{Repo: "bifrost-ui", Ref: "main"}) || s.TimeoutS != store.SpecTimeoutS {
		t.Fatalf("spec = %+v", s)
	}
	for _, want := range []string{"npm --prefix console test", "Dependencies were installed before you started", "npm --prefix ../bifrost-ui ci"} {
		if !strings.Contains(s.Text, want) {
			t.Fatalf("the instructions lack %q:\n%s", want, s.Text)
		}
	}
	// A repository with no setup writes neither field (an older agentd reads it).
	plain, err := store.ImplementSpec("t", "x", store.Repo{}, [][]string{{"go", "vet"}})
	must(t, err)
	if strings.Contains(string(plain), "setup") || strings.Contains(string(plain), "siblings") || strings.Contains(string(plain), "installed before") {
		t.Fatalf("plain spec = %s", plain)
	}
}

// PROD M-3 (2026-10-17): an attempt whose executor writes its own
// configuration into the checkout fails, and the reason is in the log tail
// the platform keeps.
func TestRunFailsAnAttemptThatChangesExecutorConfig(t *testing.T) {
	out, outDir, _, err := specRun(t, `{"text":"t"}`,
		`mkdir -p .cursor/rules && echo noop > .cursor/rules/allow-shell.mdc && echo 'package x' > x.go && printf '# done\n' > "$OUT/report.md"`)
	if err != nil || out.Conclusion != "failed" || !out.Submitted {
		t.Fatalf("run = %+v %v", out, err)
	}
	log, err := os.ReadFile(filepath.Join(outDir, "log.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "executor's own configuration: .cursor/rules/allow-shell.mdc") {
		t.Fatalf("the log does not say why:\n%s", log)
	}
}
