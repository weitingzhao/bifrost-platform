package store_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// waitingOnGraphLock waits until a session of the test database waits for
// the debt graph lock (742132), or fails.
func (dw *debtWorld) waitingOnGraphLock(t *testing.T) {
	t.Helper()
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		var n int
		ok(t, dw.rt.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted AND objid = 742132
		  AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&n))
		if n > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("nobody waits for the debt graph lock")
		}
	}
}

func edgeInsertSQL(id int64, from, to, actor string) string {
	return fmt.Sprintf(`INSERT INTO agentrt.debt_edge (debt_edge_id, edge_kind, from_debt_id, to_debt_id, needs, created_by_actor_id)
	  OVERRIDING SYSTEM VALUE VALUES (%d, 'blocks', '%s', '%s', 'closed', '%s')`, id, from, to, actor)
}

// The database's own graph lock (review 631 S-1): two transactions past the
// store each write half of a cycle; the second waits for the first's lock
// and, once the first commits, sees its half and is refused.
func TestDebtGraphLockInTheDatabase(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	D, E := dw.mustFile(t, "stg", "d"), dw.mustFile(t, "stg", "e")
	half := func(conn *pgx.Conn, id int64, from, to string) (pgx.Tx, error) {
		// No t.Fatal here: the second half runs on its own goroutine.
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			return nil, err
		}
		if _, err = store.Append(ctx, tx, de(from, "debt_edge_added", dw.crew.Actor, map[string]any{"debt_edge_id": id, "edge_kind": "blocks",
			"from_debt_id": from, "to_debt_id": to, "needs": "closed"})); err != nil {
			return tx, fmt.Errorf("the half's event does not append: %w", err)
		}
		_, err = tx.Exec(ctx, edgeInsertSQL(id, from, to, dw.crew.Actor))
		return tx, err
	}
	first, err := half(dw.db.Connect(t, dw.db.Runtime), 900701, D, E)
	ok(t, err)
	second, conn2 := make(chan error, 1), dw.db.Connect(t, dw.db.Runtime)
	go func() {
		tx, err := half(conn2, 900702, E, D)
		if err == nil {
			err = tx.Commit(ctx)
		}
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
		second <- err
	}()
	dw.waitingOnGraphLock(t)
	select {
	case err := <-second:
		t.Fatalf("the second half finished while the first held the lock: %v", err)
	default:
	}
	ok(t, first.Commit(ctx))
	wantErr(t, <-second, "closes a cycle")
}

// The store's graph lock (review 631 S-1), with both writers registered
// before, so nothing else serializes them: the first link holds the lock
// (held at the store's hook point), the second waits for it, and once the
// first commits the second is refused with the cycle it would close.
//
// Serial: SetHook
func TestDebtGraphLockInTheStore(t *testing.T) {
	dw := newDebtWorld(t, true)
	D, E := dw.mustFile(t, "stg", "d"), dw.mustFile(t, "stg", "e")
	pool := dw.db.Pool(t, dw.db.Runtime)
	w1, w2 := wr("thread:half-1"), wr("thread:half-2")
	for _, w := range []store.Writer{w1, w2} {
		_, err := store.FileDebt(ctx, pool, "stg", w, "register", debtFiling("registers "+w.Actor))
		ok(t, err)
	}
	held, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	let := func() { once.Do(func() { close(release) }) }
	defer store.SetHook(func(point, arg string) {
		if point == "debt_graph_locked" && arg == D {
			close(held)
			<-release
		}
	})()
	defer let() // a failure below still lets the first link finish
	first, second := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := store.LinkDebts(ctx, pool, w1, "half", store.DebtLink{Kind: "blocks", From: D, To: E, Needs: "closed"})
		first <- err
	}()
	<-held
	go func() {
		_, err := store.LinkDebts(ctx, pool, w2, "half", store.DebtLink{Kind: "part_of", From: E, To: D})
		second <- err
	}()
	dw.waitingOnGraphLock(t)
	let()
	ok(t, <-first)
	if err := <-second; !errors.Is(err, store.ErrCycle) {
		t.Fatalf("the second half: %v, want ErrCycle", err)
	}
}

