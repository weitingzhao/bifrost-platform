package coord_test

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Owner 2026-11-22: verify a persisted letter round trip, keeping inbox arrival,
// reading, ACK and the independent reply distinct. Uses only the disposable test DB.
func TestLetterRoundTripEvidencePG(t *testing.T) {
	t.Parallel()
	_, db, sender := conclusionDB(t)
	recipient := actor.Writer{Actor: "owner"}
	original, err := coord.SendMessage(ctx, db, sender, recipient.Actor, "ask", []byte("test request"), "")
	ok(t, err)
	letters, err := coord.Unread(ctx, db, recipient, 100)
	ok(t, err)
	if len(letters) != 1 || letters[0].ID != original || letters[0].AckedAt != nil {
		t.Fatalf("unread: %+v", letters)
	}
	done, err := coord.AckMessages(ctx, db, recipient, []string{original})
	ok(t, err)
	if len(done) != 1 {
		t.Fatalf("ACK: %v", done)
	}
	body, err := json.Marshal(map[string]any{"v": 1, "type": "note", "summary": "test reply", "reply_to": original, "text": "private test response"})
	ok(t, err)
	reply, err := coord.SendMessage(ctx, db, recipient, sender.Actor, "note", body, "")
	ok(t, err)
	ok(t, eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		var arrivals, acks int
		if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE event_type='seat.mail.arrived'), count(*) FILTER (WHERE event_type='message_acked') FROM agentrt.event WHERE body_json->>'message_id'=$1`, original).Scan(&arrivals, &acks); err != nil {
			return err
		}
		if arrivals != 1 || acks != 1 {
			t.Errorf("arrival=%d ACK=%d", arrivals, acks)
		}
		var ref string
		var disclosed bool
		if err := tx.QueryRow(ctx, `SELECT body_json->>'reply_to', body_json ? 'text' OR body_json ? 'summary' FROM agentrt.event WHERE event_type='message_posted' AND body_json->>'message_id'=$1`, reply).Scan(&ref, &disclosed); err != nil {
			return err
		}
		if ref != original || disclosed {
			t.Errorf("reply=%q disclosed=%v", ref, disclosed)
		}
		return nil
	}))
	replies, err := coord.Unread(ctx, db, sender, 100)
	ok(t, err)
	if len(replies) != 1 || replies[0].ID != reply || replies[0].AckedAt != nil {
		t.Fatalf("reply inbox: %+v", replies)
	}
}
