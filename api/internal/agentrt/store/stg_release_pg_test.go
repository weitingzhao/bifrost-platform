package store

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
)

func stgPG(t *testing.T) (*agentrttest.DB, *pgx.Conn) {
	t.Helper()
	db := agentrttest.Migrated(t)
	if _, err := Seed(context.Background(), db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	return db, db.Connect(t, db.Runtime)
}
func stgPGAppend(conn *pgx.Conn, ev Event) (Appended, error) {
	var a Appended
	err := InTx(context.Background(), conn, func(tx pgx.Tx) error { var err error; a, err = Append(context.Background(), tx, ev); return err })
	return a, err
}
func stgPGRead(t *testing.T, conn *pgx.Conn) StgState {
	t.Helper()
	var s StgState
	err := InTx(context.Background(), conn, func(tx pgx.Tx) error { var err error; s, err = StgReleaseState(context.Background(), tx); return err })
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func stgPGReplay(t *testing.T, conn *pgx.Conn, entries []StgEntry) {
	t.Helper()
	for _, e := range entries {
		restore := FreezeEventClock(e.At)
		a, err := stgPGAppend(conn, e.Event)
		restore()
		if err != nil {
			t.Fatal(err)
		}
		if a.Seq != e.Seq {
			t.Fatalf("seq=%d want %d", a.Seq, e.Seq)
		}
	}
}

// Serial: FreezeEventClock controls the recorder's deadline and intent times.
func TestStgStoreAppendRejectsAndReadsBack(t *testing.T) {
	_, conn := stgPG(t)
	h := stgReady(t, stgTestMember())
	stgPGReplay(t, conn, h.entries)
	defer FreezeEventClock(h.at)()
	good := stgNote(h.start(1))
	for _, mutate := range []func(*Event){
		func(e *Event) { e.Actor = "owner" }, func(e *Event) { e.Stream = "landing:other" }, func(e *Event) { e.Type = "pipeline_seen" },
		func(e *Event) { e.Body["landing"] = map[string]any{} }, func(e *Event) { e.Body["stg_release"].(map[string]any)["try"] = 2 },
		func(e *Event) { e.Body["stg_release"].(map[string]any)["run"] = "wrong-run" },
	} {
		before := stgPGRead(t, conn)
		bad := stgNote(stgClone(good.Body["stg_release"].(map[string]any)))
		mutate(&bad)
		_, err := stgPGAppend(conn, bad)
		var typed *StgReleaseError
		if !errors.As(err, &typed) {
			t.Fatal("expected typed refusal", err)
		}
		if !reflect.DeepEqual(before, stgPGRead(t, conn)) {
			t.Fatal("rejected append changed stored state")
		}
	}
	a, err := stgPGAppend(conn, good)
	if err != nil {
		t.Fatal(err)
	}
	s := stgPGRead(t, conn)
	started := s.Batches[stgTestBatch].Tries[1].Started
	if started.Seq != a.Seq || started.Record.Int("intent_seq") != a.Seq || s.Budget[StgBudgetKey{stgSHA("a"), 1}] != 1 {
		t.Fatal("intent/debit not stored atomically")
	}
	if _, err := stgPGAppend(conn, good); err == nil {
		t.Fatal("second started accepted")
	}
	bound := stgNote(stgRecord("bound", "try", 1, "uid", "run-uid-1"))
	first, err := stgPGAppend(conn, bound)
	if err != nil {
		t.Fatal(err)
	}
	again, err := stgPGAppend(conn, bound)
	if err != nil || again != first {
		t.Fatal("bound idempotency did not return original row", first, again, err)
	}
	bound.Body["stg_release"].(map[string]any)["uid"] = "different"
	if _, err := stgPGAppend(conn, bound); err == nil {
		t.Fatal("UID collision accepted")
	}
	count, err := VerifyStream(context.Background(), conn, StgReleaseStream)
	if err != nil || count != first.Seq {
		t.Fatal("stored chain/seq changed", count, err)
	}
}

// Serial: FreezeEventClock; two separate connections compete for the last
// debit. The stream lock must make the loser read the winner's STARTED.
func TestStgStoreTwoConnectionsSpendLastCreditOnce(t *testing.T) {
	db, one := stgPG(t)
	two := db.Connect(t, db.Runtime)
	m := stgTestMember()
	h := stgReady(t, m)
	for n := int64(1); n <= 2; n++ {
		if n > 1 {
			due := h.state().Debits[stgSHA("a")].NotBefore
			h.add(stgRecord("retry", "try", n, "not_before", due.Format(time.RFC3339Nano)))
			h.at = due
			h.add(h.snapshot(n, m))
		}
		h.failed(n, "run_failed", "unchanged")
	}
	due := h.state().Debits[stgSHA("a")].NotBefore
	h.add(stgRecord("retry", "try", 3, "not_before", due.Format(time.RFC3339Nano)))
	h.at = due
	h.add(h.snapshot(3, m))
	stgPGReplay(t, one, h.entries)
	defer FreezeEventClock(h.at)()
	goNow := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, conn := range []*pgx.Conn{one, two} {
		wg.Add(1)
		ev := stgNote(h.start(3))
		go func() { defer wg.Done(); <-goNow; _, err := stgPGAppend(conn, ev); results <- err }()
	}
	close(goNow)
	wg.Wait()
	close(results)
	pass, refused := 0, 0
	for err := range results {
		if err == nil {
			pass++
		} else {
			var se *StgReleaseError
			if !errors.As(err, &se) || se.Code != "transition" {
				t.Fatal(err)
			}
			refused++
		}
	}
	if pass != 1 || refused != 1 {
		t.Fatalf("success=%d refusal=%d", pass, refused)
	}
	s := stgPGRead(t, one)
	if s.Budget[StgBudgetKey{stgSHA("a"), 1}] != 3 || s.LastSeq != int64(len(h.entries)+1) {
		t.Fatal("double debit or extra intent")
	}
}

// Serial: FreezeEventClock; a valid append rolled back by its enclosing
// transaction must not reserve a batch or spend a sequence.
func TestStgStoreRollbackAndLegacyWriteRefusals(t *testing.T) {
	_, conn := stgPG(t)
	h := newStgHarness(t)
	defer FreezeEventClock(h.at)()
	good := stgNote(h.opened(stgTestMember()))
	rollback := errors.New("rollback fixture")
	err := InTx(context.Background(), conn, func(tx pgx.Tx) error {
		if _, err := Append(context.Background(), tx, good); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) || len(stgPGRead(t, conn).Batches) != 0 {
		t.Fatal("transaction did not roll back", err)
	}
	if _, err := stgPGAppend(conn, good); err != nil {
		t.Fatal(err)
	}
	for _, r := range []map[string]any{
		stgLegacyRecord("old", "stg_pending", "R", []any{stgSHA("a")}),
		stgLegacyRecord("old", "stg_snapshot", "P", stgSHA("b"), "U", stgSHA("c"), "try", 1, "not_before", h.at.Format(time.RFC3339Nano)),
		stgLegacyRecord("old", "stg_started", "run", "old", "try", 1), stgLegacyRecord("old", "stg_result", "run", "old", "outcome", "failed"),
		stgLegacyRecord("old", "stg_verified", "P", stgSHA("b"), "U", stgSHA("c"), "digests", map[string]any{"platform-api": stgDig("a"), "platform-workers": stgDig("a"), "platform-console": stgDig("c")}),
		stgLegacyRecord("old", "stg_failed", "why", "ci_failed"),
	} {
		_, err := stgPGAppend(conn, Event{Stream: StgReleaseStream, Type: "note", Actor: "controller", Body: map[string]any{"text": "old write", "landing": r}})
		var se *StgReleaseError
		if !errors.As(err, &se) || se.Code != "legacy" {
			t.Fatal("legacy write was not refused at entry", err)
		}
	}
	if seq, err := VerifyStream(context.Background(), conn, StgReleaseStream); err != nil || seq != 1 {
		t.Fatal("refusal changed the stream", seq, err)
	}
}