// The parts' own lock (review 631b R2-S2): while another session holds a
// debt's part lock (742133, hashtext(debt)), a writer of that debt's part
// order waits for it, past its event stream's lock (another session holding
// the part lock alone takes none of the stream), and goes on once it is let
// go.
func TestDebtPartOrderWaitsForItsLock(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	f := debtFiling("two parts")
	f.Repos = []string{"repo-a", "repo-b"}
	out, err := store.FileDebt(ctx, dw.rt, "stg", dw.crew, dw.key(), f)
	ok(t, err)
	D := out["debt_id"].(string)
	var p1, p2 int64
	ok(t, dw.rt.QueryRow(ctx, `SELECT min(debt_part_id), max(debt_part_id) FROM agentrt.debt_part WHERE debt_id = $1`, D).Scan(&p1, &p2))
	holder, err := dw.db.Connect(t, dw.db.Runtime).Begin(ctx)
	ok(t, err)
	_, err = holder.Exec(ctx, `SELECT pg_advisory_xact_lock(742133, hashtext($1))`, D)
	ok(t, err)
	done, writer := make(chan error, 1), dw.db.Connect(t, dw.db.Runtime)
	go func() {
		done <- store.InTx(ctx, writer, func(tx pgx.Tx) error {
			if _, err := store.Append(ctx, tx, de(D, "debt_part_set", dw.crew.Actor, map[string]any{"debt_part_order_ids": []any{900801}})); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO agentrt.debt_part_order (debt_part_order_id, debt_id, debt_part_id, after_debt_part_id)
			  OVERRIDING SYSTEM VALUE VALUES (900801, '%s', %d, %d)`, D, p2, p1))
			return err
		})
	}()
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		var n int
		ok(t, dw.rt.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted AND classid = 742133
		  AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&n))
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			_ = holder.Rollback(ctx)
			t.Fatal("the part order writer does not wait for the part lock")
		}
	}
	select {
	case err := <-done:
		t.Fatalf("the part order was written while the lock was held: %v", err)
	default:
	}
	ok(t, holder.Commit(ctx))
	ok(t, <-done)
}

// Two halves of a cycle of one debt's parts written at once (review 631c
// R3-S3): the second waits (for the debt's event stream, which the first
// holds, and then for the parts' lock), sees the first's half once it
// commits, and is refused; exactly one half is kept.
func TestDebtPartOrderHalvesAtOnce(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	f := debtFiling("two parts")
	f.Repos = []string{"repo-a", "repo-b"}
	out, err := store.FileDebt(ctx, dw.rt, "stg", dw.crew, dw.key(), f)
	ok(t, err)
	D := out["debt_id"].(string)
	var p1, p2 int64
	ok(t, dw.rt.QueryRow(ctx, `SELECT min(debt_part_id), max(debt_part_id) FROM agentrt.debt_part WHERE debt_id = $1`, D).Scan(&p1, &p2))
	half := func(conn *pgx.Conn, id, part, after int64) (pgx.Tx, error) {
		// No t.Fatal here: the second half runs on its own goroutine.
		tx, berr := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if berr != nil {
			return nil, berr
		}
		if _, berr = store.Append(ctx, tx, de(D, "debt_part_set", dw.crew.Actor, map[string]any{"debt_part_order_ids": []any{id}})); berr != nil {
			return tx, berr
		}
		_, berr = tx.Exec(ctx, fmt.Sprintf(`INSERT INTO agentrt.debt_part_order (debt_part_order_id, debt_id, debt_part_id, after_debt_part_id)
		  OVERRIDING SYSTEM VALUE VALUES (%d, '%s', %d, %d)`, id, D, part, after))
		return tx, berr
	}
	first, err := half(dw.db.Connect(t, dw.db.Runtime), 900901, p2, p1)
	ok(t, err)
	second, conn2 := make(chan error, 1), dw.db.Connect(t, dw.db.Runtime)
	go func() {
		tx, err := half(conn2, 900902, p1, p2)
		if err == nil {
			err = tx.Commit(ctx)
		}
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
		second <- err
	}()
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		var n int
		ok(t, dw.rt.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&n))
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the second half does not wait for the first")
		}
	}
	ok(t, first.Commit(ctx))
	wantErr(t, <-second, "closes a cycle of the parts")
	var kept int
	ok(t, dw.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.debt_part_order WHERE debt_id = $1`, D).Scan(&kept))
	if kept != 1 {
		t.Fatalf("%d halves kept, want 1", kept)
	}
}
