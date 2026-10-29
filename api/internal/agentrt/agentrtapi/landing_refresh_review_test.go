package agentrtapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func refreshFixture(t *testing.T) (*pgxpool.Pool, string, string) {
	t.Helper()
	pool := pinWorld(t)
	task := pinnedFixture(t, pool)
	s := server(t, pool)
	code, claim := call(t, s, "agentd:coordinator-mbp", "POST", "claim/pinned", map[string]any{
		"host_id": "coordinator-mbp", "task_id": task, "idempotency_key": task + ":1", "free_slots": 1,
	})
	if code != http.StatusOK {
		t.Fatalf("claim: %d %v", code, claim)
	}
	return pool, task, landingWait(t, pool, task, claim)
}

// Host kind and executor pin are immutable, and the attempt's host/pin
// relation is guarded by migration 0010. To isolate each predicate without
// disabling any database guard, replace ONE input relation in the permission
// SELECT with a read-only projection (or change only the host argument).
// PostgreSQL still evaluates the exact
// production predicates. All other reads, locks and writes use the real DB.
// afterMain switches the view only after the first check and external read.
type refreshViewDB struct {
	store.DB
	t                    *testing.T
	field                string
	afterMain, mainRead  bool
	checks, lockedChecks int
}

type refreshViewTx struct {
	pgx.Tx
	db     *refreshViewDB
	locked bool
}

func (d *refreshViewDB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := d.DB.BeginTx(ctx, opts)
	return &refreshViewTx{Tx: tx, db: d}, err
}

func (tx *refreshViewTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tag, err := tx.Tx.Exec(ctx, sql, args...)
	if err == nil && strings.Contains(sql, "event_stream") && strings.Contains(sql, "FOR UPDATE") {
		tx.locked = true
	}
	return tag, err
}

func (tx *refreshViewTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "event_stream") && strings.Contains(sql, "FOR UPDATE") {
		tx.locked = true
	}
	if strings.Contains(sql, "SELECT EXISTS (SELECT 1 FROM agentrt.attempt a JOIN agentrt.host h") {
		d := tx.db
		d.checks++
		if tx.locked {
			d.lockedChecks++
		}
		if !d.afterMain || d.mainRead {
			var from, to string
			switch d.field {
			case "host_kind":
				from, to = "agentrt.host h", "(SELECT host_id, 'vm'::text AS host_kind FROM agentrt.host) h"
			case "pin":
				from, to = "agentrt.task t", "(SELECT task_id, NULL::text AS executor_pin FROM agentrt.task) t"
			case "host_id":
				args = append([]any(nil), args...)
				args[1] = "h1"
			case "accepted":
				from, to = "agentrt.verification v", "(SELECT task_id, attempt_id, created_at, verification_id, false AS accepts_attempt FROM agentrt.verification) v"
			}
			if from != "" {
				if !strings.Contains(sql, from) {
					d.t.Fatal("permission fixture missed relation", from)
				}
				sql = strings.Replace(sql, from, to, 1)
			}
		}
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func refreshCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id='landing:bifrost-platform'`).Scan(&n))
	return n
}

func assertRefreshed(t *testing.T, pool *pgxpool.Pool, op string) {
	t.Helper()
	w, err := store.ReadLandingWait(ctx, pool, op)
	must(t, err)
	if w == nil || w.Phase != "discarded" {
		t.Fatalf("refresh not persisted: %+v", w)
	}
}

// Serial: uses server's shared fixture configuration. Public store entry,
// real permission SQL, same-entry positive control and one changed input.
func TestC4Review690RefreshStorePermissions(t *testing.T) {
	for _, afterMain := range []bool{false, true} {
		for _, field := range []string{"", "host_kind", "pin", "host_id", "accepted"} {
			t.Run(fmt.Sprintf("%s/afterMain=%v", field, afterMain), func(t *testing.T) {
				pool, task, op := refreshFixture(t)
				db := &refreshViewDB{DB: pool, t: t, field: field, afterMain: afterMain}
				before := refreshCount(t, pool)
				wrote, err := store.RefreshLanding(ctx, db, task, op, "coordinator-mbp", func(context.Context, string) (string, error) {
					db.mainRead = true
					return commitA, nil
				})
				if field == "" {
					must(t, err)
					if !wrote || db.checks != 2 || db.lockedChecks != 1 || refreshCount(t, pool) != before+1 {
						t.Fatal("success path missed checks", wrote, db)
					}
					assertRefreshed(t, pool, op)
					return
				}
				if err == nil || !strings.Contains(err.Error(), "refresh_not_permitted") || wrote || before != refreshCount(t, pool) {
					t.Fatalf("%s admitted: wrote=%v err=%v", field, wrote, err)
				}
				checks, locked := 1, 0
				if afterMain {
					checks, locked = 2, 1
				}
				if db.checks != checks || db.lockedChecks != locked || db.mainRead != afterMain {
					t.Fatal("wrong refusal point", db)
				}
			})
		}
	}
}

// Serial: same fixture as the routes world, through auth and with the action
// catalog's production exemptions (these work-queue routes are not actions).
// The permitted request is a real claim/pinned -> handin -> refresh chain.
func TestC4Review690RefreshHTTPPermissions(t *testing.T) {
	for _, path := range []string{"claim/pinned", "landing/refresh"} {
		if actions.ExemptionReason("POST", "/api/v1/agentrt/"+path) == "" {
			t.Fatal("missing work-queue exemption", path)
		}
	}
	pool, task, op := refreshFixture(t)
	db := &refreshViewDB{DB: pool, t: t}
	s := server(t, db, func(a *agentrtapi.API) {
		a.MainHead = func(context.Context, string) (string, error) { db.mainRead = true; return commitA, nil }
	})
	body := map[string]any{"task": task, "op": op}
	before := refreshCount(t, pool)
	for _, caller := range []string{"", "viewer", "operator", "agentd:h1"} {
		code, out := call(t, s, caller, "POST", "landing/refresh", body)
		want := http.StatusUnauthorized
		if caller == "agentd:h1" {
			want = http.StatusUnprocessableEntity
		}
		if code != want || before != refreshCount(t, pool) {
			t.Fatalf("%s: %d %v", caller, code, out)
		}
	}
	for _, field := range []string{"host_kind", "pin", "host_id", "accepted"} {
		db.field, db.mainRead = field, false
		code, out := call(t, s, "agentd:coordinator-mbp", "POST", "landing/refresh", body)
		if code != http.StatusUnprocessableEntity || out["error"] != "invalid" || db.mainRead || before != refreshCount(t, pool) {
			t.Fatalf("%s: %d %v", field, code, out)
		}
	}
	db.field, db.checks, db.lockedChecks = "", 0, 0
	code, out := call(t, s, "agentd:coordinator-mbp", "POST", "landing/refresh", body)
	if code != http.StatusOK || out["refreshed"] != true || db.checks != 2 || db.lockedChecks != 1 || refreshCount(t, pool) != before+1 {
		t.Fatalf("permitted: %d %v, checks=%d/%d", code, out, db.checks, db.lockedChecks)
	}
	assertRefreshed(t, pool, op)
}
