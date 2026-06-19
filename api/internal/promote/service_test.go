package promote

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
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
		Meta: opscontext.Meta{Version: "1", CatalogVersion: "1"},
		Deployment: opscontext.Deployment{Phase: "P4", ActiveTrack: "gitops"},
		Focus: opscontext.Focus{Headline: "test", FlywheelPrimary: "B"},
		Milestones: []opscontext.Milestone{{ID: "m1", Status: "OPEN"}},
		Promotion: opscontext.Promotion{
			LastGate: opscontext.LastGate{LogPath: "default.log"},
		},
		EnvironmentsExtended: map[string]opscontext.EnvironmentExtended{
			"staging": {Status: "NOT_STARTED"},
		},
	}
	out := OverlayContext(base, store)
	if out.Promotion.LastGate.At == nil || *out.Promotion.LastGate.Result != "pass" {
		t.Fatalf("expected overlaid gate pass, got %+v", out.Promotion.LastGate)
	}
	if out.EnvironmentsExtended["staging"].Status != "IN_PROGRESS" {
		t.Fatalf("expected staging IN_PROGRESS, got %s", out.EnvironmentsExtended["staging"].Status)
	}
}

func TestNarrativeBlockersGateFail(t *testing.T) {
	rec := ReleaseGateRecord{Result: "fail"}
	blockers := narrativeBlockers(GateTierProd, nil, rec)
	if len(blockers) != 1 || blockers[0] != "Release gate checks failed" {
		t.Fatalf("unexpected blockers: %v", blockers)
	}
}

func TestNarrativeBlockersStgSkipsCutover(t *testing.T) {
	rec := ReleaseGateRecord{Result: "pass"}
	cfg := &config.Config{
		OpsContext: &opscontext.File{
			Milestones: []opscontext.Milestone{
				{ID: "2c-b-prod-cutover", Status: "BLOCKED_ON", Blocker: "decision:D1"},
			},
		},
	}
	blockers := narrativeBlockers(GateTierStg, cfg, rec)
	if len(blockers) != 0 {
		t.Fatalf("stg gate should not include cutover blockers: %v", blockers)
	}
}
