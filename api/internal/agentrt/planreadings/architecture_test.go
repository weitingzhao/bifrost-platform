package planreadings

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestArchitectureReadsKeepRawRulesAndNoteTimes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ids := []string{"arch:a", "arch:b"}
	rows := &paceRows{data: [][]any{{"arch:a", "not JSON"}, {"arch:b", `{"manual":true}`}}}
	tx := &recentTx{rows: rows}
	rules, err := ArchitectureRules(ctx, tx, ids)
	if err != nil || !rows.closed || !reflect.DeepEqual(rules, map[string]string{"arch:a": "not JSON", "arch:b": `{"manual":true}`}) || !reflect.DeepEqual(tx.args, []any{ids}) {
		t.Fatalf("rules: %v %v", rules, err)
	}
	at := time.Date(2026, 11, 16, 1, 2, 3, 456, time.FixedZone("CST", -6*3600))
	rows = &paceRows{data: [][]any{{"arch:a", "note text", at, "reporter"}}}
	tx.rows = rows
	notes, err := ArchitectureNotes(ctx, tx, ids)
	want := map[string]map[string]any{"arch:a": {"text": "note text", "at": "2026-11-16T07:02:03.000000456Z", "by": "reporter"}}
	if err != nil || !rows.closed || !reflect.DeepEqual(notes, want) {
		t.Fatalf("notes: %v %v", notes, err)
	}
	tx.rows = &paceRows{}
	notes, err = ArchitectureNotes(ctx, tx, nil)
	if err != nil || notes == nil || len(notes) != 0 {
		t.Fatalf("empty notes: %v %v", notes, err)
	}
}

func TestArchitectureReadFailuresCloseRows(t *testing.T) {
	t.Parallel()
	failure := errors.New("read failed")
	for _, kind := range []string{"rules", "notes"} {
		for _, stage := range []string{"query", "scan", "iteration"} {
			t.Run(kind+"/"+stage, func(t *testing.T) {
				rows := &paceRows{data: [][]any{{"arch:a", "{}"}}}
				if kind == "notes" {
					rows.data = [][]any{{"arch:a", "text", time.Now(), "reporter"}}
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
				if kind == "rules" {
					v, err := ArchitectureRules(context.Background(), tx, []string{"arch:a"})
					if !errors.Is(err, failure) || v != nil {
						t.Fatalf("%v %v", v, err)
					}
				} else {
					v, err := ArchitectureNotes(context.Background(), tx, []string{"arch:a"})
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
}
