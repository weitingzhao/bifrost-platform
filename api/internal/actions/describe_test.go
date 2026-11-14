package actions

import (
	"context"
	"strings"
	"testing"
)

func TestDescribeSummaries(t *testing.T) {
	for _, c := range []struct {
		id, env, summary string
		params           map[string]any
	}{
		{"start_pipeline_run", "prod", "bifrost-deliver-platform-prod @ 66496a6", map[string]any{"name": "bifrost-deliver-platform-prod", "revision": "66496a6000000000000000000000000000000000"}},
		{"rollout_restart_deployment", "bifrost-prod", "Restart Deployment bifrost-prod/api", map[string]any{"namespace": "bifrost-prod", "kind": "Deployment", "name": "api"}},
		{"drain_node", "node", "Drain node ubt-k3s-02 (force)", map[string]any{"name": "ubt-k3s-02", "force": true}},
		{"owner_run_command", "host", "Delete retired ConfigMap (TD-290)", map[string]any{"command": "kubectl delete cm x", "reason": "Delete retired ConfigMap (TD-290)\nsecond line"}},
	} {
		a, ok := ByID(c.id)
		if !ok {
			t.Fatal(c.id)
		}
		d := a.Describe(context.Background(), c.params, "request reason")
		if d.Env != c.env || d.Summary != c.summary {
			t.Fatalf("%s: env %q summary %q", c.id, d.Env, d.Summary)
		}
		for k := range d.KeyParams {
			if hiddenParams[k] {
				t.Fatalf("%s: hidden param %s in key params", c.id, k)
			}
		}
	}
}

// deploy_agentd's card shows the host, the short commit and the sha256 the
// Owner compares with the package.
func TestDescribeDeployAgentd(t *testing.T) {
	a, ok := ByID("deploy_agentd")
	if !ok || a.Tier != TierD {
		t.Fatal("deploy_agentd is not a tier D action")
	}
	sha := strings.Repeat("ab", 32)
	d := a.Describe(context.Background(), map[string]any{"env": "prod", "host": "mini-1",
		"commit": "0123456789abcdef0123456789abcdef01234567", "sha256": sha}, "request reason")
	if d.Env != "prod" || d.Summary != "Deploy agentd 0123456 on host mini-1 (sha256 "+sha+")" {
		t.Fatalf("env %q summary %q", d.Env, d.Summary)
	}
	if d.KeyParams["host"] != "mini-1" || d.KeyParams["commit"] != "0123456" || d.KeyParams["sha256"] != sha {
		t.Fatalf("key params %#v", d.KeyParams)
	}
}

func TestDescribeFallsBackToTheReason(t *testing.T) {
	a, _ := ByID("trigger_data_clone")
	d := a.Describe(context.Background(), map[string]any{"source": "prod", "targets": []any{"dev"}, "confirmation_token": "tok", "confirm": true}, "x")
	if d.KeyParams["confirmation_token"] != "" || d.KeyParams["confirm"] != "" {
		t.Fatalf("token in key params: %#v", d.KeyParams)
	}
	if !strings.Contains(d.Summary, "Clone prod into dev") {
		t.Fatalf("summary %q", d.Summary)
	}
	long := strings.Repeat("é", 200)
	if got := clip(long, summaryMax); len(got) > summaryMax+len("…") {
		t.Fatalf("clip kept %d bytes", len(got))
	}
}

func TestRunnerOf(t *testing.T) {
	own, _ := ByID("owner_run_command")
	if own.RunnerOf(map[string]any{}) != RunnerOwner || own.RunnerOf(map[string]any{"runner": "host"}) != RunnerHost {
		t.Fatal("owner_run_command runner")
	}
	if rr, _ := ByID("rolling_reboot"); rr.RunnerOf(nil) != RunnerOwner {
		t.Fatal("rolling_reboot runner")
	}
	if c, _ := ByID("cordon_node"); c.RunnerOf(nil) != RunnerPlatform {
		t.Fatal("cordon_node runner")
	}
	p := map[string]any{"runner": "system"}
	if err := NormalizeRunner("owner_run_command", p); err != nil || p["runner"] != RunnerHost {
		t.Fatalf("system alias: %v %v", err, p)
	}
	p = map[string]any{"runner": "owner"}
	if err := NormalizeRunner("owner_run_command", p); err != nil || p["runner"] != nil {
		t.Fatalf("owner is the default and is dropped from params: %v", p)
	}
}

func TestDescribeApplyOwnership(t *testing.T) {
	actMu.RLock()
	previous := planLookup
	actMu.RUnlock()
	t.Cleanup(func() { SetPlanLookup(previous) })
	for _, count := range []int{0, 2} {
		for _, path := range []string{"k8s/cicd", strings.Repeat("long-path/", 30)} {
			SetPlanLookup(func(_ context.Context, id string) (PlanSummary, error) {
				if id != "plan-test" {
					t.Fatalf("plan id = %q", id)
				}
				return PlanSummary{Namespaces: []string{"cicd"}, Path: path, Objects: 3, Migrated: count}, nil
			})
			a, _ := ByID("apply_manifest")
			d := a.Describe(context.Background(), map[string]any{"plan_id": "plan-test"}, "")
			warning := "takes field ownership of 2 cicd objects from kubectl client-side apply"
			if count > 0 && !strings.Contains(d.Summary, warning) {
				t.Fatalf("missing warning: %q", d.Summary)
			}
			if count == 0 && strings.Contains(d.Summary, "takes field ownership") {
				t.Fatalf("unexpected warning: %q", d.Summary)
			}
		}
	}
}
