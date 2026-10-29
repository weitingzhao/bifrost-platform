package store

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// Row fixtures exercise the production projection and append gate, not SQL
// concurrency. The two-connection tests in landing_wait_test.go cover that.
type waitReviewTx struct {
	pgx.Tx
	requests []map[string]any // newest arrival first, as the query orders them
	entries  []landing.Entry
}

func (tx waitReviewTx) Query(_ context.Context, q string, _ ...any) (pgx.Rows, error) {
	// C-5's grant projection: these fixtures carry no grant records.
	if strings.Contains(q, "landing_grant_use") {
		return &waitReviewRows{}, nil
	}
	if strings.Contains(q, "landing_grant_request") {
		requests := tx.requests
		// The baseline query returned only the newest arrival per approval.
		// Model that row shape too, so its ordered-success control is valid.
		if strings.Contains(q, "DISTINCT ON") {
			requests = nil
			seen := map[any]bool{}
			for _, r := range tx.requests {
				if !seen[r["approval_id"]] {
					requests = append(requests, r)
					seen[r["approval_id"]] = true
				}
			}
		}
		return &waitReviewRows{requests: requests}, nil
	}
	return &waitReviewRows{entries: tx.entries}, nil
}

type waitReviewRows struct {
	pgx.Rows
	requests []map[string]any
	entries  []landing.Entry
	i        int
}

func (r *waitReviewRows) Next() bool { r.i++; return r.i <= len(r.requests)+len(r.entries) }
func (r *waitReviewRows) Close()     {}
func (r *waitReviewRows) Err() error { return nil }
func (r *waitReviewRows) Scan(dest ...any) error {
	if r.requests != nil {
		*dest[0].(*map[string]any) = maps.Clone(r.requests[r.i-1])
		return nil
	}
	e := r.entries[r.i-1]
	*dest[0].(*time.Time) = e.At
	b, err := json.Marshal(e.Record)
	*dest[1].(*[]byte) = b
	return err
}

func reviewWaitEntries(t *testing.T, start, wait time.Time) []landing.Entry {
	t.Helper()
	op, root, a, b := "land:t_00000000000000000000000001:1", "t_00000000000000000000000001", strings.Repeat("a", 40), strings.Repeat("b", 40)
	ref := "refs/heads/land/prod/" + root + "/1"
	var entries []landing.Entry
	put := func(at time.Time, kind string, kv ...any) {
		r := landing.Record{"v": landing.Version, "kind": kind, "op": op}
		for i := 0; i < len(kv); i += 2 {
			r[kv[i].(string)] = kv[i+1]
		}
		entries = append(entries, landing.Entry{At: at, Record: r})
	}
	put(start, "claimed", "task", root, "attempt", "a_00000000000000000000000001", "A", a, "rule_version", 1)
	put(start.Add(time.Second), "prepared", "R", b, "M0", a, "committer_date", start.Format(time.RFC3339Nano), "dry_run", false)
	put(start.Add(2*time.Second), "branch_intent", "ref", ref, "R", b)
	put(start.Add(3*time.Second), "branch_pushed", "ref", ref, "R", b)
	put(wait, "ci_seen", "run", "ci-review", "status", "Succeeded", "R", b, "created_at", wait.Format(time.RFC3339Nano))
	put(wait, "awaiting_grant", "soft", []any{"core"}, "files", 1, "lines", 2, "material", "sha256:"+strings.Repeat("a", 64))
	if _, err := landing.Fold(entries); err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestC4Review690ApprovalProgressIgnoresArrivalOrder(t *testing.T) {
	t.Parallel()
	for _, newer := range []string{"approved", "running", "unknown", "expired", "failed", "executed", "rejected"} {
		for _, older := range []string{"pending", "approved", "running"} {
			if newer == "approved" && older != "pending" || newer == "running" && older == "running" {
				continue
			}
			for _, reverse := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/reverse=%v", newer, older, reverse), func(t *testing.T) {
					t.Parallel()
					at := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
					s, err := landing.Fold(reviewWaitEntries(t, at.Add(-time.Hour), at))
					if err != nil {
						t.Fatal(err)
					}
					r := LandingGrantRequest{Op: "land:t_00000000000000000000000001:1", ApprovalID: "approval_42", ApprovalNumber: 42, ParamsHash: strings.Repeat("a", 64), SeenAt: at}
					r.Status = newer
					a := r.body()
					r.Status = older
					rows := []map[string]any{a, r.body()}
					if reverse {
						rows[0], rows[1] = rows[1], rows[0]
					}
					w, err := readLandingWaitStatus(context.Background(), waitReviewTx{requests: rows}, s, r.Op, at)
					if err != nil {
						t.Fatal(err)
					}
					w.ObserveMain(strings.Repeat("a", 40))
					if w.ApprovalStatus != newer || w.OwnerTodo {
						t.Fatalf("late %s won: %+v", older, w)
					}
				})
			}
		}
	}
}

