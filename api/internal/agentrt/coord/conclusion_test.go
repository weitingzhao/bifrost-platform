package coord_test

import (
	"context"
	"errors"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

var ctx = context.Background()

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// The seed (the actors a message names) and nothing else: no mission, plan
// or artifact of the store's shared fixture.
func seedOnly(ctx context.Context, owner *pgx.Conn) error {
	seeded, err := store.Seed(ctx, owner)
	if err == nil && !seeded {
		err = errors.New("seed did nothing on an empty database")
	}
	return err
}

func conclusionDB(t *testing.T) (*agentrttest.DB, store.DB, store.Writer) {
	t.Helper()
	db := agentrttest.MigratedWith(t, "seeded", seedOnly)
	pool := db.Pool(t, db.Runtime)
	w := store.Writer{Actor: "thread:local_conclusion", Token: "fixture"}
	_, _, err := store.UploadArtifact(ctx, pool, w, "message", "text/plain", []byte("register the requesting thread"))
	ok(t, err)
	return db, pool, w
}

func TestConclusionConcurrentRestartAndAck(t *testing.T) {
	t.Parallel()
	db, first, w := conclusionDB(t)
	second := db.Pool(t, db.Runtime)
	var wg sync.WaitGroup
	errs := make(chan error, 24)
	for i := 0; i < 24; i++ {
		conn := first
		if i%2 == 0 {
			conn = second
		}
		wg.Add(1)
		safego.Go("test-conclusion-race", func() {
			defer wg.Done()
			errs <- coord.PostConclusion(ctx, conn, "approval:appr_test:executed", w.Actor, "Approval #1: executed")
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		ok(t, err)
	}
	mail, err := store.Unread(ctx, first, w, 100)
	ok(t, err)
	if len(mail) != 1 || mail[0].From != "controller" || mail[0].Kind != "note" {
		t.Fatalf("mail=%+v", mail)
	}
	_, err = store.AckMessages(ctx, first, w, []string{mail[0].ID})
	ok(t, err)
	// A fresh pool/process, changed body, and an acknowledged message still
	// find the permanent receipt. The state is the identity, not the body.
	ok(t, coord.PostConclusion(ctx, db.Pool(t, db.Runtime), "approval:appr_test:executed", w.Actor, "new summary"))
	mail, err = store.Unread(ctx, second, w, 100)
	ok(t, err)
	if len(mail) != 0 {
		t.Fatal("replay became unread again")
	}
	read := db.Connect(t, db.Reader)
	var messages, receipts int
	ok(t, read.QueryRow(ctx, `SELECT count(*) FROM agentrt.message WHERE to_actor_id=$1`, w.Actor).Scan(&messages))
	ok(t, read.QueryRow(ctx, `SELECT count(*) FROM agentrt.idempotency_key
	  WHERE route='thread-conclusion' AND expires_at='infinity' AND created_at<=now()`).Scan(&receipts))
	if messages != 1 || receipts != 1 {
		t.Fatalf("messages/receipts=%d/%d", messages, receipts)
	}
	// A second terminal state of the same approval is a different conclusion.
	ok(t, coord.PostConclusion(ctx, first, "approval:appr_test:unknown", w.Actor, "unknown"))
	mail, err = store.Unread(ctx, first, w, 100)
	ok(t, err)
	if len(mail) != 1 || mail[0].Body != "unknown" {
		t.Fatalf("second state=%+v", mail)
	}
}

type failConclusionReceipt struct{ store.DB }
type failConclusionReceiptTx struct{ pgx.Tx }

func (db failConclusionReceipt) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := db.DB.BeginTx(ctx, opts)
	return failConclusionReceiptTx{tx}, err
}

func (tx failConclusionReceiptTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "INSERT INTO agentrt.idempotency_key") {
		return pgconn.CommandTag{}, errors.New("receipt insert failed")
	}
	return tx.Tx.Exec(ctx, sql, args...)
}

func TestConclusionFailureRollsBackThenRetries(t *testing.T) {
	t.Parallel()
	db, conn, w := conclusionDB(t)
	if err := coord.PostConclusion(ctx, failConclusionReceipt{conn}, "question:q_test:answered", w.Actor, "answer"); err == nil {
		t.Fatal("injected receipt failure succeeded")
	}
	read := db.Connect(t, db.Reader)
	var count int
	ok(t, read.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.message)
	  + (SELECT count(*) FROM agentrt.idempotency_key) + (SELECT count(*) FROM agentrt.event WHERE event_type='message_posted')`).Scan(&count))
	if count != 0 {
		t.Fatalf("partial commit after failed receipt: %d", count)
	}
	ok(t, coord.PostConclusion(ctx, conn, "question:q_test:answered", w.Actor, "answer"))
	ok(t, coord.PostConclusion(ctx, conn, "question:q_test:answered", w.Actor, "answer"))
	mail, err := store.Unread(ctx, conn, w, 100)
	ok(t, err)
	if len(mail) != 1 {
		t.Fatalf("retry mail=%+v", mail)
	}
}

func TestConclusionSkipsUndeliverableActors(t *testing.T) {
	t.Parallel()
	db, conn, w := conclusionDB(t)
	for _, to := range []string{"", "owner", "seat:chief_of_staff", "thread:missing", "not a thread"} {
		ok(t, coord.PostConclusion(ctx, conn, "approval:skipped:failed", to, "failed"))
	}
	// The fixture records the actor change with its event, just as the real
	// schema requires; delivery itself must never revive the recipient.
	admin := db.Connect(t, db.Admin)
	retire := func(retired bool) {
		t.Helper()
		ok(t, store.InTx(ctx, admin, func(tx pgx.Tx) error {
			if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "system", Type: "actor_registered", Actor: "controller",
				Body: map[string]any{"actors": []string{w.Actor}, "note": "test recipient availability"}}); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE agentrt.actor SET retired_at=CASE WHEN $2 THEN now() ELSE NULL END WHERE actor_id=$1`, w.Actor, retired)
			return err
		}))
	}
	retire(true)
	ok(t, coord.PostConclusion(ctx, conn, "approval:skipped:failed", w.Actor, "failed"))
	var count int
	ok(t, admin.QueryRow(ctx, `SELECT count(*) FROM agentrt.message`).Scan(&count))
	if count != 0 {
		t.Fatalf("undeliverable actors got %d messages", count)
	}
	retire(false)
	ok(t, coord.PostConclusion(ctx, conn, "approval:skipped:failed", w.Actor, "failed"))
	mail, err := store.Unread(ctx, conn, w, 100)
	ok(t, err)
	if len(mail) != 1 {
		t.Fatal("successful control did not deliver")
	}
}

