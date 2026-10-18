package agentd_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

var ctx = context.Background()

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// platform is a migrated database with an active mission and step, host h1,
// the implementer job with cursor as its candidate, one ready task of effect
// none, and the agentrt routes behind the platform's authentication.
func platform(t *testing.T) (*pgxpool.Pool, string, string) {
	t.Helper()
	return platformWith(t, store.Task{Effect: "none"}, nil)
}

// platformWith is platform with the task's effect, repository and base
// commit as given, and a branch head reader for persist.
func platformWith(t *testing.T, shape store.Task, head store.BranchHead) (*pgxpool.Pool, string, string) {
	t.Helper()
	return platformSpec(t, shape, head, "text/markdown", []byte("# task\n"))
}

// platformSpec is platformWith with the task's spec as given.
func platformSpec(t *testing.T, shape store.Task, head store.BranchHead, media string, specBody []byte) (*pgxpool.Pool, string, string) {
	t.Helper()
	db := agentrttest.Migrated(t)
	_, seedErr := store.Seed(ctx, db.Connect(t, db.Owner))
	must(t, seedErr)
	pool := db.Pool(t, db.Runtime)
	tx := func(fn func(pgx.Tx) error) { t.Helper(); must(t, store.InTx(ctx, pool, fn)) }
	tx(func(tx pgx.Tx) error {
		if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: "M-1", WorkID: "W-31", Title: "m", Goal: "g", Deadline: time.Now().Add(time.Hour)}); err != nil {
			return err
		}
		return store.MoveMission(ctx, tx, "controller", "M-1", "active", "test")
	})
	for _, n := range []store.PlanNode{
		{ID: "R2", Kind: "round", Label: "2", Title: "round", Root: "R2"},
		{ID: "R2:1", Parent: "R2", Kind: "item", Label: "1", Title: "item", Root: "R2"},
		{ID: "R2:1-1", Parent: "R2:1", Kind: "step", Label: "1-1", Title: "step", MissionID: "M-1", StepKind: "agent", Root: "R2"},
	} {
		tx(func(tx pgx.Tx) error { _, err := store.CreatePlanNode(ctx, tx, "controller", n); return err })
	}
	must(t, store.RegisterHost(ctx, pool, "owner", "h1", "dev"))
	_, ferr := pool.Exec(ctx, agentrttest.DispatchRules)
	must(t, ferr)
	tx(func(tx pgx.Tx) error {
		if err := store.LockCapabilities(ctx, tx); err != nil {
			return err
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: jobs"}}); err != nil {
			return err
		}
		for _, q := range []string{
			agentrttest.TestJobRule,
			`INSERT INTO agentrt.job (job_id, tenant, rule_id, rule_version) VALUES ('implementer', 'dev', 'job:test', 1)`,
			`INSERT INTO agentrt.job_candidate (job_candidate_id, job_id, executor_actor_id, model_provider, status, rule_id, rule_version)
			 VALUES ('implementer/exec:cursor/cursor-mix', 'implementer', 'exec:cursor', 'cursor-mix', 'active', 'job:test', 1)`,
		} {
			if _, err := tx.Exec(ctx, q); err != nil {
				return err
			}
		}
		return nil
	})
	task := store.NewTaskID()
	var spec string
	tx(func(tx pgx.Tx) error {
		var err error
		if spec, err = store.PutArtifact(ctx, tx, "controller", "task_spec", media, specBody); err != nil {
			return err
		}
		if _, err = store.ProposeTask(ctx, tx, "controller", store.Task{ID: task, MissionID: "M-1", PlanNode: "R2:1-1", Role: "implement",
			Class: "go", Title: "t", SpecArtifact: spec, Effect: shape.Effect, Repo: shape.Repo, BaseCommit: shape.BaseCommit}); err != nil {
			return err
		}
		return store.MarkReady(ctx, tx, "controller", task)
	})

	path := filepath.Join(t.TempDir(), "auth.yaml")
	must(t, os.WriteFile(path, []byte("tokens:\n  - name: agentd:h1\n    role: agentd\n    token: fixture-agentd-h1\n  - name: agentd:h2\n    role: agentd\n    token: fixture-agentd-h2\n"), 0o600))
	auth, err := actuation.LoadAuth(path)
	must(t, err)
	api := &agentrtapi.API{DB: pool, ClaimWait: 300 * time.Millisecond, ClaimPoll: 50 * time.Millisecond, Head: head}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { agentrtapi.Mount(r, auth, api) })
	s := httptest.NewServer(r)
	t.Cleanup(s.Close)
	return pool, task, s.URL + "/api/v1/agentrt"
}

