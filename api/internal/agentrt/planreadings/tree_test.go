package planreadings

import (
	"context"
	"errors"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"reflect"
	"testing"
)

func treeText(s string) *string { return &s }
func TestProgressTreeKeepsEmptyNodesOrderAndStepFacts(t *testing.T) {
	t.Parallel()
	var absent *string
	rows := &paceRows{data: [][]any{
		{"architecture", "arch_root", "Architecture", treeText("row"), treeText("A"), treeText("Row"), absent, absent, ""},
		{"empty", "round", "Empty", absent, absent, absent, absent, absent, ""},
		{"round", "round", "Round", treeText("item"), treeText("I"), treeText("Item"), treeText("step2"), treeText(plan.StateActive), "done_rule_manual"},
		{"round", "round", "Round", treeText("item"), treeText("I"), treeText("Item"), treeText("step1"), treeText(plan.StateDone), "done_rule_manual"},
		{"round", "round", "Round", treeText("item"), treeText("I"), treeText("Item"), treeText("step3"), treeText(plan.StateDone), "done_rule"},
		{"round", "round", "Round", treeText("item-empty"), treeText("E"), treeText("Empty item"), absent, absent, ""},
	}}
	tx := &recentTx{rows: rows}
	got, err := ProgressTree(context.Background(), tx)
	want := []*TreeRoot{
		{ID: "architecture", Kind: "arch_root", Title: "Architecture", Items: []*TreeItem{{ID: "row", Label: "A", Title: "Row"}}},
		{ID: "empty", Kind: "round", Title: "Empty"},
		{ID: "round", Kind: "round", Title: "Round", Items: []*TreeItem{
			{ID: "item", Label: "I", Title: "Item", Steps: []plan.Step{{ID: "step2", State: plan.StateActive}, {ID: "step1", State: plan.StateDone, Manual: true}, {ID: "step3", State: plan.StateDone}}, Active: []string{"step2"}},
			{ID: "item-empty", Label: "E", Title: "Empty item"},
		}},
	}
	if err != nil || !reflect.DeepEqual(got, want) || !rows.closed {
		t.Fatalf("tree changed: %#v %v", got, err)
	}
	tx.rows = &paceRows{}
	got, err = ProgressTree(context.Background(), tx)
	if err != nil || got != nil {
		t.Fatalf("empty tree: %v %v", got, err)
	}
}
func TestProgressTreeReadFailuresCloseRows(t *testing.T) {
	t.Parallel()
	failure := errors.New("read failed")
	for _, stage := range []string{"query", "scan", "iteration"} {
		t.Run(stage, func(t *testing.T) {
			var absent *string
			rows := &paceRows{data: [][]any{{"root", "round", "Root", absent, absent, absent, absent, absent, ""}}}
			tx := &recentTx{rows: rows}
			switch stage {
			case "query":
				tx.err = failure
			case "scan":
				rows.scanErr = failure
			case "iteration":
				rows.endErr = failure
			}
			got, err := ProgressTree(context.Background(), tx)
			if !errors.Is(err, failure) || got != nil || (stage != "query" && !rows.closed) {
				t.Fatalf("partial success or leaked rows: %v %v", got, err)
			}
		})
	}
}
