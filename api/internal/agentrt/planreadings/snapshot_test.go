package planreadings

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestSnapshotReadsPreserveValuesAndErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rows := &paceRows{data: [][]any{{[]byte(`{"plan_node_id":"n2","ordinal":2,"metadata":{"value":null}}`)}, {[]byte(`{"plan_node_id":"n1","ordinal":1}`)}}}
	tx := &recentTx{rows: rows}
	nodes, err := SnapshotNodes(ctx, tx, "root")
	if err != nil || len(nodes) != 2 || nodes[0]["plan_node_id"] != "n2" || nodes[1]["ordinal"] != float64(1) || !rows.closed || !reflect.DeepEqual(tx.args, []any{"root"}) {
		t.Fatalf("nodes: %v %v", nodes, err)
	}
	rows = &paceRows{data: [][]any{{"stream-a", int64(7)}, {"stream-b", int64(11)}}}
	tx.rows = rows
	marks, err := SourceWatermarks(ctx, tx)
	if err != nil || !rows.closed || !reflect.DeepEqual(marks, map[string]int64{"stream-a": 7, "stream-b": 11}) {
		t.Fatalf("watermarks: %v %v", marks, err)
	}
	failure := errors.New("read failed")
	for _, kind := range []string{"nodes", "watermarks"} {
		for _, stage := range []string{"query", "scan", "iteration"} {
			t.Run(kind+"/"+stage, func(t *testing.T) {
				rows := &paceRows{data: [][]any{{[]byte(`{}`)}}}
				if kind == "watermarks" {
					rows.data = [][]any{{"stream", int64(1)}}
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
				if kind == "nodes" {
					v, err := SnapshotNodes(ctx, tx, "root")
					if !errors.Is(err, failure) || v != nil {
						t.Fatalf("%v %v", v, err)
					}
				} else {
					v, err := SourceWatermarks(ctx, tx)
					if !errors.Is(err, failure) || v != nil {
						t.Fatalf("%v %v", v, err)
					}
				}
				if stage != "query" && !rows.closed {
					t.Fatal("rows left open")
				}
			})
		}
	}
	rows = &paceRows{data: [][]any{{[]byte(`{`)}}}
	tx.rows = rows
	if _, err := SnapshotNodes(ctx, tx, "root"); err == nil || !rows.closed {
		t.Fatal("invalid JSON was accepted or rows left open")
	}
}
