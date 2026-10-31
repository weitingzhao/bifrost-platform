package assessmentview

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadListKeepsCompletedSnapshotsWhenAnotherReadStalls(t *testing.T) {
	// Even delayed cleanup after cancellation must not delay the HTTP response.
	release := make(chan struct{})
	finished := make(chan struct{})
	ids := []string{"ready", "empty", "failed", "slow", "ready"}
	var calls atomic.Int32
	start := time.Now()
	out := ReadList(context.Background(), ids, func(ctx context.Context, id string) map[string]any {
		calls.Add(1)
		switch id {
		case "slow":
			<-ctx.Done()
			<-release
			defer close(finished)
			return map[string]any{"events": "late result"}
		case "failed":
			return map[string]any{"error": "read failed"}
		default:
			return map[string]any{"events": id}
		}
	})
	// Release the late reader and ensure it cannot rewrite the returned map.
	defer func() {
		close(release)
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Error("late reader did not exit")
		}
		if out["slow"]["error"] == nil {
			t.Error("late reader mutated the response")
		}
	}()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("list waited for a stalled reader: %s", elapsed)
	}
	if calls.Load() != 4 || len(out) != 4 || out["ready"]["events"] != "ready" || out["empty"]["events"] != "empty" || out["failed"]["error"] != "read failed" || out["slow"]["error"] == nil {
		t.Fatalf("lost or misassigned snapshots: %v; calls=%d", out, calls.Load())
	}
}

func TestReadListHonorsCancellationAndSkipsUnusedReaders(t *testing.T) {
	read := func(context.Context, string) map[string]any {
		t.Error("unexpected assessment read")
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ids := make([]string, 20)
	for i := range ids {
		ids[i] = fmt.Sprint(i)
	}
	out := ReadList(ctx, ids, read)
	if len(out) != len(ids) {
		t.Fatalf("missing unavailable snapshots: %v", out)
	}
	for _, snapshot := range out {
		if snapshot["error"] == nil {
			t.Fatalf("cancellation masquerades as no assessment: %v", snapshot)
		}
	}
	if len(ReadList(context.Background(), nil, read)) != 0 || len(ReadList(context.Background(), ids, nil)) != 0 {
		t.Fatal("empty or unwired list should have no snapshots")
	}
}
