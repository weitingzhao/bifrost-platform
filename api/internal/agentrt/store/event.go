// Package store is the data access of the agent runtime. It opens no HTTP;
// the kernel (2.2) calls it. Every state change goes through Append, the one
// path that writes an event: the database refuses a state write without an
// event of the same transaction (migrations 0002).
package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// Event is what a caller asks to append. Empty key strings are NULL.
type Event struct {
	Stream    string
	Type      string
	Actor     string
	MissionID string
	PlanNode  string
	TaskID    string
	AttemptID string
	Cause     int64 // 0 = none
	Body      map[string]any
}

// Appended is the stored form, as the hash covers it.
type Appended struct {
	EventID int64
	Seq     int64
	SHA256  string
	At      time.Time
}

const timeLayout = "2006-01-02T15:04:05.000000Z"

// envelope is the object hashed for an event: the recorder's canonical JSON
// of the envelope fields and the body.
func envelope(ev Event, seq int64, prev string, at time.Time) map[string]any {
	opt := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	var cause any
	if ev.Cause != 0 {
		cause = ev.Cause
	}
	return map[string]any{
		"v":              1,
		"stream":         ev.Stream,
		"seq":            seq,
		"type":           ev.Type,
		"by":             ev.Actor,
		"at":             at.UTC().Format(timeLayout),
		"prev_sha256":    prev,
		"mission_id":     opt(ev.MissionID),
		"plan_node_id":   opt(ev.PlanNode),
		"task_id":        opt(ev.TaskID),
		"attempt_id":     opt(ev.AttemptID),
		"cause_event_id": cause,
		"body":           ev.Body,
	}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Append writes one event inside tx and names it as this transaction's event
// (SET LOCAL agentrt.event_id), so the state rows written next in tx pass the
// database's check. The stream is created on first use. Callers write their
// state rows after Append and their outbox rows with the returned id.
func Append(ctx context.Context, tx pgx.Tx, ev Event) (Appended, error) {
	if ev.Stream == "" || ev.Type == "" || ev.Actor == "" {
		return Appended{}, errors.New("store: an event needs a stream, a type and an actor")
	}
	if ev.Body == nil {
		ev.Body = map[string]any{}
	}
	if err := checkBody(ev.Type, ev.Body); err != nil {
		return Appended{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO agentrt.event_stream (event_stream_id) VALUES ($1) ON CONFLICT DO NOTHING`,
		ev.Stream); err != nil {
		return Appended{}, err
	}
	var lastSeq int64
	var lastSHA string
	if err := tx.QueryRow(ctx, `SELECT last_seq, last_sha256 FROM agentrt.event_stream WHERE event_stream_id = $1 FOR UPDATE`,
		ev.Stream).Scan(&lastSeq, &lastSHA); err != nil {
		return Appended{}, err
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	seq := lastSeq + 1
	sum, err := canon.Sum(envelope(ev, seq, lastSHA, at))
	if err != nil {
		return Appended{}, fmt.Errorf("store: %s body: %w", ev.Type, err)
	}
	body, err := canon.Encode(ev.Body)
	if err != nil {
		return Appended{}, err
	}
	var cause any
	if ev.Cause != 0 {
		cause = ev.Cause
	}
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO agentrt.event
	  (event_stream_id, seq, event_type, actor_id, mission_id, plan_node_id, task_id, attempt_id,
	   cause_event_id, body_json, prev_sha256, sha256, at)
	  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13) RETURNING event_id`,
		ev.Stream, seq, ev.Type, ev.Actor, nullable(ev.MissionID), nullable(ev.PlanNode), nullable(ev.TaskID),
		nullable(ev.AttemptID), cause, string(body), lastSHA, sum, at).Scan(&id); err != nil {
		return Appended{}, err
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('agentrt.event_id', $1, true)`, strconv.FormatInt(id, 10)); err != nil {
		return Appended{}, err
	}
	return Appended{EventID: id, Seq: seq, SHA256: sum, At: at}, nil
}

// ChainBreak names the first event of a stream whose hash or link is wrong.
type ChainBreak struct {
	Stream string
	Seq    int64
	Reason string
}

func (b *ChainBreak) Error() string {
	return fmt.Sprintf("store: stream %s breaks at seq %d: %s", b.Stream, b.Seq, b.Reason)
}

// VerifyStream recomputes every hash of a stream from the stored rows and
// checks each link, seq continuity and the head (T20, T23b).
func VerifyStream(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}, stream string) (int64, error) {
	rows, err := q.Query(ctx, `SELECT seq, event_type, actor_id, COALESCE(mission_id,''), COALESCE(plan_node_id,''),
	  COALESCE(task_id,''), COALESCE(attempt_id,''), COALESCE(cause_event_id,0), body_json::text, prev_sha256, sha256, at
	  FROM agentrt.event WHERE event_stream_id = $1 ORDER BY seq`, stream)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var want int64 = 1
	prev := ""
	for rows.Next() {
		var ev Event
		var seq int64
		var bodyText, prevSHA, sha string
		var at time.Time
		ev.Stream = stream
		if err := rows.Scan(&seq, &ev.Type, &ev.Actor, &ev.MissionID, &ev.PlanNode, &ev.TaskID, &ev.AttemptID,
			&ev.Cause, &bodyText, &prevSHA, &sha, &at); err != nil {
			return 0, err
		}
		if seq != want {
			return 0, &ChainBreak{stream, seq, fmt.Sprintf("expected seq %d", want)}
		}
		if prevSHA != prev {
			return 0, &ChainBreak{stream, seq, "prev_sha256 does not match the previous event"}
		}
		body, err := canon.Parse([]byte(bodyText))
		if err != nil {
			return 0, &ChainBreak{stream, seq, "body is not canonical JSON: " + err.Error()}
		}
		ev.Body, _ = body.(map[string]any)
		sum, err := canon.Sum(envelope(ev, seq, prevSHA, at))
		if err != nil {
			return 0, &ChainBreak{stream, seq, err.Error()}
		}
		if sum != sha {
			return 0, &ChainBreak{stream, seq, "sha256 does not match the stored event"}
		}
		prev = sha
		want++
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	var lastSeq int64
	var lastSHA string
	if err := q.QueryRow(ctx, `SELECT last_seq, last_sha256 FROM agentrt.event_stream WHERE event_stream_id = $1`,
		stream).Scan(&lastSeq, &lastSHA); err != nil {
		return 0, err
	}
	if lastSeq != want-1 || lastSHA != prev {
		return 0, &ChainBreak{stream, lastSeq, "stream head does not match its last event"}
	}
	return want - 1, nil
}
