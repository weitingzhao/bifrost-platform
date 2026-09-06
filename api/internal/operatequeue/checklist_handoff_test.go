package operatequeue

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// The real thing eight times over: one market_batch gap, one handoff each run.
func marketBatchRequest() EnqueueRequest {
	return EnqueueRequest{
		ProgramID:   "daily-ops-checklist",
		OperateLane: "troubleshoot",
		Title:       "Checklist · Market batch husbandry SLA",
		Description: "semi_auto handoff for item market-batch-sla\n\nmarket_batch missed: 1 slot(s) missed cron adherence",
		HandoffKind: HandoffOneOff,
		Reason:      "checklist_dispatch",
		AgentTaskID: "daily-ops-checklist-run",
		AcceptanceCriteria: []string{
			"Checklist item market-batch-sla returns ok",
		},
		RiskLevel: RiskMedium,
		Owner:     "ops",
	}
}

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	dir := t.TempDir()
	_ = os.Setenv("PLATFORM_DATA_DIR", filepath.Join(dir, "data"))
	t.Cleanup(func() { _ = os.Unsetenv("PLATFORM_DATA_DIR") })
	return NewHandler(filepath.Join(dir, "config"), actuation.NewAuditLog(""))
}

func TestEnqueueChecklistDispatchDedupesOnOpenQueue(t *testing.T) {
	h := newTestHandler(t)

	first, created, err := h.EnqueueChecklistDispatch(marketBatchRequest())
	if err != nil {
		t.Fatal(err)
	}
	if !created || first.ID == "" {
		t.Fatalf("first enqueue must create an item, got created=%v item=%+v", created, first)
	}

	// A later run of the same unresolved condition must not open a second one,
	// whatever the dispatcher's own snapshot remembers.
	for i := 0; i < 3; i++ {
		again, created, err := h.EnqueueChecklistDispatch(marketBatchRequest())
		if err != nil {
			t.Fatal(err)
		}
		if created {
			t.Fatalf("run %d created a duplicate handoff %s", i+2, again.ID)
		}
		if again.ID != first.ID {
			t.Fatalf("run %d returned %s, want the open item %s", i+2, again.ID, first.ID)
		}
	}

	list, err := h.store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Open) != 1 {
		t.Fatalf("expected exactly 1 open handoff, got %d", len(list.Open))
	}
}

func TestEnqueueChecklistDispatchSeparatesDifferentItems(t *testing.T) {
	h := newTestHandler(t)

	if _, _, err := h.EnqueueChecklistDispatch(marketBatchRequest()); err != nil {
		t.Fatal(err)
	}
	research := marketBatchRequest()
	research.Title = "Checklist · Research batch SLA"
	research.Description = "semi_auto handoff for item research-batch-sla\n\nresearch_olap degraded"
	research.AcceptanceCriteria = []string{"Checklist item research-batch-sla returns ok"}
	if _, created, err := h.EnqueueChecklistDispatch(research); err != nil || !created {
		t.Fatalf("a different checklist item must get its own handoff (created=%v err=%v)", created, err)
	}

	list, _ := h.store.List()
	if len(list.Open) != 2 {
		t.Fatalf("expected 2 open handoffs for 2 conditions, got %d", len(list.Open))
	}
}

func TestRetireRecoveredClosesTheHandoffWhenTheSignalReturns(t *testing.T) {
	h := newTestHandler(t)
	item, _, err := h.EnqueueChecklistDispatch(marketBatchRequest())
	if err != nil {
		t.Fatal(err)
	}

	// A checklist item that is ok again retires its own work item.
	retired := h.RetireRecoveredChecklistHandoffs("market-batch-sla", "market_batch healthy: 14 slots on plan")
	if len(retired) != 1 || retired[0].ID != item.ID {
		t.Fatalf("expected the open handoff retired, got %+v", retired)
	}
	if retired[0].Status != StatusClosed {
		t.Fatalf("retired item must be closed, got %s", retired[0].Status)
	}
	joined := ""
	for _, e := range retired[0].CompletionEvidence {
		joined += e + "\n"
	}
	for _, want := range []string{"dismiss:resolved", "signal=ok", "14 slots on plan"} {
		if !contains(joined, want) {
			t.Fatalf("evidence must record why it closed (missing %q): %s", want, joined)
		}
	}

	list, _ := h.store.List()
	if len(list.Open) != 0 {
		t.Fatalf("expected the queue drained, got %d open", len(list.Open))
	}
	// Idempotent: a later ok run has nothing left to retire.
	if again := h.RetireRecoveredChecklistHandoffs("market-batch-sla", "still ok"); len(again) != 0 {
		t.Fatalf("second retire should be a no-op, got %+v", again)
	}
}

func TestRetireRecoveredLeavesOtherItemsAlone(t *testing.T) {
	h := newTestHandler(t)
	if _, _, err := h.EnqueueChecklistDispatch(marketBatchRequest()); err != nil {
		t.Fatal(err)
	}
	if retired := h.RetireRecoveredChecklistHandoffs("research-batch-sla", "ok"); len(retired) != 0 {
		t.Fatalf("recovery of one item must not close another's handoff, got %+v", retired)
	}
	if retired := h.RetireRecoveredChecklistHandoffs("", "ok"); len(retired) != 0 {
		t.Fatalf("an empty item id must retire nothing, got %+v", retired)
	}
	list, _ := h.store.List()
	if len(list.Open) != 1 {
		t.Fatalf("expected the market_batch handoff still open, got %d", len(list.Open))
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
