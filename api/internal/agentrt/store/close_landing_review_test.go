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
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
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

func (tx *work638AfterMissionTx) QueryRow(c context.Context, q string, args ...any) pgx.Row {
	row := tx.Tx.QueryRow(c, q, args...)
	if tx.after != nil && strings.Contains(q, "FROM agentrt.mission WHERE mission_id = $1 FOR SHARE") {
		after := tx.after
		tx.after = nil
		return work638AfterMissionRow{Row: row, after: after}
	}
	return row
}

type work638AfterMissionRow struct {
	pgx.Row
	after func() error
}

func (r work638AfterMissionRow) Scan(dest ...any) error {
	// QueryRow is lazy: the mission lock is known to be held only after
	// Scan succeeds. S6-6 uses this shared binding reader, no longer Exec.
	if err := r.Row.Scan(dest...); err != nil {
		return err
	}
	return r.after()
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
	var originalMission string
	ok(t, w.rt.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id=$1`, task).Scan(&originalMission))
	claim := w.db.Connect(t, w.db.Runtime)
	tried := 0
	db := work638AfterMissionDB{DB: claim, after: func() error {
		tried++
		rebind := w.db.Connect(t, w.db.Runtime)
		short, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		done := make(chan struct{})
		var n int
		var rebindErr error
		safego.Go("work638.rebind", func() {
			defer close(done)
			n, rebindErr = store.DecidePlanChanges(short, rebind, a.question, nil)
		})
		defer func() { cancel(); <-done }()
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			var blocked bool
			if err := w.rt.QueryRow(short, `SELECT $1::int = ANY(pg_blocking_pids($2::int))`,
				claim.PgConn().PID(), rebind.PgConn().PID()).Scan(&blocked); err != nil {
				return err
			}
			if blocked {
				cancel()
				<-done
				if n != 0 || rebindErr == nil {
					return fmt.Errorf("blocked rebind applied: n=%d err=%v", n, rebindErr)
				}
				return nil
			}
			select {
			case <-done:
				return fmt.Errorf("rebind did not wait for claim: n=%d err=%v", n, rebindErr)
			case <-short.Done():
				return fmt.Errorf("rebind lock wait not observed: %w", short.Err())
			case <-tick.C:
			}
		}
	}}
	op, d, err := store.ClaimLandingDrill(ctx, db, src, task, time.Now())
	ok(t, err)
	if tried != 2 || op == "" || !d.Allowed {
		t.Fatalf("fixture: tried=%d op=%s decision=%+v", tried, op, d)
	}
	var taskMission, recordedMission string
	ok(t, w.rt.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id=$1`, task).Scan(&taskMission))
	ok(t, w.rt.QueryRow(ctx, `SELECT mission_id FROM agentrt.event WHERE event_stream_id=$1
      AND body_json->'landing'->>'kind'='claimed' AND body_json->'landing'->>'op'=$2`, landingRepoStream, op).Scan(&recordedMission))
	if taskMission != originalMission || recordedMission != originalMission {
		t.Fatalf("claim changed mission: task=%s record=%s original=%s", taskMission, recordedMission, originalMission)
	}
	n, err := store.DecidePlanChanges(ctx, w.rt, a.question, nil)
	ok(t, err)
	if n != 1 {
		t.Fatalf("rebind did not apply after the claim: %d", n)
	}
	ok(t, w.rt.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id=$1`, task).Scan(&taskMission))
	if taskMission != "M-3" {
		t.Fatalf("successful rebind left task in %s", taskMission)
	}
}
