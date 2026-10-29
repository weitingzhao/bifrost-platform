package store_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// A test-only scheduler selects one mission per CloseMissions caller. The
// reverse variant swaps only the stream lock order, retaining the real SQL,
// row locks, folds and transactions. It must produce PostgreSQL 40P01.
type s66CloseDB struct {
	store.DB
	mission string
	reverse bool
	first   func()
}

func (d s66CloseDB) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	tx, err := d.DB.BeginTx(ctx, o)
	return &s66CloseTx{Tx: tx, config: d, seen: new(int)}, err
}

type s66CloseTx struct {
	pgx.Tx
	config s66CloseDB
	seen   *int
}

func (x *s66CloseTx) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := x.Tx.Begin(ctx)
	return &s66CloseTx{Tx: tx, config: x.config, seen: x.seen}, err
}
func (x *s66CloseTx) Query(ctx context.Context, q string, args ...any) (pgx.Rows, error) {
	if strings.HasPrefix(q, "SELECT m.mission_id FROM agentrt.mission m WHERE") {
		q = strings.Replace(q, " ORDER BY m.mission_id", " AND m.mission_id = $1 ORDER BY m.mission_id", 1)
		args = []any{x.config.mission}
	}
	return x.Tx.Query(ctx, q, args...)
}
func (x *s66CloseTx) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	streamLock := strings.Contains(q, "SELECT 1 FROM agentrt.event_stream") && strings.Contains(q, "FOR UPDATE") && len(args) == 1
	if streamLock && (args[0] == landingRepoStream || args[0] == infraStream) {
		if x.config.reverse {
			if args[0] == landingRepoStream {
				args = []any{infraStream}
			} else {
				args = []any{landingRepoStream}
			}
		}
		tag, err := x.Tx.Exec(ctx, q, args...)
		if err == nil {
			*x.seen++
			if *x.seen == 1 && x.config.first != nil {
				x.config.first()
			}
		}
		return tag, err
	}
	return x.Tx.Exec(ctx, q, args...)
}

func s66ClosingPair(t *testing.T) (*world, time.Time) {
	t.Helper()
	w, at := s66World(t, true, 5)
	w.goalMission(t, "M-4")
	for _, mission := range []string{"M-3", "M-4"} {
		for _, repo := range landing.LandingRepoNames() {
			s66Landed(t, w, mission, repo, at)
		}
	}
	if st := closePass(t, w, at.Add(2*time.Minute)); st != "active" {
		t.Fatal("missing close round", st)
	}
	s66ReadBoth(t, w, at.Add(3*time.Minute))
	store.FreezeEventClock(at.Add(4 * time.Minute))
	return w, at
}

// Serial: event clock, real PG scheduler. T52: sequential control, two
// simultaneous B4 closes, and a reversed-order mutation that actually
// deadlocks. Both missions carry both repositories in every case.
func TestS66T52B4LockOrderPG(t *testing.T) {
	t.Run("sequential control", func(t *testing.T) {
		w, _ := s66ClosingPair(t)
		ids, err := store.CloseMissions(ctx, w.rt)
		ok(t, err)
		if len(ids) != 2 || w.state(t, "mission", "M-3") != "done" || w.state(t, "mission", "M-4") != "done" {
			t.Fatal(ids)
		}
	})
	for _, reverse := range []bool{false, true} {
		name := "ordered"
		if reverse {
			name = "reverse mutation detected"
		}
		t.Run(name, func(t *testing.T) {
			w, _ := s66ClosingPair(t)
			rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			held, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			a := s66CloseDB{DB: w.db.Connect(t, w.db.Runtime), mission: "M-3", first: func() { close(held); <-release }}
			b := s66CloseDB{DB: w.db.Connect(t, w.db.Runtime), mission: "M-4", reverse: reverse}
			done := make(chan error, 2)
			safego.Go("s66.close.a", func() { _, err := store.CloseMissions(rctx, a); done <- err })
			select {
			case <-held:
			case err := <-done:
				t.Fatal("close did not take first stream", err)
			case <-rctx.Done():
				t.Fatal(rctx.Err())
			}
			safego.Go("s66.close.b", func() { _, err := store.CloseMissions(rctx, b); done <- err })
			w.waitFor(t, "SELECT 1 FROM agentrt.event_stream", time.Now().Add(10*time.Second))
			unblock()
			errA, errB := <-done, <-done
			if reverse {
				var pgerr *pgconn.PgError
				if !errors.As(errors.Join(errA, errB), &pgerr) || pgerr.Code != "40P01" {
					t.Fatal("mutation did not prove a deadlock", errA, errB)
				}
			} else {
				ok(t, errA)
				ok(t, errB)
				if w.state(t, "mission", "M-3") != "done" || w.state(t, "mission", "M-4") != "done" {
					t.Fatal("concurrent close differs from control")
				}
			}
		})
	}
}