// 2026-11-16, the tenant drill on STG: the bus dispatcher's wake
// (sub:<row>:<event>) was refused here, so no subscription row could wake a
// seat. It is posted once per key, as the approvals' and questions' are.
func TestConclusionTakesTheDispatchersWake(t *testing.T) {
	t.Parallel()
	_, pool, w := conclusionDB(t)
	for range 2 {
		ok(t, coord.PostConclusion(ctx, pool, "sub:drill:ev-65987", w.Actor, "bus: git.main.advanced"))
	}
	mail, err := store.Unread(ctx, pool, w, 100)
	ok(t, err)
	if len(mail) != 1 || mail[0].From != "controller" {
		t.Fatalf("mail=%+v, want one message from the controller", mail)
	}
	if err := coord.PostConclusion(ctx, pool, "other:x", w.Actor, "x"); err == nil {
		t.Fatal("a key of no known sender was taken")
	}
}

// 2026-11-17, the bus measured on PROD: of 2,482 events in 24h none named a
// cause, so a wake the dispatcher sent could not be traced to the event that
// sent it. A caused wake names its event on both letter events; a cause this
// ledger does not hold is dropped and the letter still goes.
func TestConclusionCarriesItsCause(t *testing.T) {
	t.Parallel()
	db, pool, w := conclusionDB(t)
	read := db.Connect(t, db.Reader)
	var origin int64
	ok(t, read.QueryRow(ctx, `SELECT max(event_id) FROM agentrt.event`).Scan(&origin))
	ok(t, coord.PostConclusionCaused(ctx, pool, "sub:cause:ev-1", w.Actor, "bus: release.deploy.finished", origin))
	ok(t, coord.PostConclusionCaused(ctx, pool, "sub:cause:ev-2", w.Actor, "bus: unknown", origin+1_000_000))
	rows, err := read.Query(ctx, `SELECT event_type, coalesce(cause_event_id, 0) FROM agentrt.event
	  WHERE event_id > $1 AND event_type IN ('message_posted', $2) ORDER BY event_id`, origin, coord.SeatMailType)
	ok(t, err)
	var got []int64
	for rows.Next() {
		var typ string
		var cause int64
		ok(t, rows.Scan(&typ, &cause))
		got = append(got, cause)
	}
	ok(t, rows.Err())
	if len(got) != 4 || got[0] != origin || got[1] != origin || got[2] != 0 || got[3] != 0 {
		t.Fatalf("causes=%v, want [%d %d 0 0]", got, origin, origin)
	}
	mail, err := store.Unread(ctx, pool, w, 100)
	ok(t, err)
	if len(mail) != 2 {
		t.Fatalf("mail=%d, want both letters delivered", len(mail))
	}
}
