package store_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

var (
	codex  = store.Tool{Executor: "exec:codex", Provider: "openai", Model: "gpt-x"}
	claude = store.Tool{Executor: "exec:claude", Provider: "anthropic", Model: "opus"}
	cursor = store.Tool{Executor: "exec:cursor", Provider: "cursor-mix"}
)

const (
	commitA = "2333e00753f91c74c8b2f4d693a803f8e89e7892"
	commitB = "0b9e1f3c7a1d2e4f5a6b7c8d9e0f1a2b3c4d5e6f"
	noSuch  = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
)

// newKernelWorld is a world with mission M-1 active, hosts h1–h8, and every
// tool the tests use a candidate for every job.
func newKernelWorld(t *testing.T) *world {
	t.Helper()
	ctx := ctxOf(t)
	w := newWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	w.seeded(t, func(tx pgx.Tx) {
		for i := 1; i <= 8; i++ {
			seedHost(t, tx, fmt.Sprintf("h%d", i))
		}
	})
	_, err := w.rt.Exec(ctx, agentrttest.DispatchRules)
	ok(t, err)
	// The seed's Owner facts as if he had signed capability-facts: core
	// work needs its only facts to grant.
	w.legacy(t, agentrttest.OwnerFactsRule, agentrttest.BindOwnerFacts)
	w.jobs(t, "active", "*", codex, claude, cursor, deterministic, store.Tool{Executor: "exec:claude", Provider: "other"},
		store.Tool{Executor: "exec:codex", Provider: "other"}, store.Tool{Executor: "exec:codex", Provider: "cursor-mix"},
		store.Tool{Executor: "exec:claude", Provider: "cursor-mix"})
	return w
}

// jobs writes the jobs and makes each tool a candidate for every job with
// the given status and model pattern. The Owner's signed initial rule writes
// these (O19); a test writes them with the import event, under a rule of its
// own.
func (w *world) jobs(t *testing.T, status, pattern string, tools ...store.Tool) {
	t.Helper()
	ctx := ctxOf(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: jobs and candidates"}}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, agentrttest.TestJobRule); err != nil {
			return err
		}
		for _, job := range []string{"planner", "implementer", "verifier", "reviewer", "chief_of_staff"} {
			if _, err := tx.Exec(ctx, `INSERT INTO agentrt.job (job_id, tenant, rule_id, rule_version) VALUES ($1, 'dev', 'job:test', 1)
			  ON CONFLICT DO NOTHING`, job); err != nil {
				return err
			}
			for _, tl := range tools {
				if _, err := tx.Exec(ctx, `INSERT INTO agentrt.job_candidate (job_candidate_id, job_id, executor_actor_id, model_provider,
				  model_pattern, status, rule_id, rule_version) VALUES ($1, $2, $3, $4, $5, $6, 'job:test', 1)`, job+"/"+tl.Executor+"/"+tl.Provider+"/"+pattern, job,
					tl.Executor, tl.Provider, pattern, status); err != nil {
					return err
				}
			}
		}
		return nil
	}))
}

// waitFor returns once a backend of this database waits for a lock in a
// statement containing fragment, and fails the test if that has not happened
// by the deadline: a race the test means to stage did not happen.
func (w *world) waitFor(t *testing.T, fragment string, deadline time.Time) {
	t.Helper()
	w.waitForN(t, fragment, 1, deadline)
}

// waitForN is waitFor for at least n backends.
func (w *world) waitForN(t *testing.T, fragment string, want int, deadline time.Time) {
	t.Helper()
	ctx := ctxOf(t)
	for {
		var n int
		ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()
		  AND wait_event_type = 'Lock' AND position($1 in query) > 0`, fragment).Scan(&n))
		if n >= want {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("nothing waited for a lock in %q in time; the case proves nothing", fragment)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (w *world) artifact(t *testing.T, kind, body string) string {
	t.Helper()
	ctx := ctxOf(t)
	var id string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		id, err = store.PutArtifact(ctx, tx, "controller", kind, "text/plain", []byte(body))
		return err
	}))
	return id
}

func (w *world) control(t *testing.T, id, scope, ref, kind string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.SetControl(ctx, tx, "owner", id, scope, ref, kind, "test") }))
}

func (w *world) chainHolds(t *testing.T, stream string) {
	t.Helper()
	if _, err := store.VerifyStream(ctx, w.rt, stream); err != nil {
		t.Fatalf("%s: %v", stream, err)
	}
}

func is(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("err = %v; want %v", err, target)
	}
}

func same(t *testing.T, got, want map[string]any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("answer %v; want the first answer %v", got, want)
	}
}
