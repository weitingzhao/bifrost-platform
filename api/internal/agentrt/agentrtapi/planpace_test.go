package agentrtapi_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// R5:5.5-13 slice 1: GET /plan-pace reads a round's steps left and the steps
// done on each of its last days in the Owner's time zone, with one sentence.
func TestPlanPaceRoute(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	var left int
	if err := pool.QueryRow(context.Background(), `WITH RECURSIVE tree AS (SELECT plan_node_id FROM agentrt.plan_node WHERE plan_node_id = 'R2'
	  UNION ALL SELECT n.plan_node_id FROM agentrt.plan_node n JOIN tree t ON n.parent_plan_node_id = t.plan_node_id)
	  SELECT count(*) FROM tree JOIN agentrt.plan_node n USING (plan_node_id) WHERE n.node_kind = 'step' AND n.state IN ('planned','active')`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left == 0 {
		t.Fatal("fixture: R2 has no open step")
	}
	code, out := call(t, s, "viewer", "GET", "plan-pace?root=R2", nil)
	if code != 200 || out["root"] != "R2" || out["timezone"] != "America/Chicago" || out["remaining"] != float64(left) {
		t.Fatalf("pace %d %v", code, out)
	}
	if days, _ := out["days"].([]any); len(days) != store.PaceDays {
		t.Fatalf("days %v", out["days"])
	}
	if sum, _ := out["summary"].(string); !strings.HasPrefix(sum, "R2: ") {
		t.Fatalf("summary %q", sum)
	}
	if q, _ := out["quiet_summary"].(string); q == "" || out["quiet"] == nil || out["quiet_minutes"] == nil {
		t.Fatalf("quiet fields: %v %v %v", out["quiet"], out["quiet_minutes"], out["quiet_summary"])
	}
	// A step done now counts on today's line.
	var step string
	if err := pool.QueryRow(context.Background(), `SELECT plan_node_id FROM agentrt.plan_node WHERE plan_node_id LIKE 'R2:%' AND node_kind = 'step'
	  AND state IN ('planned','active') ORDER BY plan_node_id LIMIT 1`).Scan(&step); err != nil {
		t.Fatal(err)
	}
	if err := store.InTx(context.Background(), pool, func(tx pgx.Tx) error {
		_, err := store.SetStepState(context.Background(), tx, "controller", "R2", step, "done", "done_rule")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	code, out = call(t, s, "viewer", "GET", "plan-pace?root=R2", nil)
	days, _ := out["days"].([]any)
	today, _ := days[len(days)-1].(map[string]any)
	if code != 200 || out["remaining"] != float64(left-1) || today["done"] != float64(1) || today["partial"] != true {
		t.Fatalf("after one done: %d remaining %v today %v", code, out["remaining"], today)
	}
	if code, _ = call(t, s, "viewer", "GET", "plan-pace?root=missing", nil); code != 404 {
		t.Fatalf("missing %d", code)
	}
	if code, _ = call(t, s, "viewer", "GET", "plan-pace", nil); code != 400 {
		t.Fatalf("no root %d", code)
	}
	if code, _ = call(t, s, "", "GET", "plan-pace?root=R2", nil); code != 401 {
		t.Fatalf("anonymous %d", code)
	}
}
