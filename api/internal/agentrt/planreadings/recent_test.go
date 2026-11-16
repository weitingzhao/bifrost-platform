package planreadings

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
)

type recentTx struct {
	pgx.Tx
	rows pgx.Rows
	err  error
	args []any
}

func (tx *recentTx) Query(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
	tx.args = args
	return tx.rows, tx.err
}

func TestRecentStreamBoundsAndRawEvents(t *testing.T) {
	t.Parallel()
	rows := &paceRows{data: [][]any{{"plan:R2", int64(8)}, {"external:one", int64(4)}}}
	tx := &recentTx{rows: rows}
	bounds, err := RecentStreams(context.Background(), tx, "plan:R2", "R2")
	if err != nil || !rows.closed || !reflect.DeepEqual(bounds, map[string]int64{"plan:R2": 8, "external:one": 4}) || !reflect.DeepEqual(tx.args, []any{"plan:R2", "R2"}) {
		t.Fatalf("stream bounds: %v %v %v", bounds, err, tx.args)
	}
	raw := json.RawMessage(`{"event_id":73,"body_json":{"unchanged":[1,null,"text"]}}`)
	rows = &paceRows{data: [][]any{{raw, int64(73)}}}
	tx.rows = rows
	encoded := []byte(`{"external:one":4,"plan:R2":8}`)
	events, err := RecentEvents(context.Background(), tx, "plan:R2", "R2", encoded, 80)
	if err != nil || !rows.closed || len(events) != 1 || events[0].ID != 73 || string(events[0].Raw) != string(raw) || !reflect.DeepEqual(tx.args, []any{"plan:R2", "R2", encoded, int64(80)}) {
		t.Fatalf("events: %v %v %v", events, err, tx.args)
	}
	tx.rows = &paceRows{}
	empty, err := RecentStreams(context.Background(), tx, "plan:empty", "empty")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty bounds: %v %v", empty, err)
	}
}

func TestRecentReadErrorsCloseRows(t *testing.T) {
	t.Parallel()
	failure := errors.New("database read failed")
	for _, events := range []bool{false, true} {
		for _, stage := range []string{"query", "scan", "iteration"} {
			t.Run(stage+map[bool]string{false: " streams", true: " events"}[events], func(t *testing.T) {
				rows := &paceRows{}
				if events {
					rows.data = [][]any{{json.RawMessage(`{}`), int64(1)}}
				} else {
					rows.data = [][]any{{"plan:R2", int64(1)}}
				}
				tx := &recentTx{rows: rows}
				switch stage {
				case "query":
					tx.err = failure
				case "scan":
					rows.scanErr = failure
				case "iteration":
					rows.endErr = failure
				}
				var err error
				if events {
					_, err = RecentEvents(context.Background(), tx, "plan:R2", "R2", []byte(`{}`), 0)
				} else {
					_, err = RecentStreams(context.Background(), tx, "plan:R2", "R2")
				}
				if !errors.Is(err, failure) {
					t.Fatalf("lost read error: %v", err)
				}
				if stage != "query" && !rows.closed {
					t.Fatal("rows were left open")
				}
			})
		}
	}
}
