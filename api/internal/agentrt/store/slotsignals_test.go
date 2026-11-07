package store_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// R5 phase 2 step 3b: a host's headroom is recorded when a claim first
// reports it, again when it moved materially, not while it holds still, and
// SlotSignals reads the latest; a claim with none records nothing, and one no
// host can report is refused.
// Serial: it sets HeadroomEvery, which every headroom record reads.
func TestSlotSignalsHeadroomIsRecordedWhenItMoves(t *testing.T) {
	w := newKernelWorld(t)
	h := store.Headroom{Load1: pv(1.5), CPUs: pv(8), MemAvail: pv(int64(12 << 30)), MemTotal: pv(int64(32 << 30))}
	for i, step := range []struct {
		h    store.Headroom
		want bool
	}{
		{h, true},
		{h, false},
		{store.Headroom{Load1: pv(2.0), CPUs: pv(8), MemAvail: pv(int64(11 << 30)), MemTotal: pv(int64(32 << 30))}, false},
		{store.Headroom{Load1: pv(4.0), CPUs: pv(8), MemAvail: pv(int64(11 << 30)), MemTotal: pv(int64(32 << 30))}, true},
		{store.Headroom{Load1: pv(4.0), CPUs: pv(8), MemAvail: pv(int64(2 << 30)), MemTotal: pv(int64(32 << 30))}, true},
		{store.Headroom{Load1: pv(4.0), CPUs: pv(8)}, true},
		{store.Headroom{}, false},
	} {
		got, err := store.RecordHostHeadroom(ctx, w.rt, "h1", step.h)
		ok(t, err)
		if got != step.want {
			t.Fatalf("step %d (%+v): recorded %v, want %v", i, step.h, got, step.want)
		}
	}
	for _, bad := range []store.Headroom{{Load1: pv(-1.0)}, {CPUs: pv(0)}, {MemAvail: pv(int64(1))},
		{MemAvail: pv(int64(2)), MemTotal: pv(int64(1))}} {
		if _, err := store.RecordHostHeadroom(ctx, w.rt, "h1", bad); err == nil {
			t.Fatalf("%+v was recorded", bad)
		}
	}
	s, err := store.SlotSignals(ctx, w.rt, "h1", time.Time{})
	ok(t, err)
	if s.Headroom == nil || s.Headroom.Load1 == nil || *s.Headroom.Load1 != 4 || *s.Headroom.CPUs != 8 || s.Headroom.MemAvail != nil ||
		s.Headroom.At == nil {
		t.Fatalf("headroom %+v", s.Headroom)
	}
	if s.IntervalS != int64(store.SlotSignalInterval/time.Second) {
		t.Fatalf("interval %d s", s.IntervalS)
	}
	if s, err = store.SlotSignals(ctx, w.rt, "h2", time.Time{}); err != nil || s.Headroom != nil {
		t.Fatalf("a host that reported none: %+v %v", s.Headroom, err)
	}
	// One that holds still is recorded again once its record is old.
	defer func(e time.Duration) { store.HeadroomEvery = e }(store.HeadroomEvery)
	store.HeadroomEvery = 0
	if got, err := store.RecordHostHeadroom(ctx, w.rt, "h1", store.Headroom{Load1: pv(4.0), CPUs: pv(8)}); err != nil || !got {
		t.Fatalf("an old record: %v %v", got, err)
	}
}

// R5 phase 2 step 3b: an attempt that ended on a usage limit counts as a
// refusal of its vendor pool (tool:account, every window), with the cooldown
// it set, for every host whose offered tools route to the pool, whichever
// host ran it; a host whose tools route elsewhere reads that pool's
// neighbours at zero. The queue counts the leasable tasks, and the host
// that ran the attempt its duration.
func TestSlotSignalsCountRefusalsPerPoolQueueAndDurations(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	_, err := store.RecordHostOffered(ctx, w.rt, "h1", []store.Tool{cursor, codex}, false)
	ok(t, err)
	_, err = store.RecordHostOffered(ctx, w.rt, "h2", []store.Tool{codex}, false)
	ok(t, err)
	w.ready(t, store.Task{})
	f := w.started(t, "h3", cursor)
	_, err = store.ComputedWith(ctx, w.rt, f, store.Ending{ExitCode: pv(1), EndHow: "exit", Conclusion: "failed",
		Pools: []store.PoolReading{{Pool: "cursor:owner:monthly", Limited: true, Text: "You've hit your usage limit"}}})
	ok(t, err)
	w.ready(t, store.Task{})
	w.ready(t, store.Task{})

	s, err := store.SlotSignals(ctx, w.rt, "h1", time.Time{})
	ok(t, err)
	if len(s.Pools) != 2 || s.Pools[0].Pool != "codex:owner" || s.Pools[1].Pool != "cursor:owner" {
		t.Fatalf("h1's pools %+v", s.Pools)
	}
	if c := s.Pools[1]; c.Refusals != 1 || c.LimitedAttempts != 1 || c.Cooldowns != 1 || c.CoolingUntil == nil || c.LastAt == nil {
		t.Fatalf("cursor:owner %+v", c)
	}
	if c := s.Pools[0]; c.Refusals != 0 || c.LimitedAttempts != 0 || c.Cooldowns != 0 || c.CoolingUntil != nil || c.LastAt != nil {
		t.Fatalf("codex:owner %+v", c)
	}
	if s.QueueDepth != 2 {
		t.Fatalf("queue depth %d, want the 2 ready tasks", s.QueueDepth)
	}
	if s.Durations.Count != 0 || s.Durations.MedianS != nil {
		t.Fatalf("h1 ran nothing: %+v", s.Durations)
	}
	s2, err := store.SlotSignals(ctx, w.rt, "h2", time.Time{})
	ok(t, err)
	if len(s2.Pools) != 1 || s2.Pools[0].Pool != "codex:owner" || s2.Pools[0].Refusals != 0 {
		t.Fatalf("h2's pools %+v", s2.Pools)
	}
	var all map[string]store.HostSlotSignals
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		all, err = store.HostsSlotSignals(ctx, tx, time.Time{})
		return err
	}))
	h3 := all["h3"]
	if len(all) != 8 || h3.Durations.Count != 1 || h3.Durations.MedianS == nil || *h3.Durations.MedianS < 0 || h3.Durations.PreviousCount != 0 {
		t.Fatalf("%d hosts; h3 %+v", len(all), h3.Durations)
	}
	if len(h3.Pools) != 0 {
		t.Fatalf("h3's claims never said what it offers, yet it has pools %+v", h3.Pools)
	}
	// Signals since now are those of the last interval: none is in the
	// future.
	later, err := store.SlotSignals(ctx, w.rt, "h1", time.Now().Add(time.Hour))
	ok(t, err)
	if later.Pools[1].Refusals != 1 {
		t.Fatalf("since a time not yet come: %+v", later.Pools[1])
	}
}
