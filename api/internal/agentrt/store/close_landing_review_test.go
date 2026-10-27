package store_test

// Work 638 (Codex review of the S2 hold): its probes, kept as regressions for
// the round-2 fix. Database cases use the migrated PostgreSQL test harness.
import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Serial: FreezeEventClock (through goalDrill). The normal controller can run
// Close between acceptance and the next drill tick. At that point an eligible
// task must still get its first landing turn.
func TestWork638FirstClaimSurvivesCloseTick(t *testing.T) {
	w, task, src := goalDrill(t, "M-3")
	closed, err := store.CloseMissions(ctx, w.rt)
	ok(t, err)
	op, d, err := store.ClaimLandingDrill(ctx, w.rt, src, task, time.Now())
	ok(t, err)
	if !d.Allowed || op == "" {
		t.Fatalf("eligible S2 task lost before its first drill tick: closed=%v decision=%+v", closed, d)
	}
}

// Removing the drill adapters models AGENTRT_LANDING_DRILL=off after restart.
// Serial: FreezeEventClock. No pass can settle anything, yet the hold ends.
func TestWork638DisabledDrillDoesNotHoldForever(t *testing.T) {
	for _, phase := range []string{"claimed", "parked", "discarded"} {
		t.Run(phase, func(t *testing.T) {
			w, task, src := goalDrill(t, "M-3")
			var op string
			at := time.Now()
			if phase == "claimed" {
				claimed, d, err := store.ClaimLandingDrill(ctx, w.rt, src, task, at)
				ok(t, err)
				op = claimed
				if op == "" || !d.Allowed {
					t.Fatalf("fixture claim refused: %+v", d)
				}
			} else {
				op, at = pushedDrill(t, w, task, src)
				if phase == "parked" {
					ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "parked", "why", "rule", "retry_at", at.Add(time.Minute).Format(time.RFC3339Nano))))
				} else {
					ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "discarded", "why", "main_changed")))
				}
			}
			later := at.Add(7 * time.Hour)
			restore := store.FreezeEventClock(later)
			defer restore()
			c := controller.Controller{DB: w.rt, Env: "stg", Now: func() time.Time { return later }}
			ok(t, c.LandingDrillPass(ctx))
			_, err := c.Close(ctx)
			ok(t, err)
			if got := w.state(t, "mission", "M-3"); got == "active" {
				t.Fatalf("after seven hours with drill disabled: mission=%s latest=%s", got, landingState(t, w, landingRepoStream).Operations[op].Phase)
			}
		})
	}
}

// Place a legal plan rebind immediately after the claim acquired its mission
// lock. A separate connection runs each intervening transaction.
type work638AfterMissionDB struct {
	store.DB
	after func() error
}

func (d work638AfterMissionDB) BeginTx(c context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := d.DB.BeginTx(c, opts)
	if err != nil {
		return nil, err
	}
	return &work638AfterMissionTx{Tx: tx, after: d.after}, nil
}

type work638AfterMissionTx struct {
	pgx.Tx
	after func() error
}

func (tx *work638AfterMissionTx) Exec(c context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	tag, err := tx.Tx.Exec(c, q, args...)
	if err == nil && tx.after != nil && strings.Contains(q, "FROM agentrt.mission WHERE mission_id = $1 FOR SHARE") {
		f := tx.after
		tx.after = nil
		err = f()
	}
	return tag, err
}

// Serial: FreezeEventClock (acceptedDrill). A legal step rebind that tries to
// move the task while the claim holds its locks waits for the claim: the claim
// reads, locks and records the task under one mission. At 89722fd8 (mission
// lock only) the rebind went through mid-claim.
func TestWork638ClaimLocksTheMissionUsedByFacts(t *testing.T) {
	w, task, src := acceptedDrill(t)
	w.goalMission(t, "M-3")
	a := &asker{}
	_, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "work638-rebind", root, "move accepted work", []store.PlanChange{{Op: "rebind_step", Node: s1, MissionID: "M-3"}}, a.ask)
	ok(t, err)
	a.answer(a.lastID(), store.QuestionAnswered, store.CardApplyKey)
	tried := false
	db := work638AfterMissionDB{DB: w.db.Pool(t, w.db.Runtime), after: func() error {
		tried = true
		short, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		defer cancel()
		if n, e := store.DecidePlanChanges(short, w.rt, a.question, nil); e == nil && n == 1 {
			return fmt.Errorf("the rebind moved the task while the claim held it")
		}
		return nil
	}}
	op, d, err := store.ClaimLandingDrill(ctx, db, src, task, time.Now())
	ok(t, err)
	if !tried || op == "" || !d.Allowed {
		t.Fatalf("fixture: tried=%v op=%s decision=%+v", tried, op, d)
	}
	n, err := store.DecidePlanChanges(ctx, w.rt, a.question, nil)
	ok(t, err)
	if n != 1 {
		t.Fatalf("rebind did not apply after the claim: %d", n)
	}
}
