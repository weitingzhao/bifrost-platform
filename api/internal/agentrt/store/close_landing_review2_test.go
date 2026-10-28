package store_test

// Work 640 (Codex, round-2 review of the S2 hold): its probes, kept as
// regressions. All clock changes are serial.
import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
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

// Serial: FreezeEventClock. A close that runs while a claim reads Git
// (TD-447 5: the claim holds no lock then). A root never claimed is queued by
// its facts until the stream records its claim or its refusal (TD-447 4), so
// a close during the reads keeps the mission however long they take (the old
// two-minute grace lost this claim past it, review 646 §5). Only past the
// root's window (the loop never got to it) may the close end the mission,
// and then it records the root failed{bound} first; the claim's second,
// locked read sees the mission done and claims nothing: never an operation
// under a closed mission, never a silent miss.
func TestConcurrentCloseDuringClaimReads(t *testing.T) {
	for _, c := range []struct {
		name   string
		after  time.Duration
		closes bool
	}{{"right after acceptance", 0, false}, {"past the old two-minute grace", 3 * time.Minute, false},
		{"an hour into the reads", time.Hour, false}, {"past the window", 6*time.Hour + time.Minute, true}} {
		t.Run(c.name, func(t *testing.T) {
			w, task, src := goalDrill(t, "M-3")
			restore := store.FreezeEventClock(time.Now().Add(c.after))
			defer restore()
			bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			pool := w.db.Pool(t, w.db.Runtime)
			claimConn, err := pool.Acquire(bounded)
			ok(t, err)
			defer claimConn.Release()
			closeConn, err := pool.Acquire(bounded)
			ok(t, err)
			defer closeConn.Release()
			entered, release := make(chan struct{}), make(chan struct{})
			result := src.Result
			src.Result = func(cx context.Context, repo, id, attempt, base, commit string) (bool, []rules.LandingChange, error) {
				close(entered)
				select {
				case <-release:
				case <-cx.Done():
					return false, nil, cx.Err()
				}
				return result(cx, repo, id, attempt, base, commit)
			}
			type claimResult struct {
				op string
				d  landingDecision
				e  error
			}
			claimed := make(chan claimResult, 1)
			go func() {
				op, d, e := store.ClaimLandingDrill(bounded, claimConn.Conn(), src, task, time.Now())
				claimed <- claimResult{op, landingDecision{d.Allowed, d.Reason}, e}
			}()
			select {
			case <-entered:
			case <-bounded.Done():
				t.Fatal("claim never reached external source")
			}
			// The claim holds no transaction now: the close is not blocked
			// (on the version that read Git under the mission lock it waits
			// here until the bounded context ends).
			ids, err := store.CloseMissions(bounded, closeConn.Conn())
			ok(t, err)
			if slices.Contains(ids, "M-3") != c.closes {
				t.Fatalf("close during the reads: closed=%v, want closes=%v", ids, c.closes)
			}
			close(release)
			r := <-claimed
			ok(t, r.e)
			ops := landingState(t, w, landingRepoStream).Operations
			if c.closes {
				o := ops["land:"+task+":1"]
				if r.op != "" || r.d.allowed || r.d.reason != "stale" || len(ops) != 1 || o.Phase != "failed" || o.Outcome != "bound" {
					t.Fatalf("claimed under a closed mission, or left no record: op=%q decision=%+v ops=%+v", r.op, r.d, ops)
				}
				return
			}
			if r.op == "" || !r.d.allowed || len(ops) != 1 || ops[r.op].Phase != "claimed" {
				t.Fatalf("claim lost to a close during its reads: op=%q decision=%+v", r.op, r.d)
			}
			if w.state(t, "mission", "M-3") != "active" {
				t.Fatal("closed under a committed claim")
			}
		})
	}
}

