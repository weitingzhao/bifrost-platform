package checklistview

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/checklist"
	husbandry "github.com/weitingzhao/bifrost-platform/api/internal/tenant/husbandry"
)

func TestProbeHusbandrySignals_marketDueIsOK(t *testing.T) {
	snap := laneSnap("market_batch", "due", "", "schedule=due")
	got := byItem(Signals(snap), "market-batch-sla")
	if got.Signal != SignalOK {
		t.Fatalf("due → ok, got %s (%s)", got.Signal, got.Detail)
	}
}

func TestProbeHusbandrySignals_marketMissedFails(t *testing.T) {
	snap := laneSnap("market_batch", "missed", "", "schedule=missed")
	got := byItem(Signals(snap), "market-batch-sla")
	if got.Signal != SignalFail {
		t.Fatalf("missed → fail, got %s", got.Signal)
	}
}

func TestProbeHusbandrySignals_flexNoneFails(t *testing.T) {
	snap := laneSnap("flex_batch", "degraded", "none", "token source=none")
	got := byItem(Signals(snap), "flex-tokens-secret")
	if got.Signal != SignalFail {
		t.Fatalf("source=none → fail, got %s", got.Signal)
	}
}

func TestProbeHusbandrySignals_researchMissedFails(t *testing.T) {
	snap := laneSnap("research_olap", "missed", "", "no runs")
	got := byItem(Signals(snap), "research-batch-sla")
	if got.Signal != SignalFail {
		t.Fatalf("research missed → fail, got %s", got.Signal)
	}
}

func TestProbeHusbandrySignals_flexHealthyOK(t *testing.T) {
	snap := laneSnap("flex_batch", "healthy", "secret", "source=secret")
	got := byItem(Signals(snap), "flex-tokens-secret")
	if got.Signal != SignalOK {
		t.Fatalf("flex healthy → ok, got %s", got.Signal)
	}
}

func laneSnap(id, verdict, source, detail string) husbandry.Snapshot {
	return husbandry.Snapshot{
		Lanes: []husbandry.LaneView{
			{ID: "market_batch", Label: "Market", Verdict: "healthy", Detail: "ok"},
			{ID: "flex_batch", Label: "Flex", Verdict: "healthy", Detail: "ok", Source: "secret"},
			{ID: "research_olap", Label: "Research", Verdict: "healthy", Detail: "ok"},
			{ID: id, Label: id, Verdict: verdict, Detail: detail, Source: source},
		},
	}
}

func byItem(sigs []checklist.ItemSignal, id string) checklist.ItemSignal {
	for _, s := range sigs {
		if s.ItemID == id {
			return s
		}
	}
	return checklist.ItemSignal{}
}
