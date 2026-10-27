package store_test

// Work 640 (Codex, round-2 review of the S2 hold): its probes, kept as
// regressions. All clock changes are serial.
import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Serial: FreezeEventClock. A k+1 claim does not restart the root's
// wall-clock budget; an explicit retry does. Exact 6h15m is outside the hold.
func TestWork640RootHoldDeadlineAndRetry(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(fmt.Sprint(retry), func(t *testing.T) {
			w, task, src := goalDrill(t, "M-3")
			op, _, err := store.ClaimLandingDrill(ctx, w.rt, src, task, time.Now())
			ok(t, err)
			first := landingState(t, w, landingRepoStream).Operations[op].ClaimedAt
			restore := store.FreezeEventClock(first.Add(time.Hour))
			defer restore()
			if retry {
				ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "failed", "why", "stale")))
				ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "retry", "root", task, "after_op", op, "reason", "again", "by", "owner")))
				first = first.Add(time.Hour)
			} else {
				ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "prepared", "R", commitB, "M0", commitA, "committer_date", first.Format(time.RFC3339Nano), "dry_run", true)))
				ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "branch_intent", "ref", "refs/heads/land/stg/"+task+"/1", "R", commitB)))
				ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "discarded", "why", "main_changed")))
			}
			ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op+":next", "claimed", "task", task, "attempt", landingAttempt, "A", commitA, "rule_version", 1)))
			deadline := first.Add(6*time.Hour + 15*time.Minute)
			store.FreezeEventClock(deadline.Add(-time.Microsecond))
			closed, err := store.CloseMissions(ctx, w.rt)
			ok(t, err)
			if slices.Contains(closed, "M-3") {
				t.Fatal("released before root deadline")
			}
			store.FreezeEventClock(deadline)
			closed, err = store.CloseMissions(ctx, w.rt)
			ok(t, err)
			if !slices.Contains(closed, "M-3") {
				t.Fatal("not released at root deadline")
			}
		})
	}
}

// Serial: FreezeEventClock. Red if the mission SHARE is removed. Pin both backend PIDs
// instead of searching the truncated tail of pg_stat_activity.query. Expire
// the grace before the overlap so it cannot mask a missing lock.
func TestWork640ConcurrentClaimHoldsClose(t *testing.T) {
	w, task, src := goalDrill(t, "M-3")
	restore := store.FreezeEventClock(time.Now().Add(3 * time.Minute))
	defer restore()
	pool := w.db.Pool(t, w.db.Runtime)
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	claimConn, err := pool.Acquire(bounded)
	ok(t, err)
	defer claimConn.Release()
	closeConn, err := pool.Acquire(bounded)
	ok(t, err)
	defer closeConn.Release()
	entered, release := make(chan struct{}), make(chan struct{})
	result := src.Result
	src.Result = func(c context.Context, repo, id, attempt, base, commit string) (bool, []rules.LandingChange, error) {
		close(entered)
		select {
		case <-release:
		case <-c.Done():
			return false, nil, c.Err()
		}
		return result(c, repo, id, attempt, base, commit)
	}
	claimed := make(chan error, 1)
	go func() {
		op, d, e := store.ClaimLandingDrill(bounded, claimConn.Conn(), src, task, time.Now())
		if e == nil && (op == "" || !d.Allowed) {
			e = fmt.Errorf("claim refused: %+v", d)
		}
		claimed <- e
	}()
	defer func() { cancel(); <-claimed }()
	select {
	case <-entered:
	case <-bounded.Done():
		t.Fatal("claim never reached external source")
	}
	closed := make(chan error, 1)
	go func() {
		ids, e := store.CloseMissions(bounded, closeConn.Conn())
		if e == nil && len(ids) != 0 {
			e = fmt.Errorf("closed during claim: %v", ids)
		}
		closed <- e
	}()
	defer func() { cancel(); <-closed }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		ok(t, w.rt.QueryRow(ctx, `SELECT $2::integer = ANY(pg_blocking_pids($1::integer))`, closeConn.Conn().PgConn().PID(), claimConn.Conn().PgConn().PID()).Scan(&waiting))
		if waiting {
			break
		}
		if !time.Now().Before(deadline) {
			t.Fatal("close never waited on the claim backend")
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	// Keep results for the deferred joins after checking them.
	e := <-claimed
	claimed <- e
	ok(t, e)
	e = <-closed
	closed <- e
	ok(t, e)
	if w.state(t, "mission", "M-3") != "active" {
		t.Fatal("closed under a committed claim")
	}
}

var work640Fatal = errors.New("work640 later mission lock failed")

type work640FaultDB struct {
	store.DB
	fatal bool
}

func (d work640FaultDB) BeginTx(c context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := d.DB.BeginTx(c, opts)
	return &work640FaultTx{Tx: tx, fatal: d.fatal}, err
}

type work640FaultTx struct {
	pgx.Tx
	fatal bool
}

func (tx *work640FaultTx) Begin(c context.Context) (pgx.Tx, error) {
	sp, err := tx.Tx.Begin(c)
	return &work640FaultTx{Tx: sp, fatal: tx.fatal}, err
}

func (tx *work640FaultTx) Query(c context.Context, q string, args ...any) (pgx.Rows, error) {
	if strings.Contains(q, "body_json->'landing'") {
		// A real SQL error poisons the subtransaction: fake Go errors would
		// not verify that rollback-to-savepoint recovers PostgreSQL state.
		return tx.Tx.Query(c, `SELECT 1 / 0`)
	}
	return tx.Tx.Query(c, q, args...)
}

type work640ErrorRow struct{ err error }

func (r work640ErrorRow) Scan(...any) error { return r.err }

func (tx *work640FaultTx) QueryRow(c context.Context, q string, args ...any) pgx.Row {
	if tx.fatal && strings.Contains(q, "FOR NO KEY UPDATE OF m") && len(args) > 0 && args[0] == "M-5" {
		return work640ErrorRow{work640Fatal}
	}
	return tx.Tx.QueryRow(c, q, args...)
}

// Serial: FreezeEventClock. An unreadable landing stream holds only its
// mission; a later fatal error still returns the landing error collected
// before it (true).
func TestWork640CloseErrorIsolationAndJoin(t *testing.T) {
	for _, fatal := range []bool{false, true} {
		t.Run(fmt.Sprint(fatal), func(t *testing.T) {
			w := newKernelWorld(t)
			for _, id := range []string{"M-3", "M-4", "M-5"} {
				w.goalMission(t, id)
				role := "brief"
				if id == "M-3" {
					role = "implement"
				}
				w.accept(t, w.ready(t, store.Task{MissionID: id, Role: role, OffPlanReason: "error isolation probe"}))
			}
			closed, err := store.CloseMissions(ctx, work640FaultDB{DB: w.rt, fatal: fatal})
			if w.state(t, "mission", "M-3") != "active" || !slices.Contains(closed, "M-4") {
				t.Fatalf("error not isolated: closed=%v error=%v", closed, err)
			}
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != "22012" {
				t.Fatalf("original landing SQL error was lost: %v", err)
			}
			if fatal && !errors.Is(err, work640Fatal) {
				t.Fatalf("later fatal error was lost: %v", err)
			}
		})
	}
}
