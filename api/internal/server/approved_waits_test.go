package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// #524, 2026-11-14 00:32Z: what an approved release is answered while another
// run holds the window (delivery.approvedRunWaits) must be a refusal the
// approval's executor queues again, or the Owner's approval is spent on a
// 28 ms failure. This is the exact shape of that answer.
func TestTheApprovedRunsWindowRefusalIsRetried524(t *testing.T) {
	msg := "REFUSED: release window held by another release (who=bifrost-deliver-platform-1794616265 what=bifrost-platform,bifrost-ui (held since=2026-11-14T00:31:08Z expires=2026-11-14T00:37:28Z)); this approved run starts when the window is free"
	_, err := interpretAction(http.StatusBadGateway, []byte(`{"ok":false,"message":"`+msg+`"}`))
	if !actions.IsTransient(err) {
		t.Fatalf("not queued again: %v", err)
	}
}

// #524 the other way round: an approved PROD release waits for the window
// and tries again at intervals of 30 seconds to 5 minutes; the loop passes
// every 30 seconds and would start the next STG delivery first each time
// main moved. While such a request can still run, the loop starts nothing
// and says which request it gives way to. A request nobody approved yet, one
// that ran, and one past its hour do not hold the loop.
func TestTheLoopGivesWayToAnApprovedRelease524(t *testing.T) {
	now := time.Date(2026, 11, 14, 0, 33, 0, 0, time.UTC)
	card := func(n int, status, action, pipeline string, deadline time.Time) approvals.Approval {
		return approvals.Approval{Number: n, Status: status, Action: action, Params: map[string]any{"name": pipeline},
			DecidedAt: now.Add(-time.Duration(600-n) * time.Second), Execution: &approvals.Execution{Deadline: deadline}}
	}
	hour := now.Add(59 * time.Minute)
	for name, tc := range map[string]struct {
		items []approvals.Approval
		want  string
	}{
		"524 approved, waiting for the window": {[]approvals.Approval{card(524, approvals.StatusApproved, "start_pipeline_run", "bifrost-deliver-platform-prod", hour)}, "#524 bifrost-deliver-platform-prod"},
		"the older of two goes first":          {[]approvals.Approval{card(526, approvals.StatusApproved, "start_pipeline_run", "bifrost-deliver-prod", hour), card(524, approvals.StatusRunning, "start_pipeline_run", "bifrost-deliver-platform-prod", hour)}, "#524 bifrost-deliver-platform-prod"},
		"not approved yet":                     {[]approvals.Approval{card(524, approvals.StatusPending, "start_pipeline_run", "bifrost-deliver-platform-prod", hour)}, ""},
		"already failed (524 as it was)":       {[]approvals.Approval{card(524, approvals.StatusFailed, "start_pipeline_run", "bifrost-deliver-platform-prod", hour)}, ""},
		"executed":                             {[]approvals.Approval{card(523, approvals.StatusExecuted, "start_pipeline_run", "bifrost-deliver-platform-prod", hour)}, ""},
		"past its hour":                        {[]approvals.Approval{card(524, approvals.StatusApproved, "start_pipeline_run", "bifrost-deliver-platform-prod", now.Add(-time.Minute))}, ""},
		"an approved CI run":                   {[]approvals.Approval{card(525, approvals.StatusApproved, "start_pipeline_run", "bifrost-ci-platform", hour)}, ""},
		"another approved action":              {[]approvals.Approval{card(519, approvals.StatusApproved, "apply_manifest", "bifrost-deliver-platform-prod", hour)}, ""},
	} {
		if got := approvedReleaseWaiting(tc.items, now); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}

	starts := 0
	waiting := "#524 bifrost-deliver-platform-prod"
	w := &headWorld{
		start: func(_ context.Context, _, pipeline, _ string, _ map[string]string) (delivery.PipelineRunView, string, error) {
			starts++
			return delivery.PipelineRunView{Name: pipeline + "-1"}, "", nil
		},
		approved: func() (string, error) { return waiting, nil },
	}
	run, wait, err := w.Start(context.Background(), "bifrost-deliver-platform", map[string]string{"revision": "r"})
	if err != nil || run != "" || wait != "an approved release goes first: #524 bifrost-deliver-platform-prod" || starts != 0 {
		t.Fatalf("with 524 waiting: %q wait=%q %v, %d starts", run, wait, err, starts)
	}
	waiting = ""
	if run, wait, err := w.Start(context.Background(), "bifrost-deliver-platform", map[string]string{"revision": "r"}); err != nil || run == "" || wait != "" {
		t.Fatalf("after 524 ran: %q wait=%q %v", run, wait, err)
	}
}
