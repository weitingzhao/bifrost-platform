package planreadings

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"reflect"
	"testing"
	"time"
)

func TestPendingChangesKeepScanAndVisitOrder(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 15, 23, 0, 0, 0, time.UTC)
	rows := &paceRows{data: [][]any{{int64(12), "plan:R2", "reason", now, "q1", "[]"}, {int64(13), "plan:R2", "later", now, "q2", "not-json"}}}
	tx := &recentTx{rows: rows}
	var seen []PendingChange
	err := PendingChanges(context.Background(), tx, now, 24*time.Hour, 20, func(p PendingChange) error { seen = append(seen, p); return nil })
	if err != nil || !rows.closed || len(seen) != 2 || seen[0].Event != 12 || seen[1].Question != "q2" || seen[1].Changes != "not-json" || !reflect.DeepEqual(tx.args, []any{now, float64(86400), 21}) {
		t.Fatalf("pending: %v %v %v", seen, err, tx.args)
	}
	// Decoding remains at the visitor. Its first failure wins over a later row error.
	failure := errors.New("change decoding failed")
	rows = &paceRows{data: [][]any{{int64(12), "plan:R2", "reason", now, "q1", "bad"}, {int64(13), "plan:R2", "later", now, "q2", "[]"}}, endErr: errors.New("later database failure")}
	tx.rows = rows
	err = PendingChanges(context.Background(), tx, now, time.Hour, 20, func(PendingChange) error { return failure })
	if !errors.Is(err, failure) || !rows.closed || rows.index != 1 {
		t.Fatalf("error precedence/close changed: %v %d", err, rows.index)
	}
}

func TestPendingReadErrorsRemainVisible(t *testing.T) {
	t.Parallel()
	failure := errors.New("read failed")
	now := time.Now()
	for _, stage := range []string{"query", "scan", "iteration"} {
		t.Run(stage, func(t *testing.T) {
			rows := &paceRows{data: [][]any{{int64(12), "plan:R2", "reason", now, "q1", "[]"}}}
			tx := &recentTx{rows: rows}
			switch stage {
			case "query":
				tx.err = failure
			case "scan":
				rows.scanErr = failure
			case "iteration":
				rows.endErr = failure
			}
			err := PendingChanges(context.Background(), tx, now, time.Hour, 1, func(PendingChange) error { return nil })
			if !errors.Is(err, failure) || (stage != "query" && !rows.closed) {
				t.Fatalf("lost failure or open rows: %v", err)
			}
		})
	}
}

type affectedTx struct {
	pgx.Tx
	row  pgx.Row
	args []any
}

func (tx *affectedTx) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	tx.args = args
	return tx.row
}
func TestAffectedMissionsRetainsCallerSelection(t *testing.T) {
	t.Parallel()
	nodes, bound := []string{"step", "parent"}, []string{"explicit"}
	tx := &affectedTx{row: paceRow{value: []string{"first", "second"}}}
	got, err := AffectedMissions(context.Background(), tx, nodes, bound)
	if err != nil || !reflect.DeepEqual(got, []string{"first", "second"}) || !reflect.DeepEqual(tx.args, []any{nodes, bound}) {
		t.Fatalf("affected: %v %v %v", got, err, tx.args)
	}
	failure := errors.New("query failed")
	tx.row = paceRow{err: failure}
	if _, err = AffectedMissions(context.Background(), tx, nil, nil); !errors.Is(err, failure) {
		t.Fatal(err)
	}
}