var cursor = agentd.Tool{Executor: "exec:cursor", Provider: "cursor-mix"}

// agentd's whole path through the client: claim, start, heartbeat,
// checkpoint, computed, upload, persist, submit (twice); the platform's
// refusals come back as the errors agentd acts on.
func TestClientPath(t *testing.T) {
	pool, task, base := platform(t)
	c := &agentd.Client{Base: base, Host: "h1", Token: "fixture-agentd-h1"}

	cl, err := c.Claim(ctx, 1, []agentd.Tool{cursor})
	must(t, err)
	if cl == nil || cl.TaskID != task || cl.Epoch == 0 || cl.EffectClass != "none" {
		t.Fatalf("claim = %+v", cl)
	}
	spec, err := c.GetArtifact(ctx, cl.SpecArtifact)
	if err != nil || string(spec) != "# task\n" {
		t.Fatalf("spec = %q %v", spec, err)
	}
	must(t, c.Start(ctx, cl.AttemptID, cl.Epoch, "auto", "cursor-mix"))
	stop, expires, _, err := c.Heartbeat(ctx, cl.AttemptID, cl.Epoch, nil)
	if err != nil || stop || !expires.After(time.Now()) {
		t.Fatalf("heartbeat = %v %v %v", stop, expires, err)
	}
	if _, _, _, err = c.Heartbeat(ctx, cl.AttemptID, cl.Epoch+1, nil); !errors.Is(err, agentd.ErrFenced) {
		t.Fatalf("another epoch = %v", err)
	}
	log, err := c.PutArtifact(ctx, "log_tail", "text/plain", []byte("compiling\n"))
	must(t, err)
	must(t, c.Checkpoint(ctx, cl.AttemptID, cl.Epoch, 1, []string{log}, false))
	exit := 0
	must(t, c.Computed(ctx, cl.AttemptID, cl.Epoch, &exit, "exit", "done"))
	report, err := c.PutArtifact(ctx, "report", "text/markdown", []byte("# done\n"))
	must(t, err)
	if _, err := c.PutArtifact(ctx, "report", "text/markdown", []byte("see /Users/someone\n")); err == nil {
		t.Fatal("a report with a local path was taken")
	}
	must(t, c.Persist(ctx, cl.AttemptID, cl.Epoch, agentd.Result{Report: report, Artifacts: []string{log}}))
	must(t, c.Submit(ctx, cl.AttemptID, cl.Epoch))
	must(t, c.Submit(ctx, cl.AttemptID, cl.Epoch)) // a lost answer, retried
	var state string
	must(t, pool.QueryRow(ctx, `SELECT state FROM agentrt.task WHERE task_id = $1`, task).Scan(&state))
	if state != "submitted" {
		t.Fatalf("task = %s", state)
	}

	if none, claimErr := c.Claim(ctx, 1, []agentd.Tool{cursor}); claimErr != nil || none != nil {
		t.Fatalf("an empty queue = %+v %v", none, claimErr)
	}
	other := &agentd.Client{Base: base, Host: "h1", Token: "fixture-agentd-h2"}
	if _, err := other.Claim(ctx, 1, []agentd.Tool{cursor}); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("h2's token for h1 = %v", err)
	}
	if err := c.Ended(ctx, cl.AttemptID, "exit", true); err != nil {
		t.Fatalf("ended = %v", err)
	}
}

// The token never reaches an error.
func TestClientKeepsTheTokenOut(t *testing.T) {
	s := httptest.NewServer(nil)
	base := s.URL
	s.Close()
	c := &agentd.Client{Base: base + "/api/v1/agentrt", Host: "h1", Token: "fixture-secret-token"}
	_, err := c.Claim(ctx, 1, []agentd.Tool{cursor})
	if !errors.Is(err, agentd.ErrUnavailable) || strings.Contains(err.Error(), "fixture-secret-token") {
		t.Fatalf("err = %v", err)
	}
}

// A task spec whose content is not its address is refused.
func TestClientRefusesATamperedArtifact(t *testing.T) {
	want := agentd.ArtifactID([]byte("# task\n"))
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("# another task\n")) }))
	t.Cleanup(s.Close)
	c := &agentd.Client{Base: s.URL, Host: "h1", Token: "fixture-agentd-h1"}
	if body, err := c.GetArtifact(ctx, want); err == nil {
		t.Fatalf("took %q as %s", body, want)
	}
}
