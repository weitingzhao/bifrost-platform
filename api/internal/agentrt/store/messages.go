package store

import (
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
	ID        string    `json:"message_id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Kind      string    `json:"kind"`
	MissionID string    `json:"mission_id,omitempty"`
	TaskID    string    `json:"task_id,omitempty"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
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
	case len(body) == 0 || len(body) > MaxMessageBody || !utf8.Valid(body):
		return "", invalid("a message's body is UTF-8 text of 1 to %d bytes", MaxMessageBody)
	case kindOf(to) == "":
		return "", invalid("%q is not an actor name", to)
	case to == w.Actor:
		return "", invalid("a message goes to another actor")
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

// Unread is an actor's messages not yet acknowledged, oldest first; PostgreSQL
// is the record, whatever the mailbox stream in Redis holds.
func Unread(ctx context.Context, db DB, actor string, limit int) ([]Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var out []Message
	err := InTx(ctx, db, func(tx pgx.Tx) error {
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
