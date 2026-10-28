package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Review 645 R1: a retry answered while Close decides. Close reads the
// landing stream inside its hold savepoint, then closes the mission. On
// 7fe6b34a a retry committed in between (the answer locked only the stream)
// and the mission closed under a pending root. Now the answer takes the
// task, then the mission: it waits for the close, then finds the mission done
// and is refused. The interception (review 645's) starts the answer on its
// own connection right after Close's hold savepoint and checks it is blocked
// on the close's backend before letting the close go on.

type answerAfterHoldDB struct {
	store.DB
	after func() error
}
type answerAfterHoldTx struct {
	pgx.Tx
	after  func() error
	nested bool
}

func (d answerAfterHoldDB) BeginTx(c context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := d.DB.BeginTx(c, opts)
	if err != nil {
		return nil, err
	}
	return &answerAfterHoldTx{Tx: tx, after: d.after}, nil
}
func (tx *answerAfterHoldTx) Begin(c context.Context) (pgx.Tx, error) {
	sp, err := tx.Tx.Begin(c)
	if err != nil {
		return nil, err
	}
	return &answerAfterHoldTx{Tx: sp, after: tx.after, nested: true}, nil
}
func (tx *answerAfterHoldTx) Commit(c context.Context) error {
	if err := tx.Tx.Commit(c); err != nil {
		return err
	}
	if tx.nested && tx.after != nil {
		return tx.after()
	}
	return nil
}

// Serial: FreezeEventClock.
func TestRetryAnsweredDuringCloseIsRefused(t *testing.T) {
	w, task, src := goalDrill(t, "M-3")
	at := time.Now().UTC().Truncate(time.Microsecond)
	defer store.FreezeEventClock(at)()
	op, d, err := store.ClaimLandingDrill(ctx, w.rt, src, task, at)
	ok(t, err)
	if op == "" || !d.Allowed {
		t.Fatal("fixture claim", d)
	}
	store.FreezeEventClock(at.Add(time.Minute))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "failed", "why", "stale")))
	store.FreezeEventClock(at.Add(2 * time.Minute))

	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	pool := w.db.Pool(t, w.db.Runtime)
	closeConn, err := pool.Acquire(bounded)
	ok(t, err)
	defer closeConn.Release()
	answerConn, err := pool.Acquire(bounded)
	ok(t, err)
	defer answerConn.Release()
	answered := make(chan error, 1)
	started := false
	db := answerAfterHoldDB{DB: closeConn.Conn(), after: func() error {
		if started {
			return nil
		}
		started = true
		go func() {
			answered <- store.InTx(bounded, answerConn.Conn(), func(tx pgx.Tx) error {
				return store.ApplyLandingAnswer(bounded, tx, task, op, "try_again", "explicit", "owner", at.Add(2*time.Minute))
			})
		}()
		deadline := time.Now().Add(5 * time.Second)
		for {
			var waiting bool
			if err := w.rt.QueryRow(ctx, `SELECT $2::integer = ANY(pg_blocking_pids($1::integer))`,
				answerConn.Conn().PgConn().PID(), closeConn.Conn().PgConn().PID()).Scan(&waiting); err != nil {
				return err
			}
			if waiting {
				return nil
			}
			select {
			case e := <-answered:
				answered <- e
				return nil // committed without waiting: the assertions below fail
			default:
			}
			if !time.Now().Before(deadline) {
				return errors.New("the answer neither waited for the close nor finished")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}}
	closed, err := store.CloseMissions(bounded, db)
	ok(t, err)
	if !started {
		t.Fatal("the probe did not intercept the hold savepoint")
	}
	e := <-answered
	if len(closed) != 1 || w.state(t, "mission", "M-3") != "done" {
		t.Fatalf("fixture: the close should decide first: closed=%v answer=%v", closed, e)
	}
	if !errors.Is(e, store.ErrLandingMissionClosed) {
		t.Fatalf("a retry under a closed mission was not refused: %v", e)
	}
	if s := landingState(t, w, landingRepoStream); s.Operations[op].Retried || s.RootPending(task) {
		t.Fatalf("retry recorded under a closed mission: %+v", s.Retries[task])
	}
}
