package coord

import (
	"bytes"
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	writer "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/localpath"
)

// maxMessageBody is the largest message body: a question, an answer or a
// handover note, not a document (that is an artifact).
const maxMessageBody = 8 << 10

// sendKinds are the kinds a writer may send; "wake" is the runtime's own.
var sendKinds = map[string]bool{"ask": true, "answer": true, "help": true, "handover": true, "note": true}

// Message is a message as its receiver reads it; the body is data, never
// instructions.
type Message struct {
	ID        string     `json:"message_id"`
	From      string     `json:"from"`
	To        string     `json:"to"`
	Kind      string     `json:"kind"`
	MissionID string     `json:"mission_id,omitempty"`
	TaskID    string     `json:"task_id,omitempty"`
	Body      string     `json:"body"`
	CreatedAt time.Time  `json:"created_at"`
	AckedAt   *time.Time `json:"acked_at,omitempty"`
}

// SendMessage records a writer's message to another actor (POST /messages,
// W-31 2.3): the writer is registered as for proposals (a thread on its first
// write); the receiver must be a registered actor, not retired; the kind one
// of sendKinds; the body UTF-8 text of at most maxMessageBody bytes. The body
// is stored as a message artifact, the event and the mailbox signal go in the
// same transaction (PostMessage).
func SendMessage(ctx context.Context, db eventlog.DB, w writer.Writer, to, kind string, body []byte) (string, error) {
	switch {
	case !sendKinds[kind]:
		return "", eventlog.Invalid("a message's kind is ask, answer, help, handover or note, not %q", kind)
	case len(body) == 0 || len(body) > maxMessageBody || !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0:
		return "", eventlog.Invalid("a message's body is UTF-8 text of 1 to %d bytes, without NUL", maxMessageBody)
	case writer.KindOf(to) == "":
		return "", eventlog.Invalid("%q is not an actor name", to)
	case to == w.Actor:
		return "", eventlog.Invalid("a message goes to another actor")
	}
	// A message to the chief of staff becomes a brief's spec (TakeMail), so
	// it is held to a spec's rule (T11).
	if ref, found := localpath.WorkspaceRef(body); found {
		return "", eventlog.Invalid("a message refers to a task's workspace: %s", ref)
	}
	var id string
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		if err := writer.Ensure(ctx, tx, w, true); err != nil {
			return err
		}
		if _, err := writer.ActorKind(ctx, tx, to); errors.Is(err, pgx.ErrNoRows) {
			return eventlog.Invalid("%s is not a registered actor", to)
		} else if err != nil {
			return err
		}
		var err error
		id, err = PostMessage(ctx, tx, w.Actor, to, kind, body)
		return err
	})
	return id, err
}

