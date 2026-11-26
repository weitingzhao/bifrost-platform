package actions

import (
	"context"
	"encoding/json"
	"testing"
)

func TestParamsHashStableAcrossKeyOrderAndRoundTrip(t *testing.T) {
	a := map[string]any{"b": 1, "a": "x", "n": float64(2)}
	b := map[string]any{"a": "x", "n": 2, "b": float64(1)}
	ha, err := ParamsHash(a)
	if err != nil {
		t.Fatal(err)
	}
	hb, err := ParamsHash(b)
	if err != nil {
		t.Fatal(err)
	}
	if ha != hb {
		t.Fatalf("hash mismatch %s vs %s", ha, hb)
	}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	hc, err := ParamsHash(back)
	if err != nil {
		t.Fatal(err)
	}
	if ha != hc {
		t.Fatalf("round-trip hash %s, want %s", hc, ha)
	}
}

func TestScaleTierDoesNotWeakenDaemonScaleUp(t *testing.T) {
	if got := ScaleTier("daemon", 2, 1, true); got != TierX {
		t.Fatalf("known scale-up = %s, want X", got)
	}
	if got := ScaleTier("daemon", 1, 1, true); got != TierC {
		t.Fatalf("same count = %s, want C", got)
	}
	if got := ScaleTier("daemon", 0, 2, true); got != TierC {
		t.Fatalf("scale-down = %s, want C", got)
	}
	if got := ScaleTier("daemon", 2, 0, false); got != TierX {
		t.Fatalf("unknown current with replicas>0 = %s, want X so the direct path still hits D10", got)
	}
	if got := ScaleTier("daemon", 0, 0, false); got != TierC {
		t.Fatalf("scale to zero with unknown current = %s, want C", got)
	}
	// Decision 2026-11-25 #783 (wall:106): scaling anything but daemon is
	// undone within a minute, so it is tier B.
	if got := ScaleTier("api", 3, 1, true); got != TierB {
		t.Fatalf("non-daemon = %s, want B", got)
	}
}

func TestPipelineAndAppTiers(t *testing.T) {
	for _, name := range []string{
		"bifrost-deliver-prod",
		"bifrost-deliver-platform-prod",
		"bifrost-deliver-research",
	} {
		if !ProdPipeline(name) {
			t.Fatalf("%s should be a production pipeline", name)
		}
	}
	// Staging deliver and image-build pipelines stay B. A -prod suffix that is
	// not in the explicit list is not enough.
	for _, name := range []string{
		"bifrost-deliver-platform",
		"bifrost-deliver-stg",
		"bifrost-build-stg",
		"bifrost-build-research-dagster",
		"bifrost-build-research-pine",
		"bifrost-build-frontend-stg",
		"bifrost-build-flex-query",
		"bifrost-build-ib-gateway",
		"bifrost-build-market-data",
		"bifrost-ci-frontend",
		"bifrost-ci-platform",
		"bifrost-ci-python",
		"bifrost-clone-frontend-smoke",
		"bifrost-smoke",
		"not-a-pipeline-prod",
	} {
		if ProdPipeline(name) {
			t.Fatalf("%s is not a production workload pipeline", name)
		}
	}
	if !ProdApp("trade-prod") || !ProdApp("prod-trade") || ProdApp("trade-stg") {
		t.Fatal("prod app rule")
	}
	// Decision 2026-11-25 #783 (wall:122): a restart is B everywhere, the
	// PROD and data namespaces included.
	for _, ns := range []string{"data", "bifrost-" + "prod", "bifrost-platform-prod", "bifrost-stg"} {
		if RestartTier(ns) != TierB {
			t.Fatalf("restart in %s = %s, want B", ns, RestartTier(ns))
		}
	}
	// Decision 2026-11-25 #783 (wall:123): a rollback is B for every
	// application; syncing a PROD application stays C.
	if rb, _ := ByID("gitops_rollback_app"); rb.Tier != TierB || rb.Classify != nil {
		t.Fatalf("gitops_rollback_app = %s, want plain B", rb.Tier)
	}
	if sync, _ := ByID("gitops_sync_app"); sync.Classify == nil || sync.Classify(t.Context(), map[string]any{"name": "bifrost-" + "prod"}) != TierC {
		t.Fatal("syncing a PROD application must stay C")
	}
}

