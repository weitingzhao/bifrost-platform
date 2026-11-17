package eventlog

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// The reads other packages ask of the ledger. The table is this package's:
// a package that names agentrt.event in SQL of its own has to follow every
// change of the table, and on 2026-11-16 fourteen packages did (the review
// in shared notes brain/foundation-code-quality-review-2026-11-16.md). These
// are the questions that were asked the same way in several places; a
// projection that joins the ledger to its own tables still writes its own
// SQL, and codeguard counts those lines.

// Row is an event as the ledger holds it.
type Row struct {
	ID     int64
	Seq    int64
	Stream string
	Type   string
	Actor  string
	At     time.Time
	Body   map[string]any
}

// Last is the last event of a stream; false when the stream has none.
func Last(ctx context.Context, tx pgx.Tx, stream string) (Row, bool, error) {
	r := Row{Stream: stream}
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT event_id, seq, event_type, actor_id, at, body_json FROM agentrt.event
	  WHERE event_stream_id = $1 ORDER BY seq DESC LIMIT 1`, stream).Scan(&r.ID, &r.Seq, &r.Type, &r.Actor, &r.At, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Row{}, false, nil
	}
	if err != nil {
		return Row{}, false, err
	}
	if len(raw) > 0 {
		if err = json.Unmarshal(raw, &r.Body); err != nil {
			return Row{}, false, err
		}
	}
	return r, true, nil
}

// Has says whether a stream holds an event of the type (any type when it is
// empty) whose body has each of these top-level keys with this text: the
// text a key reads as, as body_json->>key gives it.
func Has(ctx context.Context, tx pgx.Tx, stream, typ string, text map[string]string) (bool, error) {
	if text == nil {
		text = map[string]string{} // a nil map would be JSON null, which is no object
	}
	want, err := json.Marshal(text)
	if err != nil {
		return false, err
	}
	var have bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event e WHERE e.event_stream_id = $1
	  AND ($2 = '' OR e.event_type = $2)
	  AND NOT EXISTS (SELECT 1 FROM jsonb_each_text($3::jsonb) w WHERE e.body_json->>w.key IS DISTINCT FROM w.value))`,
		stream, typ, string(want)).Scan(&have)
	return have, err
}

// HasBody says whether any event of the type, of the plan node when one is
// named, has a body that contains doc (jsonb containment: every key of doc
// with an equal value).
func HasBody(ctx context.Context, tx pgx.Tx, typ, planNode string, doc map[string]any) (bool, error) {
	if doc == nil {
		doc = map[string]any{}
	}
	want, err := json.Marshal(doc)
	if err != nil {
		return false, err
	}
	var have bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_type = $1
	  AND ($2 = '' OR plan_node_id = $2) AND body_json @> $3::jsonb)`, typ, planNode, string(want)).Scan(&have)
	return have, err
}
