package operatequeue

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

func TestSweepAutoDismissesStale(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	dataDir := filepath.Join(dir, "data")
	_ = os.Setenv("PLATFORM_DATA_DIR", dataDir)
	t.Cleanup(func() { _ = os.Unsetenv("PLATFORM_DATA_DIR") })

	h := NewHandler(configDir, actuation.NewAuditLog(""))
	h.BindEvidenceSource(EvidenceFunc(func() (EvidenceBundle, error) {
		return BundleFromSignals([]EvidenceSignal{
			{ItemID: "redis", Signal: "ok", Detail: "pong"},
		}, time.Now().UTC()), nil
	}))

	item, err := h.store.Add(baseItem(func(i *Item) {
		i.ID = "sweep-stale-1"
	}))
	if err != nil {
		t.Fatal(err)
	}

	resp, err := h.Sweep(SweepRequest{AutoDrain: false})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Dismissed) != 1 || resp.Dismissed[0].ItemID != item.ID {
		t.Fatalf("dismissed=%+v", resp.Dismissed)
	}
	list, err := h.store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Open) != 0 {
		t.Fatalf("expected open cleared, got %d", len(list.Open))
	}
}

func TestSweepCreatesBriefForNeedsDecision(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	_ = os.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Cleanup(func() { _ = os.Unsetenv("PLATFORM_DATA_DIR") })

	h := NewHandler(configDir, actuation.NewAuditLog(""))
	h.BindEvidenceSource(EvidenceFunc(func() (EvidenceBundle, error) {
		return BundleFromSignals([]EvidenceSignal{
			{ItemID: "ib-feed", Signal: "fail"},
		}, time.Now().UTC()), nil
	}))

	_, err := h.store.Add(baseItem(func(i *Item) {
		i.ID = "sweep-d10-1"
		i.Description = "semi_auto handoff for item ib-feed\n\nx"
		i.AcceptanceCriteria = []string{"Checklist item ib-feed returns ok"}
	}))
	if err != nil {
		t.Fatal(err)
	}

	resp, err := h.Sweep(SweepRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Decisions) != 1 {
		t.Fatalf("decisions=%+v dismissed=%+v queued=%+v", resp.Decisions, resp.Dismissed, resp.Queued)
	}
	pending, err := h.briefs.ListPending(time.Now().UTC())
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending briefs=%+v err=%v", pending, err)
	}
}

func TestBriefDecideHoldSkipsSweep(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	_ = os.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Cleanup(func() { _ = os.Unsetenv("PLATFORM_DATA_DIR") })

	h := NewHandler(configDir, actuation.NewAuditLog(""))
	h.BindEvidenceSource(EvidenceFunc(func() (EvidenceBundle, error) {
		return BundleFromSignals([]EvidenceSignal{
			{ItemID: "redis", Signal: "fail"},
		}, time.Now().UTC()), nil
	}))

	item, err := h.store.Add(baseItem(func(i *Item) {
		i.ID = "hold-1"
		i.AgentTaskID = "cluster-auto"
	}))
	if err != nil {
		t.Fatal(err)
	}
	brief, err := h.briefs.UpsertPending(BuildDecisionBrief(item, ClassifyResult{
		Verdict: VerdictNeedsDecision, Reason: "test", FixScope: "cluster_issues_full_auto",
		FleetSignal: "NO-GO",
	}, time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	if _, applyErr := h.briefs.ApplyDecision(brief.ID, DecisionHold); applyErr != nil {
		t.Fatal(applyErr)
	}

	resp, err := h.Sweep(SweepRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Held) != 1 || resp.Held[0].ItemID != item.ID {
		t.Fatalf("held=%+v", resp.Held)
	}
	if len(resp.Queued) != 0 || len(resp.Dismissed) != 0 {
		t.Fatalf("expected hold to skip drain/dismiss: %+v", resp)
	}
}