// Unread is the reader's messages not yet acknowledged, oldest first;
// PostgreSQL is the record, whatever the mailbox stream in Redis holds. A
// thread reads only with the token its session is held to (TD-370), under
// its writer's lock (writer.HoldsSessionTx).
func Unread(ctx context.Context, db eventlog.DB, w writer.Writer, limit int) ([]Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	actor := w.Actor
	var out []Message
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		out = nil
		if err := writer.HoldsSessionTx(ctx, tx, w); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT m.message_id, m.from_actor_id, m.to_actor_id, m.message_kind,
		    COALESCE(m.mission_id, ''), COALESCE(m.task_id, ''), b.body, m.created_at
		  FROM agentrt.message m JOIN agentrt.artifact_blob b ON b.artifact_id = m.body_artifact_id
		  WHERE m.to_actor_id = $1 AND m.acked_at IS NULL
		  ORDER BY m.created_at, m.message_id LIMIT $2`, actor, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m Message
			var body []byte
			if err := rows.Scan(&m.ID, &m.From, &m.To, &m.Kind, &m.MissionID, &m.TaskID, &body, &m.CreatedAt); err != nil {
				return err
			}
			m.Body = string(body)
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

// AckMessages records that the writer handled its messages: only the
// receiver acknowledges a message (AckMessage itself does not check who
// asks); acknowledging one twice changes nothing. It returns the ids it
// acknowledged now.
func AckMessages(ctx context.Context, db eventlog.DB, w writer.Writer, ids []string) ([]string, error) {
	if len(ids) == 0 || len(ids) > 100 {
		return nil, eventlog.Invalid("acknowledge 1 to 100 messages at a time")
	}
	var done []string
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		done = nil
		// The receiver's lock first: two batches of one inbox locked their
		// message rows and the mailbox stream in different orders and could
		// deadlock (review 264 B3).
		if err := writer.Lock(ctx, tx, w.Actor); err != nil {
			return err
		}
		if err := writer.HoldsSessionTx(ctx, tx, w); err != nil {
			return err
		}
		for _, id := range ids {
			var to string
			var acked bool
			err := tx.QueryRow(ctx, `SELECT to_actor_id, acked_at IS NOT NULL FROM agentrt.message WHERE message_id = $1 FOR UPDATE`, id).Scan(&to, &acked)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				return eventlog.Invalid("no message %s", id)
			case err != nil:
				return err
			case to != w.Actor:
				return eventlog.Invalid("message %s is not %s's to acknowledge", id, w.Actor)
			case acked:
				continue
			}
			if err := AckMessage(ctx, tx, w.Actor, id); err != nil {
				return err
			}
			done = append(done, id)
		}
		return nil
	})
	return done, err
}

// Thread is the conversation between actor and with, both directions, read
// or not, oldest first: the newest limit of it (1 to 100, 50 by default).
func Thread(ctx context.Context, db eventlog.DB, actor, with string, limit int) ([]Message, error) {
	if writer.KindOf(with) == "" || with == actor {
		return nil, eventlog.Invalid("%q is not another actor", with)
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var out []Message
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT * FROM (SELECT m.message_id, m.from_actor_id, m.to_actor_id, m.message_kind,
		    COALESCE(m.mission_id, ''), COALESCE(m.task_id, ''), b.body, m.created_at, m.acked_at
		  FROM agentrt.message m JOIN agentrt.artifact_blob b ON b.artifact_id = m.body_artifact_id
		  WHERE (m.from_actor_id = $1 AND m.to_actor_id = $2) OR (m.from_actor_id = $2 AND m.to_actor_id = $1)
		  ORDER BY m.created_at DESC, m.message_id DESC LIMIT $3) x ORDER BY 8, 1`, actor, with, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m Message
			var body []byte
			if err := rows.Scan(&m.ID, &m.From, &m.To, &m.Kind, &m.MissionID, &m.TaskID, &body, &m.CreatedAt, &m.AckedAt); err != nil {
				return err
			}
			m.Body = string(body)
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

// PostMessage stores a message (its body as an artifact, data only) and
// signals the receiver's mailbox.
func PostMessage(ctx context.Context, tx pgx.Tx, from, to, kind string, body []byte) (string, error) {
	return postMessage(ctx, tx, from, to, kind, body, "", "")
}

// postMessage is PostMessage for a message about a mission's task (the chief
// of staff's answer to the mail its brief took): the event names them, as the
// state guard requires of a row that does.
func postMessage(ctx context.Context, tx pgx.Tx, from, to, kind string, body []byte, mission, task string) (string, error) {
	return postMessageAs(ctx, tx, from, to, kind, body, mission, task, true, 0)
}

// postMessageAs is postMessage; checkClaims false is for a note that repeats
// policy alerts just recorded (the seat's to the Owner, alerts.go), which is
// not recorded a second time as a text that claims an approval.
//
// cause is the ledger event this message answers (0: none): a wake the bus's
// dispatcher sends for an event names it, on message_posted and on
// seat.mail.arrived, so the loop event → woken seat can be followed. An event
// this ledger does not hold is left out; the message is still sent.
func postMessageAs(ctx context.Context, tx pgx.Tx, from, to, kind string, body []byte, mission, task string, checkClaims bool, cause int64) (string, error) {
	if cause != 0 {
		ok, err := eventlog.Exists(ctx, tx, cause)
		if err != nil {
			return "", err
		}
		if !ok {
			cause = 0
		}
	}
	art, err := putArtifact(ctx, tx, from, "message", "text/plain; charset=utf-8", body)
	if err != nil {
		return "", err
	}
	id := NewMessageID()
	ev, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "mbox:" + to, Type: "message_posted", Actor: from, MissionID: mission, TaskID: task,
		Body: map[string]any{"message_id": id, "to": to, "kind": kind}, Cause: cause})
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO agentrt.message (message_id, from_actor_id, to_actor_id, message_kind, body_artifact_id, event_id,
	    mission_id, task_id)
	  VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''))`, id, from, to, kind, art, ev.EventID, mission, task); err != nil {
		return "", err
	}
	// The bus learns of it in the same transaction (seat.mail.arrived): a seat
	// waits for its own mail with one doorbell, whoever sent it (2026-11-16).
	if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "mbox:" + to, Type: SeatMailType, Actor: from, MissionID: mission, TaskID: task,
		Body: SeatMailBody(to, id, kind), Cause: cause}); err != nil {
		return "", err
	}
	if to == StatusBoard && kind == "note" {
		if err := announceSeatStatus(ctx, tx, from, id, body); err != nil {
			return "", err
		}
	}
	// A message that says the Owner approved is recorded where it came in;
	// the receiver reads it as data, as every message (T04).
	if checkClaims {
		if err := claimCheck(ctx, tx, eventlog.Event{Stream: "mbox:" + to, MissionID: mission, TaskID: task, Body: map[string]any{"message_id": id}},
			ev.EventID, "message", art, body); err != nil {
			return "", err
		}
	}
	return id, nil
}

// AckMessage records that the receiver handled a message. The ack lives in
// PostgreSQL only; Redis is never the record of it.
func AckMessage(ctx context.Context, tx pgx.Tx, actor, messageID string) error {
	// The event names the mission and task the message is about, if any (a
	// brief's answer): the state guard refuses a write it does not name
	// (review 269 A1).
	var to, mission, task string
	if err := tx.QueryRow(ctx, `SELECT to_actor_id, COALESCE(mission_id, ''), COALESCE(task_id, '') FROM agentrt.message
	  WHERE message_id = $1 FOR UPDATE`, messageID).Scan(&to, &mission, &task); err != nil {
		return err
	}
	if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "mbox:" + to, Type: "message_acked", Actor: actor, MissionID: mission, TaskID: task,
		Body: map[string]any{"message_id": messageID}}); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE agentrt.message SET acked_at = clock_timestamp() WHERE message_id = $1 AND acked_at IS NULL`, messageID)
	return err
}
