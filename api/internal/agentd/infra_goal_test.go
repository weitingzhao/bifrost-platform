package agentd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// fakePython is python3 for the infra goal test: what the setup and the
// check ask of it, recorded in log, so that nothing is downloaded. Making
// the environment copies it into .git/venv/bin, as the environment's own
// python3.
const fakePython = `#!/bin/sh
log='LOG'
case "$*" in
"-I -m venv --clear .git/venv")
  mkdir -p .git/venv/bin && cp "$0" .git/venv/bin/python3 && echo "venv|$(pwd -P)" >> "$log" ;;
"-I -m pip --isolated install "*)
  echo "pip|$(pwd -P)|$0|$*" >> "$log" ;;
"-I -m pytest "*)
  first=other; [ "${PATH%%:*}" = "$PWD/.git/venv/bin" ] && first=venv
  cache=other; case "$GOCACHE" in */go-build) cache=go-build ;; esac
  echo "pytest|$(pwd -P)|$(git rev-parse HEAD)|$first|$cache|$0|$*" >> "$log" ;;
*)
  echo "unexpected|$*" >> "$log"; exit 2 ;;
esac
`

// An infra goal end to end (CUT-runtime-repo-infra-2026-10-19), on a test
// database behind the platform's routes: POST /goals names
// bifrost-trade-infra; the planner's plan is recorded; its implementation's
// spec is infra's; agentd runs the setup in the checkout before the executor
// and pushes the change without the environment; the deterministic check
// prepares the subject's setup on the pushed commit and runs infra's
// command there, the environment first on PATH; the check passes and the
// implementation goes to review.
func TestInfraGoalToCheck(t *testing.T) {
	const infra = "bifrost-trade-infra"
	env := gitEnvT(t.TempDir())
	bare, baseCommit := bareRemote(t, env)
	lsRemote := func(ref string) (string, error) {
		out, err := exec.Command("git", "ls-remote", bare, ref).Output()
		first, _, _ := strings.Cut(string(out), "\t")
		return strings.TrimSpace(first), err
	}
	db := agentrttest.Migrated(t)
	_, seedErr := store.Seed(ctx, db.Connect(t, db.Owner))
	must(t, seedErr)
	pool := db.Pool(t, db.Runtime)
	tx := func(fn func(pgx.Tx) error) { t.Helper(); must(t, store.InTx(ctx, pool, fn)) }
	must(t, store.RegisterHost(ctx, pool, "owner", "h1", "dev"))
	_, rulesErr := pool.Exec(ctx, agentrttest.DispatchRules)
	must(t, rulesErr)
	tx(func(tx pgx.Tx) error {
		if err := store.LockCapabilities(ctx, tx); err != nil {
			return err
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: jobs"}}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, agentrttest.TestJobRule); err != nil {
			return err
		}
		for job, tool := range map[string]store.Tool{"planner": {Executor: "exec:cursor", Provider: "cursor-mix"},
			"implementer": {Executor: "exec:cursor", Provider: "cursor-mix"}, "verifier": {Executor: "exec:deterministic", Provider: "local"}} {
			if _, err := tx.Exec(ctx, `INSERT INTO agentrt.job (job_id, tenant, rule_id, rule_version) VALUES ($1, 'dev', 'job:test', 1)`, job); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO agentrt.job_candidate (job_candidate_id, job_id, executor_actor_id, model_provider, status,
			  rule_id, rule_version) VALUES ($1, $2, $3, $4, 'active', 'job:test', 1)`, job+"/"+tool.Executor, job, tool.Executor, tool.Provider); err != nil {
				return err
			}
		}
		return nil
	})
	authPath := filepath.Join(t.TempDir(), "auth.yaml")
	must(t, os.WriteFile(authPath, []byte("tokens:\n  - name: agentd:h1\n    role: agentd\n    token: fixture-agentd-h1\n"+
		"  - name: admin\n    role: admin\n    token: fixture-admin-1\n"), 0o600))
	auth, err := actuation.LoadAuth(authPath)
	must(t, err)
	var mains []string
	api := &agentrtapi.API{DB: pool, ClaimWait: 300 * time.Millisecond, ClaimPoll: 50 * time.Millisecond,
		Head: func(_ context.Context, _, ref string) (string, error) { return lsRemote(ref) },
		MainHead: func(_ context.Context, repo string) (string, error) {
			mains = append(mains, repo)
			return lsRemote("refs/heads/main")
		}}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { agentrtapi.Mount(r, auth, api) })
	s := httptest.NewServer(r)
	t.Cleanup(s.Close)

	// The goal, as the Console sends it.
	body, err := json.Marshal(map[string]string{"idempotency_key": "g1", "goal": "Test the T26 probe's summary", "repo": infra})
	must(t, err)
	req, err := http.NewRequest("POST", s.URL+"/api/v1/agentrt/goals", bytes.NewReader(body))
	must(t, err)
	req.Header.Set("Authorization", "Bearer fixture-admin-1")
	resp, err := http.DefaultClient.Do(req)
	must(t, err)
	var goal map[string]any
	must(t, json.NewDecoder(resp.Body).Decode(&goal))
	_ = resp.Body.Close()
	mission, _ := goal["mission_id"].(string)
	if resp.StatusCode != http.StatusOK || goal["repo"] != infra || goal["base_commit"] != baseCommit || !slices.Equal(mains, []string{infra}) {
		t.Fatalf("the goal: %d %v (main read for %v)", resp.StatusCode, goal, mains)
	}
	tx(func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", mission, "active", "test") })
	_, err = store.Dispatch(ctx, pool)
	must(t, err)

	// The fake python3 first on PATH for every process agentd starts.
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "python.log")
	must(t, os.WriteFile(filepath.Join(bin, "python3"), []byte(strings.Replace(fakePython, "LOG", log, 1)), 0o755))
	procEnv := func(extra ...string) []string {
		out := []string{"AGENTD_TEST_LAUNCHER=1"}
		for _, kv := range os.Environ() {
			if k, v, _ := strings.Cut(kv, "="); k == "PATH" {
				kv = "PATH=" + bin + string(os.PathListSeparator) + v
			}
			out = append(out, kv)
		}
		return append(out, extra...)
	}
	planFile := filepath.Join(t.TempDir(), "plan.md")
	must(t, os.WriteFile(planFile, []byte("One change.\n\n```json\n"+`{"mission": {"success": ["the probe's summary has a test"]},
  "tasks": [{"ref": "t1", "title": "Test the probe's summary", "repo": "bifrost-trade-infra", "text": "Add scripts/agentrt/test_summary.py.", "verify": []}]}`+
		"\n```\n"), 0o600))
	cursorScript := func(script string) agentd.Executor {
		return func(a *agentd.Attempt) (agentd.Spawn, error) {
			return agentd.Spawn{Argv: []string{"sh", "-c", script}, Dir: a.Dir, Env: procEnv("OUT="+a.Out, "LOG="+log)}, nil
		}
	}
	// agentd's own deterministic executor (the test binary is agentd, as
	// its launcher, when it is told so).
	prod := agentd.Executors(os.Args[0], procEnv(), agentd.Secrets{})["exec:deterministic"]
	deterministic := func(a *agentd.Attempt) (agentd.Spawn, error) {
		sp, perr := prod(a)
		sp.Env = append(sp.Env, "AGENTD_TEST_LAUNCHER=1")
		return sp, perr
	}
	d := daemon(t, s.URL+"/api/v1/agentrt", nil, bare, env)
	d.Executors["exec:deterministic"] = deterministic
	run := func(tool agentd.Tool, role string, ex agentd.Executor) *agentd.Claimed {
		t.Helper()
		d.Executors["exec:cursor"] = ex
		cl, cerr := d.Client.Claim(ctx, 1, []agentd.Tool{tool})
		if cerr != nil || cl == nil || cl.Role != role || cl.Repo != infra {
			t.Fatalf("claim of the %s: %+v %v", role, cl, cerr)
		}
		out, rerr := d.Run(ctx, cl)
		if rerr != nil || !out.Submitted || out.Conclusion != "done" {
			t.Fatalf("run of the %s: %+v %v", role, out, rerr)
		}
		return cl
	}

	// The planner hands in the plan; the controller records it.
	plan := run(cursor, "plan", cursorScript(`cat "`+planFile+`" > "$OUT/report.md"`))
	var cards []store.Card
	ask := func(_ context.Context, c store.Card) (string, error) { cards = append(cards, c); return "q", nil }
	if did, aerr := store.AcceptPlan(ctx, pool, plan.TaskID, ask, func(context.Context, string, string) error { return nil }); aerr != nil || did != "accepted" {
		t.Fatalf("accept: %q %v", did, aerr)
	}
	_, err = store.Dispatch(ctx, pool)
	must(t, err)

	// The implementation: its spec is infra's; the setup made the
	// environment before the executor started; the change is pushed
	// without it.
	impl := run(cursor, "implement", cursorScript(`test -x .git/venv/bin/python3 && grep -q '^pip|' "$LOG" && mkdir -p scripts/agentrt && `+
		`printf 'def test_summary():\n    pass\n' > scripts/agentrt/test_summary.py && printf '# done\n' > "$OUT/report.md"`))
	spec, err := d.Client.GetArtifact(ctx, impl.SpecArtifact)
	must(t, err)
	ts, err := agentd.ParseSpec(spec)
	must(t, err)
	repo := store.Repos[infra]
	if !slices.EqualFunc(ts.Verify, repo.Verify, slices.Equal) || !slices.EqualFunc(ts.Setup, repo.Setup, slices.Equal) || len(ts.Siblings) != 0 {
		t.Fatalf("the implementation's spec: %+v", ts)
	}
	pushed, err := lsRemote(impl.PushedRef)
	must(t, err)
	if files := sh(t, t.TempDir(), env, "git", "--git-dir", bare, "diff", "--name-only", baseCommit, pushed); files != "scripts/agentrt/test_summary.py" {
		t.Fatalf("the pushed change: %q", files)
	}

	// The check: the subject's setup, then infra's command on the pushed
	// commit, from the checkout, the environment first on PATH.
	check, err := store.StartVerification(ctx, pool, impl.TaskID)
	if err != nil || check == "" {
		t.Fatalf("verification: %q %v", check, err)
	}
	cl := run(agentd.Tool{Executor: "exec:deterministic", Provider: "local"}, "verify", nil)
	if cl.TaskID != check {
		t.Fatalf("claimed %s, not the check %s", cl.TaskID, check)
	}
	v, err := os.ReadFile(filepath.Join(agentd.WorkspaceDir(d.Root, cl.TaskID, cl.AttemptNo)+".out", "verify.json"))
	must(t, err)
	var verification agentd.Verification
	must(t, json.Unmarshal(v, &verification))
	if !verification.Pass || len(verification.Commands) != 1 || !slices.Equal(verification.Commands[0].Argv, repo.Verify[0]) {
		t.Fatalf("verify.json: %s", v)
	}
	raw, err := os.ReadFile(log)
	must(t, err)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	implDir, err := filepath.EvalSymlinks(agentd.WorkspaceDir(d.Root, impl.TaskID, impl.AttemptNo))
	must(t, err)
	checkDir, err := filepath.EvalSymlinks(agentd.WorkspaceDir(d.Root, cl.TaskID, cl.AttemptNo))
	must(t, err)
	pip := strings.Join(repo.Setup[1][1:], " ")
	_, pytest, _ := strings.Cut(repo.Verify[0][2], "exec python3 ")
	want := []string{"venv|" + implDir, "pip|" + implDir + "|.git/venv/bin/python3|" + pip, "venv|" + checkDir,
		"pip|" + checkDir + "|.git/venv/bin/python3|" + pip,
		"pytest|" + checkDir + "|" + pushed + "|venv|go-build|" + checkDir + "/.git/venv/bin/python3|" + pytest}
	if !slices.Equal(lines, want) {
		t.Fatalf("python3 was asked:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if did, err := store.Conclude(ctx, pool, check); err != nil || did != "review" {
		t.Fatalf("conclude: %q %v", did, err)
	}
	if len(cards) != 0 {
		t.Fatalf("the plan asked %v", cards)
	}
}
