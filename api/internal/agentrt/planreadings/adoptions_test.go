package planreadings

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestAdoptedViewsPreserveStoredFieldsAndLastRoot(t *testing.T) {
	t.Parallel()
	rows := &paceRows{data: [][]any{
		{[]byte(`{"v":1,"kind":"agentrt.plan_view/v1","root":"r1","tenant":"dev","plan_version":3,"artifact_id":"a1","nodes":{"i":"n"},"decided_event_id":17}`)},
		{[]byte(`{"v":1,"root":"r2","plan_version":2}`)},
		{[]byte(`{"v":1,"root":"r1","plan_version":4,"nodes":{"j":"m"}}`)},
	}}
	tx := &recentTx{rows: rows}
	got, err := AdoptedViews(context.Background(), tx)
	want := map[string]Adoption{"r1": {V: 1, Root: "r1", Version: 4, Nodes: map[string]string{"j": "m"}}, "r2": {V: 1, Root: "r2", Version: 2}}
	if err != nil || !reflect.DeepEqual(got, want) || !rows.closed {
		t.Fatalf("adoptions: %+v %v", got, err)
	}
	tx.rows = &paceRows{data: [][]any{{[]byte(`{"v":1,"kind":"agentrt.plan_view/v1","root":"r1","tenant":"dev","plan_version":3,"artifact_id":"a1","nodes":{"i":"n"},"decided_event_id":17}`)}}}
	got, err = AdoptedViews(context.Background(), tx)
	if err != nil || got["r1"].Decision != 17 || got["r1"].Artifact != "a1" || got["r1"].Tenant != "dev" || got["r1"].Kind != "agentrt.plan_view/v1" {
		t.Fatalf("lost stored fields: %+v %v", got, err)
	}
	tx.rows = &paceRows{}
	got, err = AdoptedViews(context.Background(), tx)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty: %v %v", got, err)
	}
}
func TestAdoptionReadErrorsAndDecodePrecedence(t *testing.T) {
	t.Parallel()
	failure := errors.New("database failed")
	for _, stage := range []string{"query", "scan", "iteration", "decode"} {
		t.Run(stage, func(t *testing.T) {
			rows := &paceRows{data: [][]any{{[]byte(`{"root":"r1"}`)}}}
			tx := &recentTx{rows: rows}
			switch stage {
			case "query":
				tx.err = failure
			case "scan":
				rows.scanErr = failure
			case "iteration":
				rows.endErr = failure
			case "decode":
				rows.data[0][0] = []byte(`{`)
				rows.endErr = failure
			}
			got, err := AdoptedViews(context.Background(), tx)
			if err == nil || (stage != "query" && !rows.closed) {
				t.Fatalf("error/close: %v", err)
			}
			if stage == "decode" {
				if errors.Is(err, failure) || got != nil {
					t.Fatal("later iteration error replaced decode failure")
				}
			} else if !errors.Is(err, failure) {
				t.Fatal(err)
			}
			if stage == "iteration" && len(got) != 1 {
				t.Fatal("partial result with iteration error changed")
			}
		})
	}
}
