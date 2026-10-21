package store_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The Console's fixture of when the Owner is next needed (console/src/api/__fixtures__/agentrt-progress-owner-next.testdb.json):
// GET /progress from the rows below, written when CONSOLE_OWNER_NEXT_FIXTURE_WRITE=1 (only when every assertion held).
const consoleOwnerNextFixture = "../../../../console/src/api/__fixtures__/agentrt-progress-owner-next.testdb.json"

// When the Owner is next needed, from real rows (W-31 2.5 gap 1; v4 §20.2,
// §20.5 scenario 8): implementations of class go have a usual time (three
// accepted, 100 s); M-1's next step is the Owner's, with two implementations
// queued before it (200 to 300 s); M-2's next step is the Owner's too, with
// one implementation (100 to 150 s) that waits on one of M-1's, so M-2 is
// due at the later time, M-1's. owner_next names M-1, and lists M-2 after it
// with M-1 as what it waits on.
func TestProgressSaysWhenTheOwnerIsNextNeeded(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	for range 3 {
		w.ended(t, "done")
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-2", "active", "test") }))
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.CreatePlanNode(ctx, tx, "controller", store.PlanNode{ID: "R2:2.1-6", Parent: item, Kind: "step", Label: "2.1-6",
			Title: "(you) approve M-2's release", MissionID: "M-2", StepKind: "agent", Ordinal: 5, Root: root})
		return err
	}))
	ok(t, planChange(w, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET owner_step = true WHERE plan_node_id IN ($1, 'R2:2.1-6')`, s1)
		return err
	}))
	first := w.ready(t, store.Task{Effect: "none", OffPlanReason: "work before the Owner's step"})
	w.ready(t, store.Task{Effect: "none", OffPlanReason: "work before the Owner's step"})
	waits := w.ready(t, store.Task{MissionID: "M-2", Effect: "none", OffPlanReason: "M-2's work, after M-1's"})
	w.legacy(t, `INSERT INTO agentrt.task_dependency (task_id, depends_on_task_id) VALUES ('`+waits+`', '`+first+`')`)

	before := time.Now()
	out, err := store.Progress(ctx, w.rt)
	ok(t, err)
	raw, err := json.MarshalIndent(out, "", "  ")
	ok(t, err)
	var doc struct {
		OwnerNext struct {
			State     string `json:"state"`
			Reason    string `json:"reason"`
			MissionID string `json:"mission_id"`
			Step      struct {
				PlanNodeID string `json:"plan_node_id"`
				Title      string `json:"title"`
			} `json:"step"`
			Earliest   string   `json:"earliest"`
			Latest     string   `json:"latest"`
			After      []string `json:"after"`
			Candidates []struct {
				MissionID string   `json:"mission_id"`
				State     string   `json:"state"`
				Earliest  string   `json:"earliest"`
				Latest    string   `json:"latest"`
				After     []string `json:"after"`
				Reason    string   `json:"reason"`
			} `json:"candidates"`
		} `json:"owner_next"`
	}
	ok(t, json.Unmarshal(raw, &doc))
	next := doc.OwnerNext
	if next.State != "estimated" || next.MissionID != "M-1" || next.Step.PlanNodeID != s1 || len(next.After) != 0 ||
		next.Reason != "M-1's next step is yours once its unfinished work is done; work not proposed yet is not counted" {
		t.Fatalf("owner_next = %+v; want M-1 at %s", next, s1)
	}
	for field, d := range map[string]time.Duration{next.Earliest: 200 * time.Second, next.Latest: 300 * time.Second} {
		at, err := time.Parse(time.RFC3339, field)
		ok(t, err)
		if off := at.Sub(before.Add(d)); off < -2*time.Second || off > 5*time.Second {
			t.Errorf("%s is %s from now; want about %s", field, at.Sub(before), d)
		}
	}
	if len(next.Candidates) != 2 {
		t.Fatalf("candidates = %+v; want M-1 and M-2", next.Candidates)
	}
	m2 := next.Candidates[1]
	if m2.MissionID != "M-2" || m2.State != "estimated" || !reflect.DeepEqual(m2.After, []string{"M-1"}) ||
		m2.Earliest != next.Earliest || m2.Latest != next.Latest || m2.Reason != "M-2's next step is yours once its unfinished work is done; work not proposed yet is not counted, after M-1" {
		t.Fatalf("M-2 = %+v; want it due when M-1 is, after M-1", m2)
	}
	if os.Getenv("CONSOLE_OWNER_NEXT_FIXTURE_WRITE") == "1" && !t.Failed() {
		ok(t, os.WriteFile(consoleOwnerNextFixture, append(raw, '\n'), 0o644))
	}
}
