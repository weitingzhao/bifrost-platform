package checklist

import (
	"strings"
	"testing"
	"time"
)

// TD-253: a signal is true only for SignalTTL after it was observed.
func TestStaleSignalsReadAsUnknown(t *testing.T) {
	now := time.Date(2026, 10, 7, 6, 0, 0, 0, time.UTC)
	in := []ItemSignal{
		{ItemID: "redis", Signal: SignalOK, ObservedAt: now.Add(-10 * time.Minute).Format(time.RFC3339)},
		{ItemID: "db-backup-fresh", Signal: SignalFail, Detail: "July backup", ObservedAt: now.Add(-200 * time.Hour).Format(time.RFC3339)},
		{ItemID: "nodes-ready", Signal: SignalOK, Detail: "6/6"}, // written before signals carried a time
		{ItemID: "ib-feed", Signal: SignalUnknown},
	}
	out := withStaleness(in, now, 2*time.Hour)
	if out[0].Signal != SignalOK || out[0].Stale {
		t.Fatalf("fresh signal changed: %+v", out[0])
	}
	if out[1].Signal != SignalUnknown || !out[1].Stale || !strings.Contains(out[1].Detail, "said fail: July backup") {
		t.Fatalf("old fail must read unknown and keep what it said: %+v", out[1])
	}
	if out[2].Signal != SignalUnknown || !strings.Contains(out[2].Detail, "no observation time") {
		t.Fatalf("a signal with no time must read unknown: %+v", out[2])
	}
	if out[3].Stale {
		t.Fatal("an unknown signal is not marked stale")
	}
	if in[1].Signal != SignalFail {
		t.Fatal("withStaleness must not modify the stored record")
	}
}

func TestMergeStampsObservationTimeAndSource(t *testing.T) {
	t.Setenv("PLATFORM_DATA_DIR", t.TempDir())
	s := NewStore(t.TempDir())
	resp, err := s.Merge(MergeRequest{Source: "checklist-prober", Signals: []ItemSignal{{ItemID: "redis", Signal: "ok"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Signals) != 1 || resp.Signals[0].ObservedAt == "" || resp.Signals[0].Source != "checklist-prober" || resp.Signals[0].Signal != SignalOK {
		t.Fatalf("merged signal: %+v", resp.Signals)
	}
}
