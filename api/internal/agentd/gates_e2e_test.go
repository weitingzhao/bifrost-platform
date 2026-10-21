package agentd_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gaterun"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// gateRepo is a repository whose commit holds the gate runner as the
// platform's does (its module path, cmd/agentrt-gate and the two packages
// it uses, copied from this tree: they use the standard library only) and a
// package of two tests: one that passes, and one that needs the test
// database as agentrttest does (fatal under AGENTRT_TEST_REQUIRED=1 without
// AGENTRT_TEST_PG). It returns the bare remote and the commit.
func gateRepo(t *testing.T, env []string) (string, string) {
	t.Helper()
	bare, _ := bareRemote(t, env)
	seed := filepath.Join(t.TempDir(), "seed")
	sh(t, t.TempDir(), env, "git", "clone", "--quiet", bare, seed)
	copyGo := func(from, to string) {
		t.Helper()
		files, err := filepath.Glob(filepath.Join(from, "*.go"))
		must(t, err)
		must(t, os.MkdirAll(filepath.Join(seed, "api", to), 0o700))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			b, err := os.ReadFile(f)
			must(t, err)
			must(t, os.WriteFile(filepath.Join(seed, "api", to, filepath.Base(f)), b, 0o600))
		}
	}
	copyGo("../../cmd/agentrt-gate", "cmd/agentrt-gate")
	copyGo("../agentrt/gaterun", "internal/agentrt/gaterun")
	copyGo("../agentrt/canon", "internal/agentrt/canon")
	must(t, os.WriteFile(filepath.Join(seed, "api", "go.mod"), []byte("module github.com/weitingzhao/bifrost-platform/api\n\ngo 1.22\n"), 0o600))
	must(t, os.MkdirAll(filepath.Join(seed, "api", "internal", "probe"), 0o700))
	must(t, os.WriteFile(filepath.Join(seed, "api", "internal", "probe", "probe_test.go"), []byte(`package probe

import (
	"os"
	"testing"
)

func TestPasses(t *testing.T) {}

func TestNeedsTheDatabase(t *testing.T) {
	if os.Getenv("AGENTRT_TEST_PG") == "" {
		if os.Getenv("AGENTRT_TEST_REQUIRED") == "1" {
			t.Fatal("AGENTRT_TEST_PG is not set but AGENTRT_TEST_REQUIRED=1")
		}
		t.Skip("no test database")
	}
}
`), 0o600))
	sh(t, seed, env, "git", "add", "api")
	sh(t, seed, env, "git", "commit", "--quiet", "-m", "the runner and a probe")
	sh(t, seed, env, "git", "push", "--quiet", "origin", "HEAD:refs/heads/main")
	return bare, sh(t, seed, env, "git", "rev-parse", "HEAD")
}

