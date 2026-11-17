package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Ledger is what the relay and the gate read of the event log.
type Ledger interface {
	// After is the events of types committed since after, in commit order,
	// up to limit, and where to read on from (eventlog.EventsAfter's rule: an
	// event shows once every transaction older than its own has ended).
	After(ctx context.Context, after Cursor, limit int, types []string) ([]Row, Cursor, error)
	// Horizon is "now": a cursor before every event not yet readable and
	// after every one that is.
	Horizon(ctx context.Context) (Cursor, error)
	// Event is one event by its id.
	Event(ctx context.Context, eventID int64) (Row, bool, error)
}

// EventsAfterFunc is the event log's cross-stream read (eventlog.EventsAfter),
// handed in by the wiring: the rows as a JSON array of agentrt.event rows.
type EventsAfterFunc func(ctx context.Context, tx pgx.Tx, after Cursor, limit int, types []string) ([]byte, Cursor, error)

// TxBeginner is a pool or a connection.
type TxBeginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// PGLedger reads the ledger in PostgreSQL. DB returns nil while the database
// is not connected.
type PGLedger struct {
	DB          func() TxBeginner
	EventsAfter EventsAfterFunc
}

// ErrNotConnected: the ledger's database is not connected yet.
var ErrNotConnected = errors.New("bus: the ledger is not connected")

func (l *PGLedger) read(ctx context.Context, fn func(pgx.Tx) error) error {
	var db TxBeginner
	if l.DB != nil {
		db = l.DB()
	}
	if db == nil {
		return ErrNotConnected
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	return fn(tx)
}

func (l *PGLedger) After(ctx context.Context, after Cursor, limit int, types []string) ([]Row, Cursor, error) {
	var rows []Row
	var next Cursor
	err := l.read(ctx, func(tx pgx.Tx) error {
		doc, n, err := l.EventsAfter(ctx, tx, after, limit, types)
		if err != nil {
			return err
		}
		next = n
		rows, err = DecodeRows(doc)
		return err
	})
	return rows, next, err
}

func (l *PGLedger) Horizon(ctx context.Context) (Cursor, error) {
	var c Cursor
	err := l.read(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT txid_snapshot_xmin(txid_current_snapshot())`).Scan(&c.TxID)
	})
	return c, err
}

func (l *PGLedger) Event(ctx context.Context, eventID int64) (Row, bool, error) {
	var doc []byte
	err := l.read(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT to_jsonb(e) FROM agentrt.event e WHERE e.event_id = $1`, eventID).Scan(&doc)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Row{}, false, nil
	}
	if err != nil {
		return Row{}, false, err
	}
	var r rawRow
	if err := json.Unmarshal(doc, &r); err != nil {
		return Row{}, false, fmt.Errorf("bus: event %d: %w", eventID, err)
	}
	return r.row(), true, nil
}

// rawRow is an agentrt.event row as to_jsonb gives it.
type rawRow struct {
	EventID int64          `json:"event_id"`
	TxID    int64          `json:"txid"`
	Stream  string         `json:"event_stream_id"`
	Type    string         `json:"event_type"`
	Actor   string         `json:"actor_id"`
	Cause   *int64         `json:"cause_event_id"`
	Body    map[string]any `json:"body_json"`
	At      time.Time      `json:"at"`
}

func (r rawRow) row() Row {
	out := Row{EventID: r.EventID, TxID: r.TxID, Stream: r.Stream, Type: r.Type, Actor: r.Actor, Body: r.Body, At: r.At}
	if r.Cause != nil {
		out.Cause = *r.Cause
	}
	return out
}

// DecodeRows reads a JSON array of agentrt.event rows.
func DecodeRows(doc []byte) ([]Row, error) {
	var raw []rawRow
	if err := json.Unmarshal(doc, &raw); err != nil {
		return nil, fmt.Errorf("bus: ledger rows: %w", err)
	}
	out := make([]Row, 0, len(raw))
	for _, r := range raw {
		out = append(out, r.row())
	}
	return out, nil
}
