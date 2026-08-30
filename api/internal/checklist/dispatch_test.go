package checklist

import (
	"strings"
	"testing"
	"time"
)

func TestGateForCapability(t *testing.T) {
	if GateForCapability(FixFullAuto) != "auto" {
		t.Fatal("full_auto → auto")
	}
	if GateForCapability(FixSemiAuto) != "queue" {
		t.Fatal("semi_auto → queue")
	}
	if GateForCapability(FixManual) != "notify" {
		t.Fatal("manual → notify")
	}
	if GateForCapability(FixObserve) != "notify" {
		t.Fatal("observe → notify")
	}
}

func TestPlanActionsD10SkipIB(t *testing.T) {
	actions := PlanActions([]ItemSignal{
		{ItemID: "ib-feed", Signal: SignalFail, Detail: "down"},
		{ItemID: "failing-pods", Signal: SignalFail, Detail: "3 pods"},
	}, nil, 0)
	var ib, pods *DispatchAction
	for i := range actions {
		if actions[i].ItemID == "ib-feed" {
			ib = &actions[i]
		}
		if actions[i].ItemID == "failing-pods" {
			pods = &actions[i]
		}
	}
	if ib == nil || !ib.SkippedD10 || ib.Gate != "skip" {
		t.Fatalf("ib-feed must skip D10, got %+v", ib)
	}
	if pods == nil || pods.Gate != "auto" {
		t.Fatalf("failing-pods full_auto → auto, got %+v", pods)
	}
}

func TestPlanActionsConcurrentLimit(t *testing.T) {
	actions := PlanActions([]ItemSignal{
		{ItemID: "failing-pods", Signal: SignalFail},
		{ItemID: "redis", Signal: SignalFail},
	}, nil, 0)
	auto := 0
	queue := 0
	for _, a := range actions {
		if a.Gate == "auto" {
			auto++
		}
		if a.Gate == "queue" {
			queue++
		}
	}
	if auto != 1 {
		t.Fatalf("expected 1 auto, got %d", auto)
	}
	if queue != 1 {
		t.Fatalf("expected 1 demoted to queue, got %d", queue)
	}
	var demoted *DispatchAction
	for i := range actions {
		if actions[i].Gate == "queue" {
			demoted = &actions[i]
			break
		}
	}
	if demoted == nil || !strings.Contains(strings.ToLower(demoted.Detail), "concurrent auto") {
		t.Fatalf("demoted queue detail should mention concurrent auto, got %+v", demoted)
	}
}

func TestPlanActionsDedup(t *testing.T) {
	recent := []DispatchAction{{
		ItemID: "failing-pods", Gate: "auto", At: time.Now().UTC().Format(time.RFC3339),
	}}
	actions := PlanActions([]ItemSignal{
		{ItemID: "failing-pods", Signal: SignalFail},
	}, recent, 0)
	if len(actions) != 1 || actions[0].Gate != "skip" {
		t.Fatalf("expected dedup skip, got %+v", actions)
	}
}

func TestCatalogHas22Items(t *testing.T) {
	if len(CatalogItems) != 22 {
		t.Fatalf("expected 22 catalog items, got %d", len(CatalogItems))
	}
	if _, ok := ItemByID("db-backup-fresh"); !ok {
		t.Fatal("db-backup-fresh missing from catalog")
	}
	for _, id := range []string{"market-batch-sla", "flex-tokens-secret", "research-batch-sla"} {
		if _, ok := ItemByID(id); !ok {
			t.Fatalf("%s missing from catalog", id)
		}
	}
}
