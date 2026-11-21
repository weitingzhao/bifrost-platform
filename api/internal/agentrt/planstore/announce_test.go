package planstore_test

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// Origin: 2026-11-19, plan.tree.changed (the Console read /agentrt/plan 30,831
// times in 24 hours while the plan last changed on 11-12). Every transaction
// that writes a plan's stream announces the change exactly once, and the
// state rows still name the plan's own event, not the announcement.
func TestEveryPlanWriteIsAnnouncedOncePerTransaction(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.activate(t, s1)

	// Each transaction is told by xmin: one that wrote the plan's stream has
	// one announcement of that root, and no announcement stands alone.
	var writes, announced, twice, alone int
	ok(t, w.rt.QueryRow(ctx, `
	  WITH tx AS (
	    SELECT xmin::text AS x,
	      count(*) FILTER (WHERE event_stream_id = $1) AS plan,
	      count(*) FILTER (WHERE event_stream_id = $2 AND event_type = $3) AS told
	    FROM agentrt.event WHERE event_stream_id IN ($1, $2) GROUP BY 1)
	  SELECT count(*) FILTER (WHERE plan > 0), count(*) FILTER (WHERE plan > 0 AND told = 1),
	    count(*) FILTER (WHERE told > 1), count(*) FILTER (WHERE plan = 0)
	  FROM tx`, planstore.PlanStream(root), planstore.ChangedStream(root), planstore.TypeChanged).Scan(&writes, &announced, &twice, &alone))
	if writes == 0 || announced != writes || twice != 0 || alone != 0 {
		t.Fatalf("transactions writing the plan %d, announced once %d, announced twice %d, announcements alone %d", writes, announced, twice, alone)
	}

	var typ, ref string
	ok(t, w.rt.QueryRow(ctx, `SELECT e.event_type FROM agentrt.plan_node n JOIN agentrt.event e ON e.event_id = n.state_event_id
	  WHERE n.plan_node_id = $1`, s1).Scan(&typ))
	if typ != "plan_node_state_changed" {
		t.Fatalf("the step's state names a %s event; want its own plan_node_state_changed", typ)
	}
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'ref' FROM agentrt.event WHERE event_stream_id = $1 ORDER BY seq DESC LIMIT 1`,
		planstore.ChangedStream(root)).Scan(&ref))
	if ref != "/api/v1/agentrt/plan?root="+root {
		t.Fatalf("announcement ref %q", ref)
	}
}
