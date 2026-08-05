package promote

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/opscontext"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func newTestPromoteService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	t.Setenv("PLATFORM_DATA_DIR", dataDir)
	t.Setenv("PLATFORM_RELEASE_GATE_STATE", "")
	return &Service{
		cfg:      &config.Config{ConfigPath: filepath.Join(dir, "config", "environments.yaml")},
		prober:   probe.NewProber(),
		store:    NewStore(filepath.Join(dir, "config")),
		delivery: delivery.NewService(nil),
	}
}

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

func TestLastGateTable(t *testing.T) {
	cases := []struct {
		name   string
		tier   GateTier
		seed   *ReleaseGateRecord
		want   string
		ready  bool
		detail string
	}{
		{
			name:   "empty-stg",
			tier:   GateTierStg,
			want:   "",
			detail: "No stg release gate recorded yet",
		},
		{
			name: "pass-stg",
			tier: GateTierStg,
			seed: &ReleaseGateRecord{
				At: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Result: "pass",
				Revision: "v1.2.3", Summary: "stg release gate pass (2 checks)",
				Checks: []GateCheck{{ID: "c1", Required: true, Reachability: probe.ReachOK}},
			},
			want:  "pass",
			ready: true,
		},
		{
			name: "fail-prod",
			tier: GateTierProd,
			seed: &ReleaseGateRecord{
				At: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), Result: "fail",
				Summary: "prod release gate fail",
				Checks:  []GateCheck{{ID: "c1", Required: true, Reachability: probe.ReachFail}},
			},
			want:  "fail",
			ready: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestPromoteService(t)
			if tc.seed != nil {
				if err := svc.store.SaveTier(tc.tier, *tc.seed); err != nil {
					t.Fatalf("SaveTier: %v", err)
				}
			}
			got := svc.LastGate(context.Background(), tc.tier)
			if got.Result != tc.want {
				t.Fatalf("Result = %q, want %q", got.Result, tc.want)
			}
			if got.Ready != tc.ready {
				t.Fatalf("Ready = %v, want %v (blockers=%v)", got.Ready, tc.ready, got.Blockers)
			}
			if tc.detail != "" && got.Detail != tc.detail {
				t.Fatalf("Detail = %q, want %q", got.Detail, tc.detail)
			}
			if got.Tier != tc.tier {
				t.Fatalf("Tier = %q, want %q", got.Tier, tc.tier)
			}
		})
	}
}

func TestRunReleaseGatePersistsTable(t *testing.T) {
	cases := []struct {
		name   string
		tier   GateTier
		wantOK bool
	}{
		// Without cluster/delivery success, required checks fail → ok=false, but still persisted.
		{name: "stg-no-cluster", tier: GateTierStg, wantOK: false},
		{name: "platform-stg-no-cluster", tier: GateTierPlatformStg, wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestPromoteService(t)
			resp, err := svc.RunReleaseGate(context.Background(), tc.tier, "tester")
			if err != nil {
				t.Fatalf("RunReleaseGate: %v", err)
			}
			if resp.OK != tc.wantOK {
				t.Fatalf("OK = %v, want %v (msg=%q)", resp.OK, tc.wantOK, resp.Message)
			}
			if resp.Action != "promote.release-gate" {
				t.Fatalf("Action = %q", resp.Action)
			}
			if resp.Gate.Result == "" {
				t.Fatal("expected gate result to be set")
			}
			loaded, loadErr := svc.store.LoadTier(tc.tier)
			if loadErr != nil || loaded == nil {
				t.Fatalf("LoadTier after run: loaded=%v err=%v", loaded, loadErr)
			}
			if loaded.TriggeredBy != "tester" {
				t.Fatalf("TriggeredBy = %q, want tester", loaded.TriggeredBy)
			}
			if loaded.Result != resp.Gate.Result {
				t.Fatalf("persisted Result = %q, want %q", loaded.Result, resp.Gate.Result)
			}
		})
	}
}