func TestC4Review690WaitingBoundChecksCurrentBudget(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	entries := reviewWaitEntries(t, at, at.Add(5*time.Hour+59*time.Minute))
	ev := Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", Body: map[string]any{"landing": map[string]any{"v": landing.Version, "kind": "failed", "op": "land:t_00000000000000000000000001:1", "why": "bound"}}}
	// Same append gate: a root that really spent six hours executing can end.
	if err := checkLandingAppend(context.Background(), waitReviewTx{entries: entries[:4]}, ev, at.Add(6*time.Hour+time.Minute)); err != nil {
		t.Fatal(err)
	}
	// A entered waiting while B was reading CI: E=5h59m, W=2m.
	if err := checkLandingAppend(context.Background(), waitReviewTx{entries: entries}, ev, at.Add(6*time.Hour+time.Minute)); err == nil {
		t.Fatal("accepted stale bound after waiting paused E")
	}
}

type waitReviewRow func(...any) error

func (r waitReviewRow) Scan(dest ...any) error { return r(dest...) }

// This transaction substitute proves the public entry uses both permission
// decisions and commits only the successful append. It does not claim to
// evaluate SQL: the PostgreSQL tests isolate the four SQL predicates.
type refreshReviewDB struct {
	t               *testing.T
	entries         []landing.Entry
	permissions     []bool
	checks, inserts int
}

type refreshReviewTx struct {
	waitReviewTx
	db      *refreshReviewDB
	locked  bool
	inserts int
}

func (db *refreshReviewDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return &refreshReviewTx{waitReviewTx: waitReviewTx{entries: db.entries}, db: db}, nil
}
func (tx *refreshReviewTx) Commit(context.Context) error   { tx.db.inserts += tx.inserts; return nil }
func (tx *refreshReviewTx) Rollback(context.Context) error { return nil }
func (tx *refreshReviewTx) Exec(_ context.Context, q string, _ ...any) (pgconn.CommandTag, error) {
	if strings.Contains(q, "event_stream") && strings.Contains(q, "FOR UPDATE") {
		tx.locked = true
	}
	return pgconn.CommandTag{}, nil
}
func (tx *refreshReviewTx) Query(ctx context.Context, q string, args ...any) (pgx.Rows, error) {
	if !tx.locked {
		tx.db.t.Fatal("state read without repository lock")
	}
	return tx.waitReviewTx.Query(ctx, q, args...)
}
func (tx *refreshReviewTx) QueryRow(_ context.Context, q string, _ ...any) pgx.Row {
	return waitReviewRow(func(dest ...any) error {
		switch {
		case strings.Contains(q, "SELECT EXISTS (SELECT 1 FROM agentrt.attempt"):
			i := tx.db.checks
			if i >= len(tx.db.permissions) {
				return fmt.Errorf("unexpected permission read")
			}
			if tx.locked != (i == 1) {
				tx.db.t.Fatal("permission check at wrong lock point", i)
			}
			tx.db.checks++
			*dest[0].(*bool) = tx.db.permissions[i]
		case strings.Contains(q, "SELECT mission_id"):
			*dest[0].(*string) = "M-1"
		case strings.Contains(q, "SELECT last_seq"):
			if !tx.locked {
				tx.db.t.Fatal("append not locked")
			}
			*dest[0].(*int64) = 6
			*dest[1].(*string) = strings.Repeat("a", 64)
		case strings.Contains(q, "INSERT INTO agentrt.event"):
			tx.inserts++
			*dest[0].(*int64) = 7
		default:
			return fmt.Errorf("unexpected query: %s", q)
		}
		return nil
	})
}

