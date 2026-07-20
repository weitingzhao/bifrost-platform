package operatequeue

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentgovernance"
)

func TestMain(m *testing.M) {
	// Load agent-tasks.yaml from repo config for TaskByID validation.
	root := filepath.Join("..", "..", "..", "config")
	_ = agentgovernance.InitAgentTaskCatalog(root)
	m.Run()
}

func baseItem(mut func(*Item)) Item {
	item := Item{
		ID:          "q-1",
		ProgramID:   "daily-ops-checklist",
		Title:       "Checklist · Redis reachable",
		Status:      StatusOpen,
		Source:      SourceChecklistDispatch,
		AgentTaskID: "daily-ops-checklist-run",
		HandoffKind: HandoffOneOff,
		RiskLevel:   RiskMedium,
		CreatedAt:   time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339),
		Description: "semi_auto handoff for item redis\n\nconnection refused",
		AcceptanceCriteria: []string{
			"Checklist item redis returns ok",
		},
	}
	if mut != nil {
		mut(&item)
	}
	return item
}

func TestClassifyStaleWhenChecklistItemOK(t *testing.T) {
	item := baseItem(nil)
	cr := ClassifyItem(ClassifyInput{
		Item: item,
		Evidence: BundleFromSignals([]EvidenceSignal{
			{ItemID: "redis", Signal: "ok", Detail: "pong"},
		}, time.Now().UTC()),
	})
	if cr.Verdict != VerdictStale {
		t.Fatalf("verdict=%s reason=%s", cr.Verdict, cr.Reason)
	}
	if cr.ChecklistItemID != "redis" {
		t.Fatalf("checklist id=%q", cr.ChecklistItemID)
	}
}

func TestClassifyStillNeededWhenNOGO(t *testing.T) {
	item := baseItem(func(i *Item) {
		i.AgentTaskID = "cluster-auto"
	})
	cr := ClassifyItem(ClassifyInput{
		Item: item,
		Evidence: BundleFromSignals([]EvidenceSignal{
			{ItemID: "redis", Signal: "fail", Detail: "down"},
			{ItemID: "postgres", Signal: "ok"},
		}, time.Now().UTC()),
	})
	if cr.Verdict != VerdictStillNeeded {
		t.Fatalf("verdict=%s reason=%s scope=%s fleet=%s", cr.Verdict, cr.Reason, cr.FixScope, cr.FleetSignal)
	}
	if cr.FixScope != "cluster_issues_full_auto" {
		t.Fatalf("fixScope=%q", cr.FixScope)
	}
}

func TestClassifyNeedsDecisionRecurringSetup(t *testing.T) {
	item := baseItem(func(i *Item) {
		i.HandoffKind = HandoffRecurringSetup
		i.Source = SourceManual
		i.Description = ""
	})
	cr := ClassifyItem(ClassifyInput{
		Item: item,
		Evidence: BundleFromSignals([]EvidenceSignal{
			{ItemID: "redis", Signal: "ok"},
		}, time.Now().UTC()),
	})
	if cr.Verdict != VerdictNeedsDecision {
		t.Fatalf("verdict=%s", cr.Verdict)
	}
}

func TestClassifyNeedsDecisionD10IBFeed(t *testing.T) {
	item := baseItem(func(i *Item) {
		i.Description = "semi_auto handoff for item ib-feed\n\nobserve only"
		i.AcceptanceCriteria = []string{"Checklist item ib-feed returns ok"}
		i.AgentTaskID = "daily-ops-checklist-run"
	})
	cr := ClassifyItem(ClassifyInput{
		Item: item,
		Evidence: BundleFromSignals([]EvidenceSignal{
			{ItemID: "ib-feed", Signal: "fail", Detail: "gateway down"},
		}, time.Now().UTC()),
	})
	if cr.Verdict != VerdictNeedsDecision {
		t.Fatalf("verdict=%s reason=%s", cr.Verdict, cr.Reason)
	}
	if cr.Reason == "" || cr.Reason[:3] != "D10" {
		t.Fatalf("expected D10 reason, got %q", cr.Reason)
	}
}

