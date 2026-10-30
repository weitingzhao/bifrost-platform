package store_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

func conclusionDB(t *testing.T) (*agentrttest.DB, store.DB, store.Writer) {
	t.Helper()
	db := agentrttest.Migrated(t)
	_, err := store.Seed(ctx, db.Connect(t, db.Owner))
	ok(t, err)
	pool := db.Pool(t, db.Runtime)
	w := store.Writer{Actor: "thread:local_conclusion", Token: "fixture"}
	_, _, err = store.UploadArtifact(ctx, pool, w, "message", "text/plain", []byte("register the requesting thread"))
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
			errs <- store.PostConclusion(ctx, conn, "approval:appr_test:executed", w.Actor, "Approval #1: executed")
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
	ok(t, store.PostConclusion(ctx, db.Pool(t, db.Runtime), "approval:appr_test:executed", w.Actor, "new summary"))
	mail, err = store.Unread(ctx, second, w, 100)
	ok(t, err)
	if len(mail) != 0 {
		t.Fatal("replay became unread again")
	}
	read := db.Connect(t, db.Reader)
	var messages, signals, receipts int
	ok(t, read.QueryRow(ctx, `SELECT count(*) FROM agentrt.message WHERE to_actor_id=$1`, w.Actor).Scan(&messages))
	ok(t, read.QueryRow(ctx, `SELECT count(*) FROM agentrt.outbox WHERE topic=$1`, store.MailboxTopic(w.Actor)).Scan(&signals))
	ok(t, read.QueryRow(ctx, `SELECT count(*) FROM agentrt.idempotency_key
	  WHERE route='thread-conclusion' AND expires_at='infinity' AND created_at<=now()`).Scan(&receipts))
	if messages != 1 || signals != 1 || receipts != 1 {
		t.Fatalf("messages/signals/receipts=%d/%d/%d", messages, signals, receipts)
	}
	// A second terminal state of the same approval is a different conclusion.
	ok(t, store.PostConclusion(ctx, first, "approval:appr_test:unknown", w.Actor, "unknown"))
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
	if err := store.PostConclusion(ctx, failConclusionReceipt{conn}, "question:q_test:answered", w.Actor, "answer"); err == nil {
		t.Fatal("injected receipt failure succeeded")
	}
	read := db.Connect(t, db.Reader)
	var count int
	ok(t, read.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.message) + (SELECT count(*) FROM agentrt.outbox)
	  + (SELECT count(*) FROM agentrt.idempotency_key) + (SELECT count(*) FROM agentrt.event WHERE event_type='message_posted')`).Scan(&count))
	if count != 0 {
		t.Fatalf("partial commit after failed receipt: %d", count)
	}
	ok(t, store.PostConclusion(ctx, conn, "question:q_test:answered", w.Actor, "answer"))
	ok(t, store.PostConclusion(ctx, conn, "question:q_test:answered", w.Actor, "answer"))
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
		ok(t, store.PostConclusion(ctx, conn, "approval:skipped:failed", to, "failed"))
	}
	// The fixture records the actor change with its event, just as the real
	// schema requires; delivery itself must never revive the recipient.
	admin := db.Connect(t, db.Admin)
	retire := func(retired bool) {
		t.Helper()
		ok(t, store.InTx(ctx, admin, func(tx pgx.Tx) error {
			if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "actor_registered", Actor: "controller",
				Body: map[string]any{"actors": []string{w.Actor}, "note": "test recipient availability"}}); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE agentrt.actor SET retired_at=CASE WHEN $2 THEN now() ELSE NULL END WHERE actor_id=$1`, w.Actor, retired)
			return err
		}))
	}
	retire(true)
	ok(t, store.PostConclusion(ctx, conn, "approval:skipped:failed", w.Actor, "failed"))
	var count int
	ok(t, admin.QueryRow(ctx, `SELECT count(*) FROM agentrt.message`).Scan(&count))
	if count != 0 {
		t.Fatalf("undeliverable actors got %d messages", count)
	}
	retire(false)
	ok(t, store.PostConclusion(ctx, conn, "approval:skipped:failed", w.Actor, "failed"))
	mail, err := store.Unread(ctx, conn, w, 100)
	ok(t, err)
	if len(mail) != 1 {
		t.Fatal("successful control did not deliver")
	}
}
