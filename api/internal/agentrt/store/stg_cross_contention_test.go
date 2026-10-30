package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// Serial: real PostgreSQL locks, global event clock and local Git PATH.
// An independent connection holds the mission exactly as B4 Close does;
// pg_blocking_pids must show the sender waiting, before any debit or Create.
func TestB5HCrossPGMissionLockContention(t *testing.T) {
	x := b5hFixture(t)
	locker := x.w.db.Connect(t, x.w.db.Runtime)
	observer := x.w.db.Connect(t, x.w.db.Runtime)
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := locker.BeginTx(bounded, pgx.TxOptions{})
	ok(t, err)
	defer tx.Rollback(ctx)
	_, err = tx.Exec(bounded, `SELECT 1 FROM agentrt.mission WHERE mission_id='M-3' FOR UPDATE`)
	ok(t, err)
	done := make(chan error, 1)
	safego.Go("b5h-mission-contention", func() { done <- x.c.Pass(bounded) })
	for {
		var blocked bool
		err = observer.QueryRow(bounded, `SELECT $2::int=ANY(pg_blocking_pids($1::int))`, x.w.rt.PgConn().PID(), locker.PgConn().PID()).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case err := <-done:
			t.Fatal("sender crossed locked mission", err)
		case <-bounded.Done():
			t.Fatal("mission wait not observed")
		case <-time.After(10 * time.Millisecond):
		}
	}
	var started int
	ok(t, observer.QueryRow(bounded, `SELECT count(*) FROM agentrt.event WHERE event_stream_id='landing:bifrost-platform' AND body_json->'stg_release'->>'kind'='started'`).Scan(&started))
	if started != 0 {
		t.Fatal("permit committed while mission decision unresolved")
	}
	ok(t, tx.Commit(bounded))
	select {
	case err := <-done:
		ok(t, err)
	case <-bounded.Done():
		t.Fatal("sender did not resume")
	}
	if x.runs.creates != 1 || len(x.runs.uids) != 1 || b5hCount(x.state(t), "started") != 1 {
		t.Fatal("unlocked positive control did not send exactly once")
	}
}