func TestClassifyNeedsDecisionHighRisk(t *testing.T) {
	item := baseItem(func(i *Item) {
		i.RiskLevel = RiskHigh
		i.AgentTaskID = "cluster-auto"
	})
	cr := ClassifyItem(ClassifyInput{
		Item: item,
		Evidence: BundleFromSignals([]EvidenceSignal{
			{ItemID: "redis", Signal: "fail"},
		}, time.Now().UTC()),
	})
	if cr.Verdict != VerdictNeedsDecision {
		t.Fatalf("verdict=%s", cr.Verdict)
	}
}

func TestClassifyInProgress(t *testing.T) {
	item := baseItem(func(i *Item) {
		i.ExecutionJobID = "job-1"
		i.AgentTaskID = "cluster-auto"
	})
	cr := ClassifyItem(ClassifyInput{
		Item: item,
		Evidence: BundleFromSignals([]EvidenceSignal{
			{ItemID: "redis", Signal: "fail"},
		}, time.Now().UTC()),
		JobRunning: func(id string) bool { return id == "job-1" },
	})
	if cr.Verdict != VerdictInProgress {
		t.Fatalf("verdict=%s", cr.Verdict)
	}
}

func TestClassifyHeld(t *testing.T) {
	item := baseItem(nil)
	until := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	cr := ClassifyItem(ClassifyInput{
		Item:      item,
		Evidence:  BundleFromSignals([]EvidenceSignal{{ItemID: "redis", Signal: "fail"}}, time.Now().UTC()),
		HeldUntil: until,
	})
	if cr.Verdict != VerdictHeld {
		t.Fatalf("verdict=%s", cr.Verdict)
	}
}

func TestClassifyStaleByAgeAndNominal(t *testing.T) {
	item := baseItem(func(i *Item) {
		i.Source = SourceManual
		i.Description = ""
		i.AcceptanceCriteria = nil
		i.AgentTaskID = "cluster-auto"
		i.CreatedAt = time.Now().UTC().Add(-50 * time.Hour).Format(time.RFC3339)
	})
	// No scope-related fail rows → QuietNominal true, but scope may be GO via overall nominal
	ev := BundleFromSignals([]EvidenceSignal{
		{ItemID: "platform-api", Signal: "ok"},
		{ItemID: "redis", Signal: "ok"},
	}, time.Now().UTC())
	cr := ClassifyItem(ClassifyInput{Item: item, Evidence: ev})
	// Fleet for cluster_issues is GO (redis ok) → STALE via fleet GO first
	if cr.Verdict != VerdictStale {
		t.Fatalf("verdict=%s reason=%s fleet=%s", cr.Verdict, cr.Reason, cr.FleetSignal)
	}
}

func TestClassifyNeedsDecisionMissingTask(t *testing.T) {
	item := baseItem(func(i *Item) {
		i.AgentTaskID = ""
		i.Source = SourceManual
		i.Description = ""
		i.AcceptanceCriteria = nil
	})
	cr := ClassifyItem(ClassifyInput{
		Item:     item,
		Evidence: BundleFromSignals([]EvidenceSignal{{ItemID: "redis", Signal: "fail"}}, time.Now().UTC()),
	})
	if cr.Verdict != VerdictNeedsDecision {
		t.Fatalf("verdict=%s", cr.Verdict)
	}
}

func TestExtractChecklistItemID(t *testing.T) {
	item := baseItem(nil)
	if id := ExtractChecklistItemID(item); id != "redis" {
		t.Fatalf("got %q", id)
	}
}

func TestFleetProbeForScope(t *testing.T) {
	p, ok := FleetProbeForScope("deliver-stg-recover")
	if !ok || p.Role != "rocket" || p.Env != "stg" {
		t.Fatalf("got %+v ok=%v", p, ok)
	}
}

func TestBuildDecisionBriefMarkdown(t *testing.T) {
	item := baseItem(nil)
	cr := ClassifyResult{
		Verdict:     VerdictNeedsDecision,
		Reason:      "risk_level=high",
		FixScope:    "cluster_issues_full_auto",
		FleetSignal: "NO-GO",
		FleetDetail: "satellite:prod (redis=fail)",
	}
	b := BuildDecisionBrief(item, cr, time.Now().UTC())
	if b.FullBrief == "" || b.Suggestion == "" {
		t.Fatalf("brief incomplete: %+v", b)
	}
	if b.ItemID != item.ID {
		t.Fatalf("item id mismatch")
	}
}