func TestC4Review690RefreshChecksBothPermissions(t *testing.T) {
	t.Parallel()
	for name, permitted := range map[string][]bool{"success": {true, true}, "first_check": {false}, "locked_check": {true, false}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			at := time.Now().UTC().Add(-time.Minute)
			db := &refreshReviewDB{t: t, entries: reviewWaitEntries(t, at.Add(-time.Hour), at), permissions: permitted}
			reads := 0
			wrote, err := RefreshLanding(context.Background(), db, "t_00000000000000000000000001", "land:t_00000000000000000000000001:1", "coordinator-mbp",
				func(context.Context, string) (string, error) { reads++; return strings.Repeat("b", 40), nil })
			if name == "success" {
				if err != nil || !wrote || db.inserts != 1 {
					t.Fatal(wrote, err, db.inserts)
				}
			} else if err == nil || !strings.Contains(err.Error(), "refresh_not_permitted") || wrote || db.inserts != 0 {
				t.Fatal("permission bypass", wrote, err, db.inserts)
			}
			if db.checks != len(permitted) || reads != len(permitted)-1 {
				t.Fatal("wrong check count", db.checks, reads)
			}
		})
	}
}

func TestC4Review690TimerUsesLockedState(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC().Add(-6*time.Hour - time.Minute)
	entries := reviewWaitEntries(t, at, at.Add(5*time.Hour+59*time.Minute))
	old, err := landing.Fold(entries[:4])
	if err != nil {
		t.Fatal(err)
	}
	op := "land:t_00000000000000000000000001:1"
	for name, current := range map[string][]landing.Entry{"expired": entries[:4], "now_waiting": entries} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := &refreshReviewDB{t: t, entries: current}
			why, err := SettleLandingOperationBound(context.Background(), db, op, old.Operations[op])
			if err != nil {
				t.Fatal(err)
			}
			if name == "expired" {
				if why != "bound" || db.inserts != 1 {
					t.Fatal("real bound not settled", why, db.inserts)
				}
			} else if why != "" || db.inserts != 0 {
				t.Fatal("stale clock wrote", why, db.inserts)
			}
		})
	}
	// The same timer entry never terminates main_intent or an unknown push.
	main := append([]landing.Entry(nil), entries[:5]...)
	main = append(main, landing.Entry{At: at.Add(6 * time.Hour), Record: landing.Record{"v": landing.Version, "kind": "main_intent", "op": op,
		"R": strings.Repeat("b", 40), "M0": strings.Repeat("a", 40)}})
	for _, phase := range []string{"main_intent", "main_result"} {
		if phase == "main_result" {
			main = append(main, landing.Entry{At: at.Add(6 * time.Hour), Record: landing.Record{"v": landing.Version, "kind": "main_result", "op": op, "outcome": "unknown"}})
		}
		s, err := landing.Fold(main)
		if err != nil {
			t.Fatal(err)
		}
		db := &refreshReviewDB{t: t, entries: main}
		why, err := SettleLandingOperationBound(context.Background(), db, op, s.Operations[op])
		if err != nil || why != "" || db.inserts != 0 {
			t.Fatal("timer ended push", phase, why, err)
		}
		if phase == "main_result" {
			// The reconciler's bound after a main read keeps its old meaning.
			ev := Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", Body: map[string]any{"landing": map[string]any{"v": landing.Version, "kind": "failed", "op": op, "why": "bound"}}}
			if err := checkLandingAppend(context.Background(), waitReviewTx{entries: main}, ev, time.Now()); err != nil {
				t.Fatal("reconciler bound refused", err)
			}
		}
	}
}
