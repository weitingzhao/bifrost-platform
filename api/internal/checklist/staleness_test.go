package checklist

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/datahusbandry"
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

func TestStoreDropsItemsNoLongerInTheCatalog(t *testing.T) {
	t.Setenv("PLATFORM_DATA_DIR", t.TempDir())
	s := NewStore(t.TempDir())
	// Shaped like the PROD state file on 2026-10-10.
	rec := FileRecord{Version: stateVersion, Signals: []ItemSignal{
		{ItemID: "redis", Signal: SignalOK, ObservedAt: time.Now().UTC().Format(time.RFC3339)},
		{ItemID: "runners-ha", Signal: SignalUnknown, Detail: "no runners configured"},
		{ItemID: "git-bridge", Signal: SignalUnknown, Detail: "local-only (dev workstation)"},
		{ItemID: "mac-probe-bridge", Signal: SignalUnknown, Detail: "local-only (dev workstation)"},
		{ItemID: "hermes-tooling", Signal: SignalDegraded, Detail: "not ready", ObservedAt: "2026-10-09T00:39:39Z"},
	}}
	if err := s.saveLocked(&rec); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Signals) != 1 || got.Signals[0].ItemID != "redis" {
		t.Fatalf("Get kept retired items: %+v", got.Signals)
	}
	merged, err := s.Merge(MergeRequest{Source: "checklist-prober", Signals: []ItemSignal{{ItemID: "nodes-ready", Signal: "ok"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Signals) != 2 {
		t.Fatalf("Merge carried retired items forward: %+v", merged.Signals)
	}
	if len(merged.NewFailures) != 0 {
		t.Fatalf("a retired item counted as a new failure: %v", merged.NewFailures)
	}
}

type emptyHusbandry struct{}

func (emptyHusbandry) Snapshot(context.Context) datahusbandry.Snapshot {
	return datahusbandry.Snapshot{}
}

func TestLiveHusbandrySignalsCarryTheirObservation(t *testing.T) {
	h := NewHandler(t.TempDir(), nil)
	h.BindHusbandry(emptyHusbandry{})
	sigs := h.liveHusbandrySignals(context.Background())
	if len(sigs) != 3 {
		t.Fatalf("got %d husbandry signals, want 3", len(sigs))
	}
	for _, s := range sigs {
		if s.ObservedAt == "" || s.Source != "data-husbandry" {
			t.Errorf("%s: observed_at=%q source=%q", s.ItemID, s.ObservedAt, s.Source)
		}
	}
}
