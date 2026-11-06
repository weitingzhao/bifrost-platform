package agentrtapi_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// GET /check-value reads the check value table: the weeks asked for and the
// current one, every row in the order of the check chain, a number the
// records cannot give null with its reason.
func TestCheckValueRoute(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	// An answer the Owner gave on a card: a run of the Owner's cards this
	// week, whose catch the records cannot tell without the recommendation.
	if _, err := store.SeeQuestion(ctx, pool, "q_check_value_1", func(context.Context) (store.Question, bool) {
		return store.Question{ID: "q_check_value_1", Status: store.QuestionAnswered, Option: "a", AnsweredBy: "owner",
			AnsweredAt: time.Now().UTC().Format(time.RFC3339)}, true
	}); err != nil {
		t.Fatal(err)
	}
	code, out := call(t, s, "viewer", "GET", "check-value", nil)
	weeks, _ := out["weeks"].([]any)
	rows, _ := out["rows"].([]any)
	if code != 200 || len(weeks) != store.CheckValueWeeksDefault+1 || len(rows) != 10 || out["as_of"] == nil {
		t.Fatalf("check value %d %v", code, out)
	}
	y, w := time.Now().UTC().ISOWeek()
	if current := weeks[len(weeks)-1]; current != fmt.Sprintf("%04d-W%02d", y, w) {
		t.Fatalf("the current week is %v", current)
	}
	var cards map[string]any
	for i, r := range rows {
		row := r.(map[string]any)
		if row["order"] != float64(i+1) || len(row["cells"].([]any)) != len(weeks) {
			t.Fatalf("row %d: %v", i, row)
		}
		if row["key"] == "owner_cards" {
			cards = row
		}
	}
	cells := cards["cells"].([]any)
	now := cells[len(cells)-1].(map[string]any)
	unknown, _ := now["unknown"].(map[string]any)
	if now["ran"] != float64(1) || now["caught"] != nil || unknown["caught"] != "recorded from R5:5.7" || now["owner_minutes"] != float64(2) {
		t.Fatalf("the Owner's cards this week: %v", now)
	}
	if code, out = call(t, s, "viewer", "GET", "check-value?weeks=3", nil); code != 200 || len(out["weeks"].([]any)) != 4 {
		t.Fatalf("weeks=3: %d %v", code, out["weeks"])
	}
	for _, q := range []string{"0", "13", "x"} {
		if code, _ = call(t, s, "viewer", "GET", "check-value?weeks="+q, nil); code != 400 {
			t.Fatalf("weeks=%s: %d", q, code)
		}
	}
	if code, _ = call(t, s, "viewer", "GET", "check-value?weeks=12", nil); code != 200 {
		t.Fatalf("weeks=12: %d", code)
	}
}
