package planreadings

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
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

// Origin: 2026-11-18, the plan view's slowdown after b6c6abc2: the first page and its stream bounds became one read (RecentFirstPage), whose rows of both kinds must reach their places with the raw JSON untouched.
func TestRecentStreamBoundsAndRawEvents(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"event_id":73,"body_json":{"unchanged":[1,null,"text"]}}`)
	stream := func(s string, seq int64) []any { return []any{&s, &seq, json.RawMessage(nil), (*int64)(nil)} }
	id := int64(73)
	rows := &paceRows{data: [][]any{{(*string)(nil), (*int64)(nil), raw, &id}, stream("plan:R2", 8), stream("external:one", 4)}}
	tx := &recentTx{rows: rows}
	bounds, first, err := RecentFirstPage(context.Background(), tx, "plan:R2", "R2")
	if err != nil || !rows.closed || !reflect.DeepEqual(bounds, map[string]int64{"plan:R2": 8, "external:one": 4}) || !reflect.DeepEqual(tx.args, []any{"plan:R2", "R2"}) {
		t.Fatalf("stream bounds: %v %v %v", bounds, err, tx.args)
	}
	if len(first) != 1 || first[0].ID != 73 || string(first[0].Raw) != string(raw) {
		t.Fatalf("the first page: %v", first)
	}
	rows = &paceRows{data: [][]any{{raw, int64(73)}}}
	tx.rows = rows
	encoded := []byte(`{"external:one":4,"plan:R2":8}`)
	events, err := RecentEvents(context.Background(), tx, "plan:R2", "R2", encoded, 80)
	if err != nil || !rows.closed || len(events) != 1 || events[0].ID != 73 || string(events[0].Raw) != string(raw) || !reflect.DeepEqual(tx.args, []any{"plan:R2", "R2", encoded, int64(80)}) {
		t.Fatalf("events: %v %v %v", events, err, tx.args)
	}
	tx.rows = &paceRows{}
	empty, none, err := RecentFirstPage(context.Background(), tx, "plan:empty", "empty")
	if err != nil || empty == nil || len(empty) != 0 || none == nil || len(none) != 0 {
		t.Fatalf("empty bounds: %v %v %v", empty, none, err)
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
					s, seq := "plan:R2", int64(1)
					rows.data = [][]any{{&s, &seq, json.RawMessage(nil), (*int64)(nil)}}
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
					_, _, err = RecentFirstPage(context.Background(), tx, "plan:R2", "R2")
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

// Origin: 2026-11-18, TD-582: Recent changes was one query over the plan's events and its work's, and is now two reads merged here; the page must stay what the one query gave: newest first, an event in both reads once, at most RecentLimit, and each stream at its highest sequence in either read.
func TestRecentReadsMergeAsOneQuery(t *testing.T) {
	t.Parallel()
	event := func(id int64) RecentEvent {
		return RecentEvent{Raw: json.RawMessage(strconv.FormatInt(id, 10)), ID: id}
	}
	ids := func(events []RecentEvent) []int64 {
		out := []int64{}
		for _, e := range events {
			out = append(out, e.ID)
		}
		return out
	}
	got := MergeRecent([]RecentEvent{event(9), event(5), event(2)}, []RecentEvent{event(7), event(5), event(1)})
	if want := []int64{9, 7, 5, 2, 1}; !reflect.DeepEqual(ids(got), want) {
		t.Fatalf("merged %v; want %v", ids(got), want)
	}
	if got := MergeRecent(nil, nil); got == nil || len(got) != 0 {
		t.Fatalf("nothing merged: %#v", got)
	}
	var own, work []RecentEvent
	for i := int64(RecentLimit * 2); i > 0; i-- {
		if i%2 == 0 {
			own = append(own, event(i))
		} else {
			work = append(work, event(i))
		}
	}
	page := MergeRecent(own, work)
	if len(page) != RecentLimit || page[0].ID != RecentLimit*2 || page[RecentLimit-1].ID != RecentLimit+1 {
		t.Fatalf("a full page: %v", ids(page))
	}
	streams := MergeRecentStreams(map[string]int64{"plan:R2": 8, "both": 3}, map[string]int64{"both": 5, "external:one": 4})
	if want := map[string]int64{"plan:R2": 8, "both": 5, "external:one": 4}; !reflect.DeepEqual(streams, want) {
		t.Fatalf("streams %v; want %v", streams, want)
	}
	if streams := MergeRecentStreams(map[string]int64{"both": 5}, map[string]int64{"both": 3}); streams["both"] != 5 {
		t.Fatalf("the lower sequence won: %v", streams)
	}
}
