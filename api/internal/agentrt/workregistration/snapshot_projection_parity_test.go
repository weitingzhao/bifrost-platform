package workregistration

import (
	"reflect"
	"testing"
	"time"
)

// Origin: moved from store/snapshot_projection_parity_test.go (TestSnapshotDisplayFrozenParity, the external activity half), 2026-11-18 (TD-582).
func TestSnapshotDisplayFrozenParity(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 16, 0, 0, 0, 0, time.FixedZone("offset", 3600))
	observations := func() []map[string]any {
		return []map[string]any{{"external_work_id": "1", "state": "Registered", "ended_at": "2026-11-15T00:00:00Z"}, {"external_work_id": "2", "state": "Running", "ended_at": "2026-11-16T00:00:00Z"}, {"external_work_id": "3", "state": "Done", "ended_at": "short"}, {"external_work_id": "4", "state": "Unconfirmed"}}
	}
	old, new := observations(), observations()
	working, finished := []any{}, []any{}
	var oldTrace, newTrace []string
	verify := func(id string) any { return map[string]any{"id": id, "state": "Unknown"} }
	for _, e := range old {
		oldTrace = append(oldTrace, e["external_work_id"].(string))
		e["verification"] = verify(e["external_work_id"].(string))
		if e["state"] == "Running" || e["state"] == "Registered" {
			working = append(working, e)
		}
		if end, _ := e["ended_at"].(string); len(end) >= 10 && end[:10] == now.UTC().Format("2006-01-02") {
			finished = append(finished, e)
		}
	}
	gw, gf := ActivityBuckets(new, now, func(id string) any { newTrace = append(newTrace, id); return verify(id) })
	if !reflect.DeepEqual(old, new) || !reflect.DeepEqual(working, gw) || !reflect.DeepEqual(finished, gf) || !reflect.DeepEqual(oldTrace, newTrace) {
		t.Fatal("activity output or callback order differs")
	}
}
