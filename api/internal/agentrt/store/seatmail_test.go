package store_test

import (
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// 2026-11-16: a seat that stops while it waits arms one doorbell on its own
// address. Every message posted to an inbox announces seat.mail.arrived in
// the same transaction, subject the receiver, change the message's kind.
func TestPostMessageAnnouncesSeatMail(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	to := "seat:chief_of_staff"
	var id string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		id, err = store.PostMessage(ctx, tx, "owner", to, "ask", []byte("status?"))
		return err
	}))
	var subject, change, msg string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'subject', body_json->>'change', body_json->>'message_id' FROM agentrt.event
	  WHERE event_type = 'seat.mail.arrived' AND event_stream_id = $1 ORDER BY event_id DESC LIMIT 1`, "mbox:"+to).Scan(&subject, &change, &msg))
	if subject != to || change != "ask" || msg != id {
		t.Fatalf("seat.mail.arrived subject=%q change=%q message=%q, want %q ask %q", subject, change, msg, to, id)
	}
}