// TD-397 end to end (CUT-td397-gate-runs-2026-10-20.md): two architecture
// rows each with a gate, on a test database behind the platform's routes.
// The controller records what dev runs (deploy_seen) and dispatches each
// row's gate on that commit; agentd's own deterministic executor, unchanged,
// checks the commit out and runs the gate runner there; the controller uses
// each result. The gate whose test passes ticks its row. The gate whose
// test needs the test database, which the attempt does not have (agentd
// passes no AGENTRT_TEST_PG to an executor; TD-403 will), fails: its row is
// not passed, and says the test failed, never a skip taken for a pass.
func TestGateRunsOnTheDeployedCommit(t *testing.T) {
	env := gitEnvT(t.TempDir())
	bare, commit := gateRepo(t, env)
	db := agentrttest.Migrated(t)
	_, err := store.Seed(ctx, db.Connect(t, db.Owner))
	must(t, err)
	pool := db.Pool(t, db.Runtime)
	tx := func(fn func(pgx.Tx) error) { t.Helper(); must(t, store.InTx(ctx, pool, fn)) }
	must(t, store.RegisterHost(ctx, pool, "owner", "h1", "dev"))
	_, err = pool.Exec(ctx, agentrttest.DispatchRules)
	must(t, err)
	tx(func(tx pgx.Tx) error {
		if err := store.LockCapabilities(ctx, tx); err != nil {
			return err
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: jobs"}}); err != nil {
			return err
		}
		for _, q := range []string{agentrttest.TestJobRule,
			`INSERT INTO agentrt.job (job_id, tenant, rule_id, rule_version) VALUES ('verifier', 'dev', 'job:test', 1)`,
			`INSERT INTO agentrt.job_candidate (job_candidate_id, job_id, executor_actor_id, model_provider, status, rule_id, rule_version)
			 VALUES ('verifier/exec:deterministic', 'verifier', 'exec:deterministic', 'local', 'active', 'job:test', 1)`} {
			if _, err := tx.Exec(ctx, q); err != nil {
				return err
			}
		}
		return nil
	})
	key := rptest.NewKey(t)
	envelope := "rule_id: standing-envelope:dev\nrule_version: 1\ndirection: initial\neffective_at: 2026-10-01T00:00:00Z\n" +
		"expires_at: 2027-01-15T00:00:00Z\ntenant: dev\nmax_action: branch\nbudget: {max_attempts: 40, max_wall_hours: 48}\n" +
		"executors: [exec:deterministic]\nhosts: [h1]\n"
	_, err = store.SignRule(ctx, pool, store.Writer{Actor: "owner", Token: "admin"}, rules.KindEnvelope+":dev", []byte(envelope),
		key.Sign(t, []byte(envelope), rules.Namespace), key.Fingerprint)
	must(t, err)
	gate := func(id, test string) string {
		t.Helper()
		g := gaterun.Gate{ID: id, Text: "gate " + id, Commands: []gaterun.Command{{Pkg: "./internal/probe/", Tests: []string{test}}}}
		body, sum, err := g.Spec()
		must(t, err)
		tx(func(tx pgx.Tx) error {
			_, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "application/json", body)
			return err
		})
		return `{"gates_passed":[{"test":"` + id + `","spec_sha256":"` + sum + `"}],"env":"dev"}`
	}
	rows := map[string]string{"ARCH:passes": gate("T40", "TestPasses"), "ARCH:needs-the-database": gate("T41", "TestNeedsTheDatabase")}
	tx(func(tx pgx.Tx) error {
		if _, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "ARCH", Kind: "arch_root", Label: "ARCH", Title: "架构对照",
			Root: "ARCH"}); err != nil {
			return err
		}
		i := 0
		for id := range rows {
			if _, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: id, Parent: "ARCH", Kind: "arch_row",
				Label: strings.TrimPrefix(id, "ARCH:"), Title: id, Ordinal: i, Root: "ARCH"}); err != nil {
				return err
			}
			i++
		}
		return nil
	})
	for id, rule := range rows {
		tx(func(tx pgx.Tx) error {
			if _, err := store.Append(ctx, tx, store.Event{Stream: store.PlanStream("ARCH"), Type: "plan_changed", Actor: "controller",
				Body: map[string]any{"change": "set_done_rule"}}); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = $2::jsonb WHERE plan_node_id = $1`, id, rule)
			return err
		})
	}

	// The platform's routes, and the controller of dev reading what dev runs.
	authPath := filepath.Join(t.TempDir(), "auth.yaml")
	must(t, os.WriteFile(authPath, []byte("tokens:\n  - name: agentd:h1\n    role: agentd\n    token: fixture-agentd-h1\n"), 0o600))
	auth, err := actuation.LoadAuth(authPath)
	must(t, err)
	api := &agentrtapi.API{DB: pool, ClaimWait: 300 * time.Millisecond, ClaimPoll: 50 * time.Millisecond}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { agentrtapi.Mount(r, auth, api) })
	s := httptest.NewServer(r)
	t.Cleanup(s.Close)
	ref := map[string]any{"delivery_run": "bifrost-deliver-platform-7", "images": map[string]any{"platform-api": "sha256:7a"}}
	c := &controller.Controller{DB: pool, Env: "dev", GateRepo: "bifrost-platform", Sources: controller.Sources{
		Deployed: func(context.Context, string) (string, map[string]any, bool) { return commit, ref, true }}}
	if _, err := c.Facts(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := c.Gates(ctx); err != nil || n != 2 {
		t.Fatalf("the controller dispatched %d gates (%v)", n, err)
	}

	// agentd runs each with its own deterministic executor (the test binary
	// is agentd, as its launcher, when told so).
	prod := agentd.Executors(os.Args[0], append(os.Environ(), "AGENTD_TEST_LAUNCHER=1"), agentd.Secrets{})["exec:deterministic"]
	d := daemon(t, s.URL+"/api/v1/agentrt", nil, bare, env)
	d.Executors["exec:deterministic"] = func(a *agentd.Attempt) (agentd.Spawn, error) {
		sp, perr := prod(a)
		sp.Env = append(sp.Env, "AGENTD_TEST_LAUNCHER=1")
		return sp, perr
	}
	for i := 0; i < 2; i++ {
		cl, cerr := d.Client.Claim(ctx, 1, []agentd.Tool{{Executor: "exec:deterministic", Provider: "local"}})
		if cerr != nil || cl == nil || cl.Role != "verify" || cl.BaseCommit != commit {
			t.Fatalf("claim %d of a gate: %+v %v", i, cl, cerr)
		}
		out, rerr := d.Run(ctx, cl)
		if rerr != nil || !out.Submitted {
			t.Fatalf("run %d of a gate: %+v %v", i, out, rerr)
		}
	}
	if n, err := c.Accept(ctx); err != nil || n != 2 {
		t.Fatalf("the controller used %d gate results (%v)", n, err)
	}

	progress, err := store.Progress(ctx, pool)
	must(t, err)
	raw, err := json.Marshal(progress)
	must(t, err)
	var doc struct {
		Plan []struct {
			PlanNodeID string           `json:"plan_node_id"`
			Rows       []map[string]any `json:"rows"`
		} `json:"plan"`
	}
	must(t, json.Unmarshal(raw, &doc))
	ticks := map[string][2]string{}
	for _, p := range doc.Plan {
		for _, row := range p.Rows {
			ticks[row["plan_node_id"].(string)] = [2]string{row["tick"].(string), row["reason"].(string)}
		}
	}
	if got := ticks["ARCH:passes"]; got[0] != "true" || !strings.Contains(got[1], "T40 passed on dev at "+commit[:12]) {
		t.Fatalf("the passing gate's row: %v", got)
	}
	if got := ticks["ARCH:needs-the-database"]; got[0] != "false" ||
		!strings.Contains(got[1], "T41 (failed: FAIL ./internal/probe/: TestNeedsTheDatabase it failed)") {
		t.Fatalf("the gate that needs the test database: %v", got)
	}
}