// Serial: hook and event clock. T52's other edge: the final gate waits on
// B4's mission lock without holding a stream the close needs.
func TestS66T52CloseAgainstSecondGatePG(t *testing.T) {
	w, at := s66World(t, true, 5)
	s66Landed(t, w, "M-3", platformRepo, at)
	root := s66Root(t, w, "M-3", infraRepo)
	op, d, err := store.ClaimLanding(ctx, w.rt, s66Source(), "prod", root, at)
	ok(t, err)
	if !d.Allowed {
		t.Fatal(d)
	}
	prepareS6(t, w, infraRepo, root, op, at)
	store.FreezeEventClock(at.Add(2 * time.Minute))
	held, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	defer store.SetHook(func(point, id string) {
		if point == "close: mission locked" && id == "M-3" {
			close(held)
			<-release
		}
	})()
	dbClose, dbGate := w.db.Connect(t, w.db.Runtime), w.db.Connect(t, w.db.Runtime)
	rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	closed, gate := make(chan error, 1), make(chan s66GateResult, 1)
	safego.Go("s66.close.gate", func() { _, err := store.CloseMissions(rctx, dbClose); closed <- err })
	select {
	case <-held:
	case err := <-closed:
		t.Fatal("close not held", err)
	case <-rctx.Done():
		t.Fatal(rctx.Err())
	}
	safego.Go("s66.gate.close", func() {
		a, d, err := store.AuthorizeLandingPush(rctx, dbGate, s66Source(), op, observed(at), infraRepo)
		gate <- s66GateResult{a, d, err}
	})
	w.waitFor(t, "SELECT tenant FROM agentrt.mission", time.Now().Add(10*time.Second))
	unblock()
	ok(t, <-closed)
	r := <-gate
	ok(t, r.err)
	if !r.decision.Allowed || r.auth.Repo != infraRepo || w.state(t, "mission", "M-3") != "active" {
		t.Fatal("gate/close outcome", r)
	}
}

// Serial: the UTC clock jumps between reserved and main_intent. T55 retains
// the existing platform coverage and proves the infra transaction also
// rolls back its quota and intent, then can rejudge on the new day.
func TestS66T55InfraMidnightRollbackPG(t *testing.T) {
	midnight := time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	w, at := s66World(t, true, 5, midnight.Add(-5*time.Minute))
	root := s66Root(t, w, "M-3", infraRepo)
	// Check the persisted fence against the database's mission clock before
	// exercising midnight. A before_enable refusal is a broken fixture.
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		f, err := store.LandingFacts(ctx, tx, s66Source(), "prod", "live", root, "", "", at)
		if err != nil {
			return err
		}
		if f.EnabledAt.IsZero() || f.MissionCreated.Before(f.EnabledAt) || f.AcceptedAt.Before(f.EnabledAt) {
			t.Fatalf("fixture fence=%s mission=%s accepted=%s", f.EnabledAt, f.MissionCreated, f.AcceptedAt)
		}
		return nil
	}))
	op, d, err := store.ClaimLanding(ctx, w.rt, s66Source(), "prod", root, at)
	ok(t, err)
	if !d.Allowed || op == "" {
		t.Fatal(d)
	}
	prepareS6(t, w, infraRepo, root, op, at)
	start := at.Add(2 * time.Minute)
	next := start.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	store.FreezeEventClock(start)
	reserved, intents := 0, 0
	restore := store.SetHook(func(point, id string) {
		if id != op {
			return
		}
		if point == "landing gate: reserved" {
			reserved++
			store.FreezeEventClock(next.Add(time.Second))
		}
		if point == "landing gate: before main_intent" {
			intents++
		}
	})
	defer restore()
	resultReads, historyReads, observationReads := 0, 0, 0
	src := s66Source()
	result, history := src.Result, src.History
	src.Result = func(c context.Context, repo, task, attempt, base, commit string) (bool, []rules.LandingChange, error) {
		resultReads++
		return result(c, repo, task, attempt, base, commit)
	}
	src.History = func(c context.Context, repo, task, commit, snapshot string) (landing.MainHistory, error) {
		historyReads++
		return history(c, repo, task, commit, snapshot)
	}
	observe := func(c context.Context, o landing.Operation) (store.LiveObservation, error) {
		observationReads++
		return observed(at)(c, o)
	}
	a, d, err := store.AuthorizeLandingPush(ctx, w.rt, src, op, observe, infraRepo)
	restore()
	if !errors.Is(err, store.ErrLandingDayChanged) || reserved != 1 || intents != 1 || a != (store.Authorization{}) || d.Allowed {
		t.Fatal("did not roll back between records", reserved, intents, a, d, err)
	}
	if landingState(t, w, infraStream).Operations[op].Phase != "branch_pushed" {
		t.Fatal("intent survived rollback")
	}
	oldQuota := "landing-quota:dev:" + start.Format("2006-01-02")
	newQuota := "landing-quota:dev:" + next.Format("2006-01-02")
	for _, stream := range []string{oldQuota, newQuota} {
		if len(landingState(t, w, stream).Reservations) != 0 {
			t.Fatal("reservation survived rollback", stream)
		}
	}
	var records int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE body_json->'landing'->>'op'=$1
      AND body_json->'landing'->>'kind' IN ('ci_seen','reserved','main_intent')`, op).Scan(&records))
	if records != 0 {
		t.Fatal("gate records survived rollback", records)
	}
	if resultReads != 1 || historyReads != 1 || observationReads != 1 {
		t.Fatal("first gate skipped evidence", resultReads, historyReads, observationReads)
	}
	// Retry the entire gate on the new day; its root is still within six hours.
	store.FreezeEventClock(next.Add(time.Second))
	a, d, err = store.AuthorizeLandingPush(ctx, w.rt, src, op, observe, infraRepo)
	ok(t, err)
	if !d.Allowed || a.QuotaDay != next.Format("2006-01-02") || a.Repo != infraRepo || a.Root != root || a.Op != op {
		t.Fatal("stable-day control", a, d)
	}
	if resultReads != 2 || historyReads != 2 || observationReads != 2 {
		t.Fatal("new-day gate did not reread evidence", resultReads, historyReads, observationReads)
	}
	if len(landingState(t, w, oldQuota).Reservations) != 0 {
		t.Fatal("retry charged the old day")
	}
	q := landingState(t, w, newQuota)
	if len(q.Reservations) != 1 || q.Reservations[root] != op || landingState(t, w, infraStream).Operations[op].Phase != "main_intent" {
		t.Fatal("retry did not commit one new-day reservation and intent", q.Reservations)
	}
}
