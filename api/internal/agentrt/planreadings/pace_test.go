package planreadings

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"reflect"
	"testing"
	"time"
)

type paceRow struct {
	value any
	err   error
}

func (r paceRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	reflect.ValueOf(dest[0]).Elem().Set(reflect.ValueOf(r.value))
	return nil
}

type paceRows struct {
	pgx.Rows
	data            [][]any
	index           int
	closed          bool
	scanErr, endErr error
}

func (r *paceRows) Next() bool { return r.index < len(r.data) }
func (r *paceRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	for i, v := range r.data[r.index] {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(v))
	}
	r.index++
	return nil
}
func (r *paceRows) Close()     { r.closed = true }
func (r *paceRows) Err() error { return r.endErr }

type paceTx struct {
	pgx.Tx
	t                      *testing.T
	rowCalls, queryCalls   int
	rowErrors, queryErrors [2]error
	events, spans          *paceRows
	root, stream           string
	start                  time.Time
}

func (tx *paceTx) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	i := tx.rowCalls
	tx.rowCalls++
	if len(args) != 1 || args[0] != tx.root {
		tx.t.Fatalf("lost root binding: %v", args)
	}
	var v any = "round"
	if i == 1 {
		v = 4
	}
	return paceRow{v, tx.rowErrors[i]}
}
func (tx *paceTx) Query(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
	i := tx.queryCalls
	tx.queryCalls++
	if i == 0 {
		if len(args) != 2 || args[0] != tx.stream || !args[1].(time.Time).Equal(tx.start) {
			tx.t.Fatalf("lost stream/day boundary: %v", args)
		}
		return tx.events, tx.queryErrors[i]
	}
	if !tx.events.closed {
		tx.t.Fatal("activity query began before events closed")
	}
	return tx.spans, tx.queryErrors[i]
}
func paceFixture(t *testing.T) (*paceTx, time.Time, *time.Location) {
	t.Helper()
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, loc)
	return &paceTx{t: t, root: "R2", stream: "plan:R2", start: time.Date(2026, 11, 9, 0, 0, 0, 0, loc), events: &paceRows{data: [][]any{{now.AddDate(0, 0, -1), "active", "done"}, {now.AddDate(0, 0, -1), "done", "active"}, {now.AddDate(0, 0, -2), "active", "done"}}}, spans: &paceRows{data: [][]any{{[]byte(`{"started_at":"2026-11-14T18:00:00Z","ended_at":"2026-11-15T18:00:00Z"}`)}}}}, now, loc
}
func TestPaceSnapshotAndLocalDays(t *testing.T) {
	t.Parallel()
	tx, now, loc := paceFixture(t)
	got, err := Pace(context.Background(), tx, tx.root, tx.stream, now, loc)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != "R2" || got.Remaining != 4 || got.Timezone != "America/Chicago" || len(got.Days) != 7 || got.Days[4].Done != 1 || got.Days[5].Done != 0 || !got.Days[6].Partial || got.Average != 0.3 || got.ETA.Days != 12 || got.QuietMinutes != 0 || len(got.Quiet) != 0 {
		t.Fatalf("changed pace: %+v", got)
	}
	if !tx.events.closed || !tx.spans.closed || tx.rowCalls != 2 || tx.queryCalls != 2 {
		t.Fatal("lost snapshot/row lifecycle")
	}
}
func TestPaceReadErrorsRemainVisible(t *testing.T) {
	t.Parallel()
	failure := errors.New("read failed")
	for _, stage := range []string{"missing root", "count", "events query", "events scan", "events iteration", "activity query", "activity iteration"} {
		t.Run(stage, func(t *testing.T) {
			tx, now, loc := paceFixture(t)
			want := failure
			switch stage {
			case "missing root":
				tx.rowErrors[0] = pgx.ErrNoRows
				want = pgx.ErrNoRows
			case "count":
				tx.rowErrors[1] = failure
			case "events query":
				tx.queryErrors[0] = failure
			case "events scan":
				tx.events.scanErr = failure
			case "events iteration":
				tx.events.endErr = failure
			case "activity query":
				tx.queryErrors[1] = failure
			case "activity iteration":
				tx.spans.endErr = failure
			}
			_, err := Pace(context.Background(), tx, tx.root, tx.stream, now, loc)
			if !errors.Is(err, want) {
				t.Fatalf("lost error: %v", err)
			}
			if (stage == "events scan" || stage == "events iteration") && !tx.events.closed {
				t.Fatal("event rows left open")
			}
		})
	}
}