func TestIBTiersAreAssigned(t *testing.T) {
	mode, ok := ByID("ib_mode")
	if !ok || mode.Tier != TierC {
		t.Fatalf("ib_mode = %s ok=%v", mode.Tier, ok)
	}
	maint, ok := ByID("ib_maintenance")
	if !ok || maint.Tier != TierC {
		t.Fatalf("ib_maintenance = %s ok=%v", maint.Tier, ok)
	}
	heal, ok := ByID("ib_self_heal")
	if !ok || heal.Tier != TierB {
		t.Fatalf("ib_self_heal = %s ok=%v, want B", heal.Tier, ok)
	}
}

func TestCatalogIDsUniqueAndLeveled(t *testing.T) {
	seen := map[string]bool{}
	for _, a := range Catalog() {
		if seen[a.ID] {
			t.Fatalf("duplicate action %s", a.ID)
		}
		seen[a.ID] = true
		if !a.Tier.Valid() || a.Tier == TierX {
			t.Fatalf("%s resting tier %s", a.ID, a.Tier)
		}
		if a.Description == "" {
			t.Fatalf("%s missing description", a.ID)
		}
	}
	for _, id := range []string{
		"start_pipeline_run", "delete_pipeline_run", "gitops_sync_app", "gitops_rollback_app",
		"rollout_restart_deployment", "scale_deployment", "delete_pod", "cordon_node", "uncordon_node",
		"drain_node", "poweroff_compute_node", "wake_compute_node",
		"trigger_cnpg_backup", "repair_cnpg_wal_store", "trigger_data_clone", "market_data_heal",
		"ib_reconnect", "ib_mode", "ib_maintenance", "ib_self_heal",
		"unifi_firewall_apply", "unifi_client_fixed_ip",
		"sweep_failed_backups", "ensure_metrics_server", "ensure_kube_prometheus_stack",
		"sync_kubeconfig", "ensure_kubeconfig_secret", "update_data_clone_schedule",
		"market_data_delete",
	} {
		if !seen[id] {
			t.Fatalf("catalog missing %s", id)
		}
	}
}

func TestUniFiFixedIPCatalogAndCard(t *testing.T) {
	a, ok := ByID("unifi_client_fixed_ip")
	if !ok || a.Tier != TierD || a.Method != "POST" || a.Pattern != "/api/v1/network/clients/fixed-ip" || len(a.Params) != 3 {
		t.Fatalf("action=%+v", a)
	}
	params := map[string]any{"mac": "68:1d:ef:5f:29:dd", "ip": "192.168.10.80", "name": "ubt-k3s-07"}
	for _, p := range a.Params {
		if p.Type != "string" || !p.Required || p.In != "body" {
			t.Fatalf("param=%+v", p)
		}
		missing := map[string]any{}
		for k, v := range params {
			if k != p.Name {
				missing[k] = v
			}
		}
		if a.Missing(missing) != p.Name {
			t.Fatalf("missing %s accepted", p.Name)
		}
	}
	d := a.Describe(context.Background(), params, "reserve new node")
	if d.Env != "network" || len(d.KeyParams) != 3 || d.Summary != "Set UniFi client 68:1d:ef:5f:29:dd to fixed IP 192.168.10.80 and name ubt-k3s-07" {
		t.Fatalf("card=%+v", d)
	}
}

// 2026-11-19: the Command page's "done for you" list showed plan_manifest
// trials as work done; the page filters on effect instead of a hand-kept list.
func TestEffectMarksOnlyTrialsAsNone(t *testing.T) {
	for _, a := range Catalog() {
		v := a.View()
		want := EffectChanges
		if a.ID == "plan_manifest" {
			want = EffectNone
		}
		if v.Effect != want {
			t.Fatalf("%s effect = %q, want %q", a.ID, v.Effect, want)
		}
	}
}

// Decision 2026-11-25 #783 (wall:110, wall:112, wall:116; LAW-6 二): an
// on-demand backup only adds one, a data clone matches the clone schedule
// (already B), and ensuring declared namespaces only adds them. They are
// tier B; moving them is a change of the referee and needs the Owner.
func TestUndoableActionsFromDecision783AreTierB(t *testing.T) {
	for _, id := range []string{"trigger_cnpg_backup", "trigger_data_clone", "ensure_bifrost_namespaces"} {
		if a, ok := ByID(id); !ok || a.Tier != TierB {
			t.Errorf("%s = %+v %v, want tier B", id, a.Tier, ok)
		}
	}
	// Positive control: an action #783 does not cover stays where it was.
	if a, _ := ByID("repair_cnpg_wal_store"); a.Tier != TierD {
		t.Errorf("repair_cnpg_wal_store = %s, want D", a.Tier)
	}
}
