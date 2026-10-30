package store_test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

type stgSettlementDB struct {
	store.DB
	afterRead, afterLookup func()
	transactions           int
}
type stgSettlementTx struct {
	pgx.Tx
	afterCommit func()
}

func (d *stgSettlementDB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := d.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	d.transactions++
	var after func()
	if d.transactions == 1 {
		after = d.afterRead
	}
	if d.transactions == 2 {
		after = d.afterLookup
	}
	return &stgSettlementTx{Tx: tx, afterCommit: after}, nil
}
func (tx *stgSettlementTx) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	if tx.afterCommit != nil {
		tx.afterCommit()
	}
	return nil
}
func stgSettlementNone(ctx context.Context, db store.DB) error {
	body := strings.Replace(landingRuleBody, "stg_release: bifrost-deliver-platform", "stg_release: none", 1)
	_, err := store.TightenRule(ctx, db, store.Writer{Actor: "owner", Token: "admin"}, "landing:dev", []byte(ruleText("landing:dev", 2, "tighten", since, until, body)))
	return err
}
func stgSettlementResume(t *testing.T, x *b5Fixture) {
	t.Helper()
	_, err := (signer{t, rptest.NewKey(t)}).sign(x.w, ruleText("landing:dev", 3, "loosen", since, until, landingRuleBody))
	ok(t, err)
}
func stgSettlementFixture(t *testing.T, action string) (*b5Fixture, *fakeCards, store.CardRecord) {
	t.Helper()
	mode := "exhausted"
	if action == "withdraw" {
		mode = "blocked"
	}
	x, trigger := b5gFixture(t, mode)
	f := newFakeCards(store.DecisionAt)
	kind := store.CardSTGFailed
	if action == "withdraw" {
		kind = store.CardSTGBlocked
	}
	c := b5gCard(t, x, f, kind)
	if action == "ack" {
		f.answer(c.QuestionID, "a", store.DecisionAt())
		out, _, err := store.HandleLandingCardAnswer(ctx, x.w.rt, c, f.byID[c.QuestionID].q, store.DecisionAt())
		ok(t, err)
		if out != "applied" {
			t.Fatal(out)
		}
	} else {
		b5gResolveCI(t, x, c, trigger)
	}
	return x, f, c
}

// Serial: DB-anchored clock; two real connections interleave a tightening
// commit after the initial read or Lookup, before the final external write.
func TestStgCardsPGSettlementNoneInterleaving(t *testing.T) {
	for _, action := range []string{"ack", "withdraw"} {
		phases := []string{"enabled", "none_at_entry", "after_read"}
		if action == "withdraw" {
			phases = append(phases, "after_lookup")
		}
		for _, phase := range phases {
			t.Run(action+"/"+phase, func(t *testing.T) {
				x, f, c := stgSettlementFixture(t, action)
				second := x.w.db.Connect(t, x.w.db.Runtime)
				tighten := func() { ok(t, stgSettlementNone(ctx, second)) }
				db := &stgSettlementDB{DB: x.w.rt}
				q := f.source()
				if phase == "none_at_entry" {
					tighten()
				}
				if phase == "after_read" {
					db.afterRead = tighten
				}
				if phase == "after_lookup" {
					db.afterLookup = tighten
				}
				calls := 0
				ack, withdraw := q.Ack, q.Withdraw
				q.Ack = func(ctx context.Context, id string) (bool, error) { calls++; return ack(ctx, id) }
				q.Withdraw = func(ctx context.Context, id, why string) (string, error) { calls++; return withdraw(ctx, id, why) }
				before, cards := x.read(t), cardsNow(t, x.w)
				count := x.w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id IN ('landing:bifrost-platform','landing-cards:bifrost-platform')`)
				result, err := store.SettleLandingCard(ctx, db, q, c, store.DecisionAt())
				ok(t, err)
				want := "acked"
				if action == "withdraw" {
					want = "resolved"
				}
				if phase == "enabled" {
					if result != want || calls != 1 {
						t.Fatal("enabled control", result, calls)
					}
				} else {
					if result != "" || calls != 0 || !reflect.DeepEqual(before, x.read(t)) || !reflect.DeepEqual(cards, cardsNow(t, x.w)) || count != x.w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id IN ('landing:bifrost-platform','landing-cards:bifrost-platform')`) {
						t.Fatal("none wrote question or receipt", result, calls)
					}
					stgSettlementResume(t, x)
					result, err = store.SettleLandingCard(ctx, x.w.rt, f.source(), c, store.DecisionAt())
					ok(t, err)
					if result != want {
						t.Fatal("same card did not resume", result)
					}
				}
				if x.read(t).LastSeq != before.LastSeq {
					t.Fatal("settlement reapplied business")
				}
				if len(cardsNow(t, x.w)) != len(cards) || !cardsNow(t, x.w)[c.Prep].Ended() {
					t.Fatal("replaced or failed to finish original card")
				}
			})
		}
	}
}

// Serial: real advisory locks and safego; no sleep-based concurrency oracle.
// A tightening already waiting at Ack/Withdraw must stay blocked through
// the receipt commit, then become visible. This kills a recheck-only fix.
func TestStgCardsPGSettlementHoldsPolicyThroughWrite(t *testing.T) {
	for _, action := range []string{"ack", "withdraw"} {
		t.Run(action, func(t *testing.T) {
			x, f, c := stgSettlementFixture(t, action)
			second := x.w.db.Connect(t, x.w.db.Runtime)
			observer := x.w.db.Connect(t, x.w.db.Runtime)
			runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			done := make(chan error, 1)
			q := f.source()
			waiting := func() error {
				safego.Go("stg-settlement-rule-tighten-test", func() { done <- stgSettlementNone(runCtx, second) })
				for {
					var blocked bool
					err := observer.QueryRow(runCtx, `SELECT $1::int = ANY(pg_blocking_pids($2::int))`, x.w.rt.PgConn().PID(), second.PgConn().PID()).Scan(&blocked)
					if err != nil {
						return err
					}
					if blocked {
						return nil
					}
					select {
					case err := <-done:
						return fmt.Errorf("tightening finished before question write/receipt: %v", err)
					case <-runCtx.Done():
						return runCtx.Err()
					default:
					}
				}
			}
			ack, withdraw := q.Ack, q.Withdraw
			q.Ack = func(ctx context.Context, id string) (bool, error) {
				if err := waiting(); err != nil {
					return false, err
				}
				return ack(ctx, id)
			}
			q.Withdraw = func(ctx context.Context, id, why string) (string, error) {
				if err := waiting(); err != nil {
					return "", err
				}
				return withdraw(ctx, id, why)
			}
			result, err := store.SettleLandingCard(runCtx, x.w.rt, q, c, store.DecisionAt())
			ok(t, err)
			select {
			case err := <-done:
				ok(t, err)
			case <-runCtx.Done():
				t.Fatal("tightener did not finish", runCtx.Err())
			}
			if result == "" || !cardsNow(t, x.w)[c.Prep].Ended() {
				t.Fatal("question write lacked receipt", result)
			}
			b5AssertRule(t, x.w, store.DecisionAt(), 2, "none")
		})
	}
}
