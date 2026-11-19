package approvalreadings

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

// ApprovedReleaseWaiting names the oldest approved start of a delivery
// pipeline that has not run yet and can still run (its execution deadline is
// not past): "#524 bifrost-deliver-platform-prod". A request still waiting
// for its approval is not one: nobody knows when, or whether, it is approved.
func ApprovedReleaseWaiting(items []approvals.Approval, now time.Time) string {
	var first *approvals.Approval
	for i := range items {
		a := &items[i]
		if a.Action != "start_pipeline_run" || (a.Status != approvals.StatusApproved && a.Status != approvals.StatusRunning) {
			continue
		}
		name, _ := a.Params["name"].(string)
		if !strings.HasPrefix(name, "bifrost-deliver-") {
			continue
		}
		if a.Execution != nil && !a.Execution.Deadline.IsZero() && now.After(a.Execution.Deadline) {
			continue
		}
		if first == nil || a.DecidedAt.Before(first.DecidedAt) {
			first = a
		}
	}
	if first == nil {
		return ""
	}
	name, _ := first.Params["name"].(string)
	return fmt.Sprintf("#%d %s", first.Number, name)
}

// OpenReleaseCards names each approval of the action on a run of the pipeline
// that is not finished: waiting for its decision, or approved and not yet run
// to its end.
func OpenReleaseCards(items []approvals.Approval, action, pipeline string) []string {
	var out []string
	for _, a := range items {
		if a.Action != action {
			continue
		}
		if name, _ := a.Params["name"].(string); name != pipeline {
			continue
		}
		switch a.Status {
		case approvals.StatusPending, approvals.StatusApproved, approvals.StatusRunning:
			out = append(out, fmt.Sprintf("#%d %s", a.Number, a.Status))
		}
	}
	sort.Strings(out)
	return out
}
