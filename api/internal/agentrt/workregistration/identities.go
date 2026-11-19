package workregistration

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Identity is one external work's latest record as a session's
// binding reads it (R5:5.5-12 §3.1): a collector's work, with the host and
// session identity it was registered with (a record naming an identity but
// no host has Host "").
type Identity struct {
	ID, Root, Node, State, Host, Vendor, Session string
	Epoch                                        int64
}

// latestWorkNotes is each external work's latest record, read through the
// stream's own index. Sorting every note of every stream instead wrote a 9 MB
// temporary file on each 30-second pass of the thread watch (2026-11-19).
const latestWorkNotes = `SELECT body FROM (SELECT e.body_json->'external_work' AS body FROM agentrt.event_stream s
	  CROSS JOIN LATERAL (SELECT body_json FROM agentrt.event WHERE event_stream_id = s.event_stream_id
	    AND event_type IN ('external_work_noted','note') AND body_json ? 'external_work' ORDER BY seq DESC LIMIT 1) e
	  WHERE s.event_stream_id LIKE 'external-work:%') x`

// Identities lists every external work whose record names a session
// identity, its latest note each: the plan view's query shape (one read-only
// snapshot, the latest note of each stream).
func Identities(ctx context.Context, db eventlog.DB) ([]Identity, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	rows, err := tx.Query(ctx, latestWorkNotes+` WHERE body ? 'vendor' ORDER BY body->>'external_work_id'`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Identity, error) {
		var raw []byte
		if err := r.Scan(&raw); err != nil {
			return Identity{}, err
		}
		return identityFromJSON(raw)
	})
}

func identityFromJSON(raw []byte) (Identity, error) {
	var n Note
	if err := json.Unmarshal(raw, &n); err != nil {
		return Identity{}, err
	}
	return Identity{ID: n.ID, Root: n.Root, Node: n.Node, State: n.State, Host: n.Host, Vendor: n.Vendor, Session: n.Session, Epoch: n.Epoch}, nil
}