type landingDecision struct {
	allowed bool
	reason  string
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

// Serial: FreezeEventClock via goalDrill. TD-447 5: while the claim reads Git
// it holds no lock and no transaction. Another transaction takes the mission
// row exclusively with NOWAIT, on another connection; on the version that read Git inside the
// locked transaction the NOWAIT fails.
func TestClaimReadsGitWithNoLockHeld(t *testing.T) {
	w, task, src := goalDrill(t, "M-3")
	pool := w.db.Pool(t, w.db.Runtime)
	claimConn, err := pool.Acquire(ctx)
	ok(t, err)
	defer claimConn.Release()
	otherConn, err := pool.Acquire(ctx)
	ok(t, err)
	defer otherConn.Release()
	result := src.Result
	var during error
	src.Result = func(cx context.Context, repo, id, attempt, base, commit string) (bool, []rules.LandingChange, error) {
		during = store.InTx(cx, otherConn.Conn(), func(tx pgx.Tx) error {
			_, e := tx.Exec(cx, `SELECT 1 FROM agentrt.mission WHERE mission_id = 'M-3' FOR UPDATE NOWAIT`)
			return e
		})
		return result(cx, repo, id, attempt, base, commit)
	}
	op, d, err := store.ClaimLandingDrill(ctx, claimConn.Conn(), src, task, time.Now())
	ok(t, err)
	if during != nil {
		t.Fatalf("the claim held the mission while reading Git: %v", during)
	}
	if op == "" || !d.Allowed {
		t.Fatal("claim refused", d)
	}
}

// openTxDB counts the transactions open on it, so a test can see whether a
// claim's external reads run inside one (review 646 R646-2: an empty
// transaction around them would still take a pool connection).
type openTxDB struct {
	store.DB
	open *atomic.Int32
}

func (d openTxDB) BeginTx(c context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := d.DB.BeginTx(c, opts)
	if err != nil {
		return nil, err
	}
	d.open.Add(1)
	return &openTx{Tx: tx, open: d.open}, nil
}

type openTx struct {
	pgx.Tx
	open *atomic.Int32
	done bool
}

func (tx *openTx) end() {
	if !tx.done {
		tx.done = true
		tx.open.Add(-1)
	}
}
func (tx *openTx) Commit(c context.Context) error   { defer tx.end(); return tx.Tx.Commit(c) }
func (tx *openTx) Rollback(c context.Context) error { defer tx.end(); return tx.Tx.Rollback(c) }

// Serial: FreezeEventClock via goalDrill. TD-447 5, review 646: every
// external read of a claim (the result, main's history, each dependency's
// history) runs with no transaction of the claim open.
func TestClaimEvidenceReadsOpenNoTransaction(t *testing.T) {
	w, task, src := goalDrill(t, "M-3")
	var open atomic.Int32
	db := openTxDB{DB: w.rt, open: &open}
	var seen []string
	result, history := src.Result, src.History
	src.Result = func(cx context.Context, repo, id, attempt, base, commit string) (bool, []rules.LandingChange, error) {
		seen = append(seen, fmt.Sprintf("result open=%d", open.Load()))
		return result(cx, repo, id, attempt, base, commit)
	}
	src.History = func(cx context.Context, repo, id, commit, snapshot string) (landing.MainHistory, error) {
		seen = append(seen, fmt.Sprintf("history open=%d", open.Load()))
		return history(cx, repo, id, commit, snapshot)
	}
	op, d, err := store.ClaimLandingDrill(ctx, db, src, task, time.Now())
	ok(t, err)
	if op == "" || !d.Allowed {
		t.Fatal("claim refused", d)
	}
	if len(seen) < 2 {
		t.Fatal("no external reads seen", seen)
	}
	for _, s := range seen {
		if !strings.HasSuffix(s, "open=0") {
			t.Fatalf("an external read ran inside a transaction: %v", seen)
		}
	}
	if open.Load() != 0 {
		t.Fatal("a transaction left open", open.Load())
	}
}

// Serial: FreezeEventClock via goalDrill. The evidence's keys move while it
// is read (here the task's base, as a rebase would): the claim refuses
// "changed" and writes nothing. Review 646's mutants that compare less than
// the keys claim on the stale evidence.
func TestClaimRefusesEvidenceWhoseKeysMoved(t *testing.T) {
	w, task, src := goalDrill(t, "M-3")
	admin := w.db.Connect(t, w.db.Admin)
	result := src.Result
	src.Result = func(cx context.Context, repo, id, attempt, base, commit string) (bool, []rules.LandingChange, error) {
		tx, e := admin.Begin(cx)
		if e != nil {
			return false, nil, e
		}
		defer func() { _ = tx.Rollback(cx) }()
		if _, e = tx.Exec(cx, `SET LOCAL session_replication_role = replica`); e != nil {
			return false, nil, e
		}
		if _, e = tx.Exec(cx, `UPDATE agentrt.task SET base_commit = $2 WHERE task_id = $1`, task, commitA); e != nil {
			return false, nil, e
		}
		if e = tx.Commit(cx); e != nil {
			return false, nil, e
		}
		return result(cx, repo, id, attempt, base, commit)
	}
	op, d, err := store.ClaimLandingDrill(ctx, w.rt, src, task, time.Now())
	ok(t, err)
	if op != "" || d.Allowed || d.Reason != "changed" || len(landingState(t, w, landingRepoStream).Operations) != 0 {
		t.Fatalf("claimed on evidence for a moved base: op=%q decision=%+v", op, d)
	}
}

// Serial: FreezeEventClock via goalDrill. Review 646 N646-1: another root's
// operation holds the only in-flight slot when the claim starts and ends
// while the claim reads Git; the claim judges the slot on its second read
// and lands, instead of throwing its reads away as "changed".
func TestClaimUsesASlotFreedDuringItsReads(t *testing.T) {
	w, task, src := goalDrill(t, "M-3")
	other := "t_01ARZ3NDEKTSV4RRFFQ69G5FAW"
	otherOp := "land:" + other + ":1"
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(otherOp, "claimed", "task", other, "attempt", landingAttempt, "A", commitA, "rule_version", 1)))
	result := src.Result
	src.Result = func(cx context.Context, repo, id, attempt, base, commit string) (bool, []rules.LandingChange, error) {
		if e := appendLanding(w, landingRepoStream, newLandingRecord(otherOp, "failed", "why", "stale")); e != nil {
			return false, nil, e
		}
		return result(cx, repo, id, attempt, base, commit)
	}
	op, d, err := store.ClaimLandingDrill(ctx, w.rt, src, task, time.Now())
	ok(t, err)
	if op == "" || !d.Allowed {
		t.Fatalf("a slot freed during the reads was not used: %+v", d)
	}
}
