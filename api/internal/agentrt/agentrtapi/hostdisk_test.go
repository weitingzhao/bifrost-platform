package agentrtapi_test

import (
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// diskPages are the ids of the pages the server sent the Owner of a host's
// disk (API.NotifyOwner in server).
var (
	diskPagesMu sync.Mutex
	diskPages   []string
)

func pagedDisk(id string) bool {
	diskPagesMu.Lock()
	defer diskPagesMu.Unlock()
	return slices.Contains(diskPages, id)
}

// diskRecords counts host h1's disk_low alerts and its notes that it is back
// above the floor.
func diskRecords(t *testing.T, pool *pgxpool.Pool) (alerts, back int) {
	t.Helper()
	must(t, pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE event_type = 'policy_alert' AND body_json->>'rule' = 'disk_low'),
	    count(*) FILTER (WHERE event_type = 'note' AND starts_with(body_json->>'text', 'h1 has free disk above the floor again'))
	  FROM agentrt.event WHERE event_stream_id = 'host:h1'`).Scan(&alerts, &back))
	return alerts, back
}

// TD-415: a host whose claim reports free disk below the floor (15 GiB by
// default) on its root's volume or agentd's is given no new work, and the
// platform records it once and pages the Owner; once the host reports room
// again it takes the ready task, and that is recorded and paged once. Each
// value goes with the other.
func TestAHostLowOnDiskTakesNoWorkUntilItHasRoom(t *testing.T) {
	pool, task := world(t)
	s := server(t, pool)
	cursor := map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}
	claim := func(host, root int64) map[string]any {
		return map[string]any{"host_id": "h1", "free_slots": 1, "tools": []any{cursor}, "disk_free_bytes": host, "root_free_bytes": root}
	}
	const low, room = int64(10) << 30, int64(100) << 30
	for i, body := range []map[string]any{claim(room, low), claim(low, room)} {
		if code, out := call(t, s, "agentd:h1", "POST", "claim", body); code != http.StatusNoContent {
			t.Fatalf("claim %d below the floor: %d %v", i, code, out)
		}
	}
	if alerts, back := diskRecords(t, pool); alerts != 1 || back != 0 {
		t.Fatalf("below twice: %d alerts, %d notes", alerts, back)
	}
	if code, out := call(t, s, "agentd:h1", "POST", "claim", map[string]any{"host_id": "h1", "free_slots": 1, "tools": []any{cursor},
		"disk_free_bytes": room}); code != http.StatusBadRequest {
		t.Fatalf("one value alone: %d %v", code, out)
	}
	code, out := call(t, s, "agentd:h1", "POST", "claim", claim(room, room))
	if code != http.StatusOK || out["task_id"] != task {
		t.Fatalf("claim with room again: %d %v", code, out)
	}
	if alerts, back := diskRecords(t, pool); alerts != 1 || back != 1 {
		t.Fatalf("back above: %d alerts, %d notes", alerts, back)
	}
	for deadline := time.Now().Add(5 * time.Second); !pagedDisk("disk:h1:below") || !pagedDisk("disk:h1:above"); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			diskPagesMu.Lock()
			defer diskPagesMu.Unlock()
			t.Fatalf("pages %v", diskPages)
		}
	}
}
