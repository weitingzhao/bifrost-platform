package planreadings

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestWaitingMissionsPreservesOrderAndBound(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	rows := &paceRows{data: [][]any{{"m1", "First", at, "q1", "reason", 0}, {"m2", "Second", at, "q1", "same card", 0}}}
	tx := &recentTx{rows: rows}
	got, err := WaitingMissions(context.Background(), tx, 1)
	want := []WaitingMission{{Mission: "m1", Title: "First", At: at, Question: "q1", Why: "reason"}, {Mission: "m2", Title: "Second", At: at, Question: "q1", Why: "same card"}}
	if err != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(tx.args, []any{2}) || !rows.closed {
		t.Fatalf("read/order/bound: %+v %v %v", got, err, tx.args)
	}
	// The read keeps duplicates and the extra row; the card projection owns both decisions.
	tx.rows = &paceRows{}
	got, err = WaitingMissions(context.Background(), tx, 1)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty: %v %v", got, err)
	}
}

func TestWaitingMissionFailuresCloseRows(t *testing.T) {
	t.Parallel()
	failure := errors.New("read failed")
	for _, stage := range []string{"query", "scan", "iteration"} {
		t.Run(stage, func(t *testing.T) {
			rows := &paceRows{data: [][]any{{"m1", "First", time.Now(), "q1", "reason", 0}}}
			tx := &recentTx{rows: rows}
			switch stage {
			case "query":
				tx.err = failure
			case "scan":
				rows.scanErr = failure
			case "iteration":
				rows.endErr = failure
			}
			_, err := WaitingMissions(context.Background(), tx, 1)
			if !errors.Is(err, failure) || (stage != "query" && !rows.closed) {
				t.Fatalf("failure/close: %v", err)
			}
		})
	}
}
