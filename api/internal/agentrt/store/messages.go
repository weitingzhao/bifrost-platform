package store

import (
	"bytes"
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// MaxMessageBody is the largest message body: a question, an answer or a
// handover note, not a document (that is an artifact).
const MaxMessageBody = 8 << 10

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
// of sendKinds; the body UTF-8 text of at most MaxMessageBody bytes. The body
// is stored as a message artifact, the event and the mailbox signal go in the
// same transaction (PostMessage).
func SendMessage(ctx context.Context, db DB, w Writer, to, kind string, body []byte) (string, error) {
	switch {
	case !sendKinds[kind]:
		return "", invalid("a message's kind is ask, answer, help, handover or note, not %q", kind)
	case len(body) == 0 || len(body) > MaxMessageBody || !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0:
		return "", invalid("a message's body is UTF-8 text of 1 to %d bytes, without NUL", MaxMessageBody)
	case kindOf(to) == "":
		return "", invalid("%q is not an actor name", to)
	case to == w.Actor:
		return "", invalid("a message goes to another actor")
	}
	// A message to the chief of staff becomes a brief's spec (TakeMail), so
	// it is held to a spec's rule (T11).
	if ref, found := WorkspaceRef("message", body); found {
		return "", invalid("a message refers to a task's workspace: %s", ref)
	}
	var id string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		if err := ensureWriter(ctx, tx, w, true); err != nil {
			return err
		}
		if _, err := ActorKind(ctx, tx, to); errors.Is(err, pgx.ErrNoRows) {
			return invalid("%s is not a registered actor", to)
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
// its writer's lock (holdsSessionTx).
func Unread(ctx context.Context, db DB, w Writer, limit int) ([]Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	actor := w.Actor
	var out []Message
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		out = nil
		if err := holdsSessionTx(ctx, tx, w); err != nil {
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
func AckMessages(ctx context.Context, db DB, w Writer, ids []string) ([]string, error) {
	if len(ids) == 0 || len(ids) > 100 {
		return nil, invalid("acknowledge 1 to 100 messages at a time")
	}
	var done []string
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		done = nil
		// The receiver's lock first: two batches of one inbox locked their
		// message rows and the mailbox stream in different orders and could
		// deadlock (review 264 B3).
		if err := writerLock(ctx, tx, w.Actor); err != nil {
			return err
		}
		if err := holdsSessionTx(ctx, tx, w); err != nil {
			return err
		}
		for _, id := range ids {
			var to string
			var acked bool
			err := tx.QueryRow(ctx, `SELECT to_actor_id, acked_at IS NOT NULL FROM agentrt.message WHERE message_id = $1 FOR UPDATE`, id).Scan(&to, &acked)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				return invalid("no message %s", id)
			case err != nil:
				return err
			case to != w.Actor:
				return invalid("message %s is not %s's to acknowledge", id, w.Actor)
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
func Thread(ctx context.Context, db DB, actor, with string, limit int) ([]Message, error) {
	if kindOf(with) == "" || with == actor {
		return nil, invalid("%q is not another actor", with)
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var out []Message
	err := InTx(ctx, db, func(tx pgx.Tx) error {
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
