package promote

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/opscontext"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func TestOverlayContext(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	at := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	result := "pass"
	_ = store.Save(ReleaseGateRecord{
		At: at, Result: result, LogPath: "release_gate.log",
		Checks: []GateCheck{
			{ID: "stg-api-monitor", Reachability: probe.ReachOK, Detail: "HTTP 200"},
		},
	})
	base := &opscontext.File{
		Meta:       opscontext.Meta{Version: "1", CatalogVersion: "1"},
		Deployment: opscontext.Deployment{Phase: "P4", ActiveTrack: "gitops"},
		Focus:      opscontext.Focus{Headline: "test", FlywheelPrimary: "B"},
		Milestones: []opscontext.Milestone{{ID: "m1", Status: "OPEN"}},
		Promotion: opscontext.Promotion{
			LastGate: opscontext.LastGate{LogPath: "default.log"},
		},
		EnvironmentsExtended: map[string]opscontext.EnvironmentExtended{
			"staging": {Status: "NOT_STARTED"},
		},
	}
	out := OverlayContext(base, store)
	if out.Promotion.LastGate.At != nil || out.Promotion.LastGate.Result != nil || out.Promotion.LastGate.LogPath != "" {
		t.Fatalf("last_gate must not be published on /context, got %+v", out.Promotion.LastGate)
	}
	if out.EnvironmentsExtended["staging"].Status != "IN_PROGRESS" {
		t.Fatalf("expected staging IN_PROGRESS, got %s", out.EnvironmentsExtended["staging"].Status)
	}
	if base.EnvironmentsExtended["staging"].Status != "NOT_STARTED" {
		t.Fatalf("OverlayContext must not mutate base EnvironmentsExtended map")
	}
}

func TestOverlayContextConcurrentSafe(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	_ = store.Save(ReleaseGateRecord{
		At: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC), Result: "pass",
		Checks: []GateCheck{{ID: "stg-api-monitor", Reachability: probe.ReachOK}},
	})
	base := &opscontext.File{
		EnvironmentsExtended: map[string]opscontext.EnvironmentExtended{
			"staging": {Status: "NOT_STARTED"},
		},
	}
	const n = 64
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			_ = OverlayContext(base, store)
			done <- struct{}{}
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}
	if base.EnvironmentsExtended["staging"].Status != "NOT_STARTED" {
		t.Fatalf("base mutated under concurrency: %+v", base.EnvironmentsExtended["staging"])
	}
}
