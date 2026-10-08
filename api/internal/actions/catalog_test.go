package actions

import (
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
	if got := ScaleTier("api", 3, 1, true); got != TierC {
		t.Fatalf("non-daemon = %s, want C", got)
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
	if RestartTier("data") != TierC || RestartTier("bifrost-"+"prod") != TierC || RestartTier("bifrost-platform-prod") != TierC {
		t.Fatal("restart C namespaces")
	}
	if RestartTier("bifrost-stg") != TierB {
		t.Fatal("other namespace should be B")
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
		"unifi_firewall_apply", "stack_install_addon", "stack_upgrade_addon",
		"sweep_failed_backups", "ensure_metrics_server", "ensure_kube_prometheus_stack",
		"sync_kubeconfig", "ensure_kubeconfig_secret", "update_data_clone_schedule",
		"market_data_delete",
	} {
		if !seen[id] {
			t.Fatalf("catalog missing %s", id)
		}
	}
}
