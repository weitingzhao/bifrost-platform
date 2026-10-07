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
	if !ProdPipeline("bifrost-deliver-prod") || ProdPipeline("bifrost-deliver-platform") {
		t.Fatal("*-prod pipeline rule")
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
		"drain_node", "poweroff_compute_node", "wake_compute_node", "join_cluster_node",
		"trigger_cnpg_backup", "repair_cnpg_wal_store", "trigger_data_clone", "market_data_heal",
		"ib_reconnect", "unifi_firewall_apply", "stack_install_addon", "stack_upgrade_addon",
	} {
		if !seen[id] {
			t.Fatalf("catalog missing %s", id)
		}
	}
}
