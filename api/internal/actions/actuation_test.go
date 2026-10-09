package actions

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
)

func loadPolicy(t *testing.T) *actuationpolicy.Policy {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	p, err := actuationpolicy.Load(filepath.Join(filepath.Dir(file), "..", "..", "..", "config", "actuation-policy.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestApplyManifestTiers(t *testing.T) {
	p := loadPolicy(t)
	SetActuationPolicy(p)
	t.Cleanup(func() {
		SetActuationPolicy(nil)
		SetPlanLookup(nil)
	})
	var bNS, cNS string
	for ns, tier := range p.Apply.Namespaces {
		if tier == "B" && bNS == "" {
			bNS = ns
		}
		if tier == "C" && cNS == "" {
			cNS = ns
		}
	}
	SetPlanLookup(func(_ context.Context, id string) (PlanSummary, error) {
		switch id {
		case "b":
			return PlanSummary{Ready: true, Namespaces: []string{bNS}}, nil
		case "c":
			return PlanSummary{Ready: true, Namespaces: []string{cNS}}, nil
		case "daemon":
			return PlanSummary{Ready: true, Namespaces: []string{bNS}, Daemon: true}, nil
		default:
			return PlanSummary{}, errors.New("missing")
		}
	})
	a, _ := ByID("apply_manifest")
	if got := a.TierOf(context.Background(), map[string]any{"plan_id": "b"}); got != TierB {
		t.Fatalf("dev/stg plan tier %s, want B", got)
	}
	if got := a.TierOf(context.Background(), map[string]any{"plan_id": "c"}); got != TierC {
		t.Fatalf("higher plan tier %s, want C", got)
	}
	if got := a.TierOf(context.Background(), map[string]any{"plan_id": "daemon"}); got != TierX {
		t.Fatalf("daemon plan tier %s, want X", got)
	}
}

func TestProbeAndJobClassify(t *testing.T) {
	p := loadPolicy(t)
	SetActuationPolicy(p)
	t.Cleanup(func() { SetActuationPolicy(nil) })
	var dataNS, otherNS, denied string
	for ns, tier := range p.Probe.Namespaces {
		if tier == "C" && dataNS == "" {
			dataNS = ns
		}
		if tier == "B" && otherNS == "" {
			otherNS = ns
		}
	}
	if len(p.Jobs.Deny) > 0 {
		denied = p.Jobs.Deny[0]
	}
	image := "registry.example/" + p.Probe.Images[0] + "1"
	probe, _ := ByID("run_probe_pod")
	if got := probe.TierOf(context.Background(), map[string]any{"namespace": dataNS, "image": image}); got != TierC {
		t.Fatalf("data probe tier %s, want C", got)
	}
	if got := probe.TierOf(context.Background(), map[string]any{"namespace": otherNS, "image": "library/not-listed:1"}); got != TierX {
		t.Fatalf("unlisted image tier %s, want X", got)
	}
	allowed := map[string]bool{}
	for _, ns := range p.Probe.EnvFrom {
		allowed[ns] = true
	}
	outside := ""
	for ns := range p.Probe.Namespaces {
		if !allowed[ns] {
			outside = ns
			break
		}
	}
	if outside == "" {
		t.Fatal("no probe namespace without env_from")
	}
	if got := probe.TierOf(context.Background(), map[string]any{"namespace": outside, "image": image, "env_from": "some-deploy"}); got != TierX {
		t.Fatalf("env_from outside the list tier %s, want X", got)
	}
	job, _ := ByID("create_job_from_cronjob")
	if got := job.TierOf(context.Background(), map[string]any{"namespace": denied}); got != TierX {
		t.Fatalf("denied job tier %s, want X", got)
	}
}

func TestOwnerRunDoesNotExecute(t *testing.T) {
	called := false
	SetCommandRunner(func(context.Context, string) error {
		called = true
		return errors.New("must not run")
	})
	t.Cleanup(func() { SetCommandRunner(nil) })
	got, err := ExecuteOwnerRunCommand(context.Background(), map[string]any{"command": "kubectl get ns"})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("command runner was called")
	}
	if got.(map[string]any)["executed_by_platform"] != false {
		t.Fatalf("platform executed the command: %#v", got)
	}
	if got.(map[string]any)["command"] != "kubectl get ns" {
		t.Fatalf("command not recorded: %#v", got)
	}
	if _, ok := got.(map[string]any)["command_sha256"].(string); !ok {
		t.Fatal("missing command hash")
	}
}
