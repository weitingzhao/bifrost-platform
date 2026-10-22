package store_test

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-415: a claim that reports free disk below the floor on either volume
// leases nothing, as one with no free slot, and the same claim with room
// leases the ready task; a claim that reports none is not judged. The floor
// is the default 15 GiB, or the configured one.
func TestAClaimBelowTheDiskFloorLeasesNothing(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{})
	const gib = int64(1) << 30
	for _, d := range []store.HostDisk{{Host: 14 * gib, Root: 100 * gib}, {Host: 100 * gib, Root: 15*gib - 1}} {
		c, err := store.ClaimNext(ctx, w.rt, store.ClaimRequest{HostID: "h1", FreeSlots: 1, Tools: []store.Tool{codex}, Disk: &d})
		ok(t, err)
		if c != nil {
			t.Fatalf("a claim with %+v leased %s", d, c.TaskID)
		}
	}
	if c, err := store.ClaimNext(ctx, w.rt, store.ClaimRequest{HostID: "h1", FreeSlots: 1, Tools: []store.Tool{codex},
		Disk: &store.HostDisk{Host: 100 * gib, Root: 100 * gib}, DiskFloor: 200 * gib}); err != nil || c != nil {
		t.Fatalf("a claim under a configured floor of 200 GiB: %+v %v", c, err)
	}
	c, err := store.ClaimNext(ctx, w.rt, store.ClaimRequest{HostID: "h1", FreeSlots: 1, Tools: []store.Tool{codex},
		Disk: &store.HostDisk{Host: 15 * gib, Root: 15 * gib}})
	ok(t, err)
	if c == nil || c.TaskID != task {
		t.Fatalf("a claim at the floor: %+v", c)
	}
}

// TD-415: a host's disk going below the floor is recorded once, and its
// going back above once, whatever the claims in between.
func TestAHostsDiskIsRecordedOnceEachWay(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	const gib = int64(1) << 30
	low, room := store.HostDisk{Host: 100 * gib, Root: gib}, store.HostDisk{Host: 100 * gib, Root: 100 * gib}
	for i, step := range []struct {
		d    store.HostDisk
		want string
	}{{room, ""}, {low, "below"}, {low, ""}, {room, "above"}, {room, ""}, {low, "below"}} {
		got, err := store.RecordHostDisk(ctx, w.rt, "h1", step.d, 0)
		ok(t, err)
		if got != step.want {
			t.Fatalf("step %d (%+v): %q, want %q", i, step.d, got, step.want)
		}
	}
	var alerts, notes int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FILTER (WHERE event_type = 'policy_alert'), count(*) FILTER (WHERE event_type = 'note')
	  FROM agentrt.event WHERE event_stream_id = 'host:h1' AND actor_id = 'controller'`).Scan(&alerts, &notes))
	if alerts != 2 || notes != 1 {
		t.Fatalf("%d alerts, %d notes", alerts, notes)
	}
}
