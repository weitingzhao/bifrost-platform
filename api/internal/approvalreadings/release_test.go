package approvalreadings

import (
	"reflect"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

// Origin: 2026-11-16, the PROD batch shadow (headrelease/prod_reads.go): the
// open cards of a PROD release are the pending and the approved ones of that
// pipeline's start_pipeline_run, not Trade's pipeline, not an executed one,
// not another action. It was TestProdShadowGatherNamesWhatStandsInTheWay's
// card fixture before the filter moved here (decoupling item 5, A3).
func TestOpenReleaseCardsAreThePendingAndApprovedStartsOfThePipeline(t *testing.T) {
	const prod = "bifrost-deliver-platform-prod"
	cards := []approvals.Approval{
		{Number: 524, Action: "start_pipeline_run", Status: approvals.StatusPending, Params: map[string]any{"name": prod}},
		{Number: 525, Action: "start_pipeline_run", Status: approvals.StatusApproved, Params: map[string]any{"name": prod}},
		{Number: 526, Action: "start_pipeline_run", Status: approvals.StatusPending, Params: map[string]any{"name": "bifrost-deliver-prod"}}, // Trade's
		{Number: 527, Action: "start_pipeline_run", Status: approvals.StatusExecuted, Params: map[string]any{"name": prod}},
		{Number: 528, Action: "apply_manifest", Status: approvals.StatusPending, Params: map[string]any{"name": prod}},
	}
	if got := OpenReleaseCards(cards, "start_pipeline_run", prod); !reflect.DeepEqual(got, []string{"#524 pending", "#525 approved"}) {
		t.Fatalf("open cards: %v", got)
	}
}
