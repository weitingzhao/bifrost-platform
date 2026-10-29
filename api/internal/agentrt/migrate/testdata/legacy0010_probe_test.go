// This driver is compiled ONLY with the complete API at 5a8edd55 by
// migrate.TestUpgradeTo0010KeepsDataAndSupportsLegacyRuntime.
package store_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Serial: the parent owns this one upgraded database; no other probe runs here.
func TestT50LegacyOnUpgradedDatabase(t *testing.T) {
	if os.Getenv("AGENTRT_TEST_REQUIRED") != "1" || os.Getenv("AGENTRT_T50_RUNTIME") == "" {
		t.Fatal("T50 requires an explicitly supplied upgraded database; skipping is not acceptance")
	}
	db := &agentrttest.DB{Runtime: os.Getenv("AGENTRT_T50_RUNTIME")}
	w := &world{db: db, rt: db.Connect(t, db.Runtime)}
	var version int
	ok(t, w.rt.QueryRow(ctx, `SELECT max(schema_migration_id) FROM agentrt.schema_migration`).Scan(&version))
	if version != 10 {
		t.Fatalf("expected an already-upgraded 0010 database, got %d", version)
	}
	ok(t, w.rt.QueryRow(ctx, `SELECT spec_artifact_id FROM agentrt.task WHERE title='proposed'`).Scan(&w.spec))
	_, err := w.rt.Exec(ctx, agentrttest.DispatchRules)
	ok(t, err)
	w.jobs(t, "active", "*", codex)

	// Resume a lease that existed before migration, then hand in its result.
	var old store.Fence
	var previous time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT attempt_id, host_id, lease_epoch, lease_expires_at FROM agentrt.attempt WHERE state='running'`).
		Scan(&old.AttemptID, &old.HostID, &old.Epoch, &previous))
	beat, err := store.Heartbeat(ctx, w.rt, old)
	ok(t, err)
	if beat.LeaseExpiresAt.Before(previous) {
		t.Fatal("old lease moved backwards")
	}
	finish := func(f store.Fence) {
		t.Helper()
		exit := 0
		_, err := store.Computed(ctx, w.rt, f, &exit, "exit", "done")
		ok(t, err)
		_, err = store.Persist(ctx, w.rt, f, store.Result{Report: w.artifact(t, "report", "T50 "+f.AttemptID)}, nil)
		ok(t, err)
		_, err = store.Submit(ctx, w.rt, f)
		ok(t, err)
		if state := w.state(t, "attempt", f.AttemptID); state != "submitted" {
			t.Fatalf("submit state=%s", state)
		}
	}
	finish(old)

	// Ordinary proposal -> ready -> claim on a VM -> start -> renew -> submit.
	out, err := store.ProposeTasks(ctx, w.rt, wr("thread:t50"), "t50-propose", "M-1", []store.TaskProposal{{
		Ref: "plain", OffPlanReason: "T50 compatibility", Role: "implement", Class: "go", Title: "legacy ordinary task",
		SpecArtifact: w.spec, Effect: "none",
	}})
	ok(t, err)
	id := out["tasks"].([]any)[0].(map[string]any)["task_id"].(string)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MarkReady(ctx, tx, "controller", id) }))
	c := w.claim(t, "old-vm", time.Minute, codex)
	if c == nil || c.TaskID != id {
		t.Fatalf("legacy claim=%+v, want task %s", c, id)
	}
	f := fence(c, "old-vm")
	ok(t, store.Start(ctx, w.rt, f, "gpt-x", "openai"))
	beat, err = store.Heartbeat(ctx, w.rt, f)
	ok(t, err)
	if !beat.LeaseExpiresAt.After(c.LeaseExpiresAt) {
		t.Fatal("legacy heartbeat did not extend the one-minute lease")
	}
	finish(f)

	// Actual baseline HTTP read handlers, using its baseline auth and SQL.
	authPath := filepath.Join(t.TempDir(), "auth.yaml")
	ok(t, os.WriteFile(authPath, []byte("tokens:\n  - {name: viewer, role: viewer, token: fixture-t50-read}\n"), 0o600))
	auth, err := actuation.LoadAuth(authPath)
	ok(t, err)
	r := chi.NewRouter()
	agentrtapi.Mount(r, auth, &agentrtapi.API{DB: w.rt})
	for path, want := range map[string]string{
		"/agentrt/tasks": id, "/agentrt/tasks/" + id: id, "/agentrt/missions/M-1": "M-1", "/agentrt/hosts": "old-vm",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer fixture-t50-read")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") ||
			!strings.Contains(rec.Body.String(), want) {
			t.Fatalf("baseline GET %s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.task WHERE executor_pin IS NOT NULL`); n != 0 {
		t.Fatalf("legacy ordinary writes created %d pins", n)
	}
	for _, stream := range []string{"system", "mission:M-1", "host:old-dev", "host:old-vm"} {
		_, err := store.VerifyStream(ctx, w.rt, stream)
		ok(t, err)
	}
	t.Log("baseline proposal, claim, start, renewal, compute, persist, submit and HTTP reads passed on upgraded DB")
}
