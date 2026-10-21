package controller_test

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Review 487 A1: the chief of staff's scheduled brief, ready and taken by no
// host, waits; once its wait passes its limit the controller's waits pass
// tells the Owner itself, in his conversation with the seat, once, for the
// seat that would tell him is what does not run.
func TestWaitsTellTheOwnerOfABriefNoHostTakes(t *testing.T) {
	pool := setup(t)
	loc, err := time.LoadLocation(store.BriefZone)
	must(t, err)
	slot := time.Date(2026, 10, 19, 8, 5, 0, 0, loc)
	c := &controller.Controller{DB: pool, Sources: (&world{}).sources(), Briefs: true, Now: func() time.Time { return slot }}
	if _, err = c.Accept(ctx); err != nil {
		t.Fatal(err)
	}
	var task, mission string
	must(t, pool.QueryRow(ctx, `SELECT task_id, mission_id FROM agentrt.task WHERE role = 'brief'`).Scan(&task, &mission))
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", mission, "active", "test") }))
	if _, err = store.Dispatch(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var state string
	must(t, pool.QueryRow(ctx, `SELECT state FROM agentrt.task WHERE task_id = $1`, task).Scan(&state))
	if state != "ready" {
		t.Fatalf("the brief is %s", state)
	}
	told := func() int {
		t.Helper()
		var n int
		must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.message m JOIN agentrt.artifact_blob b ON b.artifact_id = m.body_artifact_id
		  WHERE m.from_actor_id = $1 AND m.to_actor_id = 'owner' AND convert_from(b.body, 'UTF8') LIKE 'From the platform, not the chief of staff%'
		    AND convert_from(b.body, 'UTF8') LIKE '%' || $2 || '%'`, store.Seat, task).Scan(&n))
		return n
	}
	later := &controller.Controller{DB: pool, Now: func() time.Time { return time.Now().Add(31 * time.Minute) }}
	for pass := 0; pass < 2; pass++ {
		if _, err = later.Waits(ctx); err != nil {
			t.Fatal(err)
		}
		if n := told(); n != 1 {
			t.Fatalf("pass %d: the Owner was told %d times", pass, n)
		}
	}
	var body string
	must(t, pool.QueryRow(ctx, `SELECT convert_from(b.body, 'UTF8') FROM agentrt.message m JOIN agentrt.artifact_blob b ON b.artifact_id = m.body_artifact_id
	  WHERE m.from_actor_id = $1 AND m.to_actor_id = 'owner'`, store.Seat).Scan(&body))
	if !strings.Contains(body, "Scheduled brief: the morning brief") || !strings.Contains(body, "Why, as far as the platform can tell") {
		t.Fatalf("the note: %s", body)
	}
}
