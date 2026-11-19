package approvals

import (
	"context"
	"fmt"
	"time"
)

// A card for apply_manifest ends when the apply run ends, not when it starts
// (origin: #607 — the apply run failed because the commit was not reachable
// from main, and the card said executed; #604 and #608 succeeded and said the
// same, which only meant "started"). The platform runner calls this after the
// action returned, keeps the lease renewed meanwhile, and the context ends at
// the card's deadline.

// ApplyFollowEvery is how often the apply run is read.
const ApplyFollowEvery = 5 * time.Second

// ApplyOutcome is what the follow-up reads: the work service's reading of one
// apply run.
type ApplyOutcome func(ctx context.Context, name string) (done, ok bool, why string, err error)

// ApplyFollowMax bounds how long the approving request waits for the run: the
// platform runner runs inside the Owner's approve request, so a run that is
// still going after this is left going and the card says so ("started").
const ApplyFollowMax = 45 * time.Second

// FollowApplyRun waits, up to ApplyFollowMax or the context's end, for the
// apply run an apply_manifest card started and returns the outcome the card
// should show: the run succeeded (executed), the run failed (an error with the
// run's words), the run was still going (executed, marked "started"), or the
// run could not be read to the end (an error). Any other action passes through.
func FollowApplyRun(ctx context.Context, rec Approval, result any, outcome ApplyOutcome, every time.Duration) (any, error) {
	return followApplyRun(ctx, rec, result, outcome, every, ApplyFollowMax)
}

func followApplyRun(ctx context.Context, rec Approval, result any, outcome ApplyOutcome, every, max time.Duration) (any, error) {
	if rec.Action != "apply_manifest" {
		return result, nil
	}
	out, _ := result.(map[string]any)
	run, _ := out["apply_run"].(string)
	if run == "" {
		return result, nil
	}
	annotate := func(status string) any {
		merged := map[string]any{}
		for k, v := range out {
			merged[k] = v
		}
		merged["apply_run_status"] = status
		return merged
	}
	ctx, cancel := context.WithTimeout(ctx, max)
	defer cancel()
	var readErr error
	for {
		done, ok, why, err := outcome(ctx, run)
		readErr = err
		switch {
		case err == nil && done && ok:
			return annotate("Succeeded"), nil
		case err == nil && done:
			return result, fmt.Errorf("apply run %s failed: %s", run, why)
		}
		// Still running, or a read that failed: try again until the bound.
		select {
		case <-ctx.Done():
			if readErr != nil {
				return result, fmt.Errorf("apply run %s could not be read: %v", run, readErr)
			}
			return annotate(fmt.Sprintf("started: the run was still going when the card closed (%s)", max)), nil
		case <-time.After(every):
		}
	}
}