func TestReleaseStateGateStatusTable(t *testing.T) {
	cases := []struct {
		name     string
		tier     string
		stgGate  *ReleaseGateRecord
		prodGate *ReleaseGateRecord
		wantStg  string
		wantProd string
		wantNext string
	}{
		{
			name:     "trade-empty",
			tier:     "trade",
			wantStg:  "none",
			wantProd: "none",
			wantNext: "deploy_stg",
		},
		{
			name: "trade-stg-gate-recorded",
			tier: "trade",
			stgGate: &ReleaseGateRecord{
				At: time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
				Result: "pass", Revision: "v9", Summary: "stg pass",
			},
			wantStg:  "pass",
			wantProd: "none",
			// No PipelineRun → StgDeploy stays none → next stays deploy_stg.
			wantNext: "deploy_stg",
		},
		{
			name:     "platform-empty",
			tier:     "platform",
			wantStg:  "none",
			wantProd: "none",
			wantNext: "deploy_stg",
		},
		{
			name: "platform-stg-fail",
			tier: "platform",
			stgGate: &ReleaseGateRecord{
				At: time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC),
				Result: "fail", Summary: "platform stg fail",
			},
			wantStg:  "fail",
			wantProd: "none",
			wantNext: "deploy_stg",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestPromoteService(t)
			stgTier, prodTier := GateTierStg, GateTierProd
			if tc.tier == "platform" {
				stgTier, prodTier = GateTierPlatformStg, GateTierPlatformProd
			}
			if tc.stgGate != nil {
				if err := svc.store.SaveTier(stgTier, *tc.stgGate); err != nil {
					t.Fatalf("SaveTier stg: %v", err)
				}
			}
			if tc.prodGate != nil {
				if err := svc.store.SaveTier(prodTier, *tc.prodGate); err != nil {
					t.Fatalf("SaveTier prod: %v", err)
				}
			}
			state := svc.ReleaseState(context.Background(), tc.tier)
			if state.StgGate.Status != tc.wantStg {
				t.Fatalf("StgGate.Status = %q, want %q", state.StgGate.Status, tc.wantStg)
			}
			if state.ProdGate.Status != tc.wantProd {
				t.Fatalf("ProdGate.Status = %q, want %q", state.ProdGate.Status, tc.wantProd)
			}
			if state.NextAction == nil || state.NextAction.Action != tc.wantNext {
				t.Fatalf("NextAction = %+v, want %q", state.NextAction, tc.wantNext)
			}
			if !state.Consistent {
				t.Fatalf("Consistent = false, warnings=%v", state.Warnings)
			}
		})
	}
}

func TestResolveReleaseActionsTable(t *testing.T) {
	svc := &Service{}
	cases := []struct {
		name     string
		state    ReleaseStateResponse
		wantNext string
	}{
		{
			name:     "no-stg-deploy",
			state:    ReleaseStateResponse{StgDeploy: ReleaseStageState{Status: "none"}},
			wantNext: "deploy_stg",
		},
		{
			name: "stg-deployed-gate-missing",
			state: ReleaseStateResponse{
				StgDeploy: ReleaseStageState{Status: "succeeded", Revision: "v1"},
				StgGate:   ReleaseStageState{Status: "none"},
			},
			wantNext: "run_stg_gate",
		},
		{
			name: "stg-gate-fail",
			state: ReleaseStateResponse{
				StgDeploy: ReleaseStageState{Status: "succeeded", Revision: "v1"},
				StgGate:   ReleaseStageState{Status: "fail"},
			},
			wantNext: "run_stg_gate",
		},
		{
			name: "stg-pass-needs-prod-deploy",
			state: ReleaseStateResponse{
				StgDeploy:  ReleaseStageState{Status: "succeeded", Revision: "v2"},
				StgGate:    ReleaseStageState{Status: "pass", Revision: "v2"},
				ProdDeploy: ReleaseStageState{Status: "none"},
			},
			wantNext: "deploy_prod",
		},
		{
			name: "prod-deployed-needs-gate",
			state: ReleaseStateResponse{
				StgDeploy:  ReleaseStageState{Status: "succeeded", Revision: "v3"},
				StgGate:    ReleaseStageState{Status: "pass", Revision: "v3"},
				ProdDeploy: ReleaseStageState{Status: "succeeded", Revision: "v3"},
				ProdGate:   ReleaseStageState{Status: "none"},
			},
			wantNext: "run_prod_gate",
		},
		{
			name: "fully-released",
			state: ReleaseStateResponse{
				StgDeploy:  ReleaseStageState{Status: "succeeded", Revision: "v4"},
				StgGate:    ReleaseStageState{Status: "pass", Revision: "v4"},
				ProdDeploy: ReleaseStageState{Status: "succeeded", Revision: "v4"},
				ProdGate:   ReleaseStageState{Status: "pass", Revision: "v4"},
			},
			wantNext: "released",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, next := svc.resolveReleaseActions(tc.state, "bifrost-deliver-stg", "bifrost-deliver-prod", GateTierStg, GateTierProd)
			if next == nil || next.Action != tc.wantNext {
				t.Fatalf("next = %+v, want %q", next, tc.wantNext)
			}
		})
	}
}
