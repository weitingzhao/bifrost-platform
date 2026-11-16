package store

import (
	"testing"
	"time"
)

// Origin: domain extraction 2026-11-15. The storage adapter must preserve task-to-mission
// dependencies and the existing unknown result when a required mission is absent.
func TestOwnerNextSnapshotAdapter(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	step := map[string]any{"owner_step": true, "plan_node_id": "p1", "title": "Owner review"}
	missions := []*progressMission{{id: "M-1", state: "active", next: []any{step}, tasks: []*ProgressTask{{TaskID: "t1", DependsOn: []string{"t2"}}}}}
	eta := map[string]map[string]any{"M-1": {"state": "estimated", "earliest": now.Format(time.RFC3339), "latest": now.Format(time.RFC3339)}}
	got := ownerNext(missions, eta, false, now)
	if got["state"] != "unknown" || got["reason"] != "M-1 waits on task t2, which is in no open goal listed here" {
		t.Fatalf("dependency lost: %v", got)
	}
	missions[0].tasks = nil
	got = ownerNext(missions, eta, false, now)
	if got["mission_id"] != "M-1" || got["ready"] != true || got["earliest"] != now.Format(time.RFC3339) {
		t.Fatalf("ready projection: %v", got)
	}
}
