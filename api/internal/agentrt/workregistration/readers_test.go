package workregistration

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"
)

type noteRows struct {
	pgx.Rows
	raw    []string
	at     time.Time
	next   int
	closed bool
}

func (r *noteRows) Next() bool { return r.next < len(r.raw) }
func (r *noteRows) Scan(dest ...any) error {
	*dest[0].(*[]byte) = []byte(r.raw[r.next])
	r.next++
	if len(dest) > 1 {
		*dest[1].(*time.Time) = r.at
	}
	return nil
}
func (r *noteRows) Close()     { r.closed = true }
func (r *noteRows) Err() error { return nil }

// activityRows gives notes as the work rows of ReadRootActivity.
type activityRows struct{ *noteRows }

func (r activityRows) Scan(dest ...any) error {
	*dest[5].(*[]byte) = []byte(r.raw[r.next])
	at := r.at
	*dest[6].(**time.Time) = &at
	order := int64(r.next + 1)
	*dest[7].(**int64) = &order
	r.next++
	return nil
}

type activityTx struct {
	pgx.Tx
	rows *noteRows
}

func (t *activityTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return activityRows{t.rows}, nil
}

func readWork(tx pgx.Tx, now time.Time, description func(context.Context, pgx.Tx, string) (any, error)) ([]map[string]any, error) {
	a, err := ReadRootActivity(context.Background(), tx, "root", now, RecentPage{Limit: 21}, description)
	return a.Work, err
}

type noteTx struct {
	pgx.Tx
	rows *noteRows
}

func (t *noteTx) Query(context.Context, string, ...any) (pgx.Rows, error) { return t.rows, nil }

// The extraction must preserve the caller's transaction, consume rows before
// reading descriptions, and retain the old freshness boundary and wire times.
func TestSnapshotBoundaryAndDescription(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 15, 20, 0, 0, 123456789, time.UTC)
	rows := &noteRows{at: now, raw: []string{
		`{"external_work_id":"one","artifact_id":"a","state":"Running","heartbeat_at":"2026-11-15T19:59:00.123456789Z","started_at":"2026-11-15T19:58:00.123456789Z"}`,
		`{"external_work_id":"two","artifact_id":"a","state":"Registered","heartbeat_at":"2026-11-15T19:59:00Z","source_started_at":"old"}`,
	}}
	tx := &activityTx{rows: rows}
	got, err := readWork(tx, now, func(_ context.Context, actual pgx.Tx, id string) (any, error) {
		if actual != tx || !rows.closed || id != "a" {
			t.Fatalf("description lost snapshot or open rows: %v %v %s", actual == tx, rows.closed, id)
		}
		return "validated", nil
	})
	if err != nil || len(got) != 2 {
		t.Fatalf("snapshot=%v err=%v", got, err)
	}
	if got[0]["status"] != "Running" || got[0]["elapsed_s"] != int64(120) || got[0]["recorded_at"] != "2026-11-15T20:00:00.123456Z" || got[0]["description"] != "validated" || got[1]["status"] != "Unconfirmed" || got[1]["backfilled"] != true {
		t.Fatalf("changed projection: %v", got)
	}
	failure := errors.New("description rejected")
	tx.rows = &noteRows{raw: []string{`{"artifact_id":"bad"}`}}
	_, err = readWork(tx, now, func(context.Context, pgx.Tx, string) (any, error) { return nil, failure })
	if !errors.Is(err, failure) {
		t.Fatalf("lost rejection: %v", err)
	}
}

func TestWorkSpansFallbackAndWholeRecord(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 11, 15, 19, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	tx := &noteTx{rows: &noteRows{raw: []string{
		`{"registered_at":"2026-11-15T19:00:00Z","heartbeat_at":"2026-11-15T20:00:00Z"}`,
		`{"started_at":"2026-11-15T17:00:00Z","ended_at":"2026-11-15T18:00:00Z"}`,
		`{"started_at":"invalid","registered_at":"invalid"}`,
	}}}
	got, err := WorkSpans(context.Background(), tx, from, to)
	if err != nil || len(got) != 1 || got[0] != [2]time.Time{from, to} {
		t.Fatalf("spans=%v err=%v", got, err)
	}
	tx.rows = &noteRows{raw: []string{`{"commit_shas":7}`}}
	if _, err = WorkSpans(context.Background(), tx, from, to); err == nil {
		t.Fatal("malformed non-projected field accepted")
	}
	if _, err = readWork(&activityTx{rows: &noteRows{raw: []string{`{"commit_shas":7}`}}}, to, nil); err == nil {
		t.Fatal("snapshot accepted malformed whole record")
	}
}
