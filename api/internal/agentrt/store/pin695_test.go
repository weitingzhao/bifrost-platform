package store_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

var err695Commit = errors.New("fixture: connection lost at claim COMMIT")

// Fault only the transaction writing the claim key, not registration, a
// lookup, or a nested savepoint. All SQL and COMMIT run against PostgreSQL.
type claimCommit695DB struct {
	store.DB
	mode     string
	fired    bool
	readback int
}

func (d *claimCommit695DB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	if d.fired {
		d.readback++
	}
	tx, err := d.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &claimCommit695Tx{Tx: tx, db: d}, nil
}

type claimCommit695Tx struct {
	pgx.Tx
	db          *claimCommit695DB
	writesClaim bool
}

func (tx *claimCommit695Tx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tag, err := tx.Tx.Exec(ctx, sql, args...)
	if err == nil && strings.Contains(sql, "INSERT INTO agentrt.idempotency_key") && args[1] == "claim_pinned" {
		tx.writesClaim = true
	}
	return tag, err
}

func (tx *claimCommit695Tx) Commit(ctx context.Context) error {
	if !tx.writesClaim || tx.db.fired || tx.db.mode == "normal" {
		return tx.Tx.Commit(ctx)
	}
	var err error
	if tx.db.mode == "landed" {
		err = tx.Tx.Commit(ctx)
	} else {
		err = tx.Tx.Rollback(ctx)
	}
	if err != nil {
		return err
	}
	tx.db.fired = true
	return err695Commit
}

// T13: public ClaimPinned must recover the first claim's committed response.
// Removing its production readBackKey call makes the landed case fail.
func Test695ClaimPinnedCommitOutcome(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"normal", "landed", "rolled back"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			w := pinWorld(t)
			task := w.readyPinned(t, "claim")
			db := &claimCommit695DB{DB: w.rt, mode: mode}
			out, err := store.ClaimPinned(ctx, db, exactReq(task, "key"))
			if db.fired != (mode != "normal") || mode != "normal" && db.readback == 0 {
				t.Fatalf("COMMIT injection/readback not exercised: %+v", db)
			}
			// An independent connection observes what really committed.
			observer := w.db.Pool(t, w.db.Runtime)
			var attempts, keys int
			ok(t, observer.QueryRow(ctx, `SELECT count(*) FROM agentrt.attempt WHERE task_id=$1`, task).Scan(&attempts))
			keyID := "host:" + externalHost + "|claim_pinned|key"
			ok(t, observer.QueryRow(ctx, `SELECT count(*) FROM agentrt.idempotency_key WHERE idempotency_key_id=$1`, keyID).Scan(&keys))
			if mode == "rolled back" {
				if !errors.Is(err, err695Commit) || out != nil || attempts != 0 || keys != 0 {
					t.Fatalf("rollback: response=%v err=%v attempts=%d keys=%d", out, err, attempts, keys)
				}
				return
			}
			ok(t, err)
			var stored string
			ok(t, observer.QueryRow(ctx, `SELECT response_json::text FROM agentrt.idempotency_key WHERE idempotency_key_id=$1`, keyID).Scan(&stored))
			want, err := canon.Parse([]byte(stored))
			ok(t, err)
			if attempts != 1 || keys != 1 || !reflect.DeepEqual(out, want) {
				t.Fatalf("response=%v stored=%v attempts=%d keys=%d", out, want, attempts, keys)
			}
			again, err := store.ClaimPinned(ctx, observer, exactReq(task, "key"))
			ok(t, err)
			if !reflect.DeepEqual(again, out) || w.attempts(t, task) != 1 {
				t.Fatalf("retry created or changed claim: %v / %v", out, again)
			}
		})
	}
}

// T17: generate a real review via submit -> verify -> conclude, then take it
// through ClaimNext. Forge only the subject's provider, retaining its Claude
// executor so the provider check cannot be masked by executor equality.
func Test695PinnedReviewClaim(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"anthropic", "openai"} {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			w := pinWorld(t)
			r := w.pinnedRun(t, "subject", time.Hour)
			head, commits := forge(commitB, goodCommit(r.task))
			_, err := store.PersistChecked(ctx, w.rt, r.fence, store.Result{Report: r.report, Ref: store.AttemptRef(r.task, 1), Commit: commitB}, head, commits)
			ok(t, err)
			_, err = store.Submit(ctx, w.rt, r.fence)
			ok(t, err)
			v, err := store.StartVerification(ctx, w.rt, r.task)
			ok(t, err)
			vc := w.run(t, "h1", deterministic, "done")
			if vc.TaskID != v {
				t.Fatalf("claimed verification %s, want %s", vc.TaskID, v)
			}
			did, err := store.Conclude(ctx, w.rt, v)
			ok(t, err)
			if did != "review" {
				t.Fatalf("conclude: %s", did)
			}
			review := w.checker(t, r.task, "review")
			if provider == "openai" {
				// Use the existing import event path to inject corrupt legacy
				// evidence after routing, testing the claim's fresh check too.
				ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
					if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller", Body: map[string]any{"source": "test: forged subject provider"}}); err != nil {
						return err
					}
					_, err := tx.Exec(ctx, `UPDATE agentrt.attempt SET model_provider=$2 WHERE attempt_id=$1`, r.attempt, provider)
					return err
				}))
			}
			claim, err := store.ClaimNext(ctx, w.rt, store.ClaimRequest{HostID: "h1", FreeSlots: 1, Tools: []store.Tool{codex, claude, cursor}})
			ok(t, err)
			if provider == "openai" {
				if claim != nil || w.attempts(t, review) != 0 {
					t.Fatalf("forged openai subject got a reviewer: %+v", claim)
				}
				return
			}
			if claim == nil || claim.TaskID != review || claim.Tool != codex || claim.BaseCommit != commitB {
				t.Fatalf("independent review claim: %+v", claim)
			}
			if spec := w.checkSpec(t, review); spec["attempt"] != r.attempt || spec["commit"] != commitB {
				t.Fatalf("review spec lost its subject result: %v", spec)
			}
		})
	}
}
