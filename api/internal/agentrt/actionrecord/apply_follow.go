package actionrecord

import (
	"context"
	"fmt"
	"time"
)

// A direct apply_manifest call returns when its apply run starts. Its record
// was written then, saying done with no check, though the run could still
// fail (2026-11-20, CI lane step 7: every apply record since the record began
// had an empty check_result). The record is append only, so it waits for the
// run instead: the server writes it once the run has ended, with the run's
// outcome and a check that says what was read.

// RunOutcome reads one apply run: whether it ended, whether it succeeded, and
// the run's words when it did not.
type RunOutcome func(ctx context.Context, name string) (done, ok bool, why string, err error)

// AfterApplyRun follows the apply run until it ends, max passes or ctx ends,
// reading it every `every`, and returns rec with the outcome and check the run
// supports: done and "apply run X succeeded", failed and the run's words, or
// done with a check that says the run was still going or could not be read.
func AfterApplyRun(ctx context.Context, rec Record, run string, outcome RunOutcome, every, max time.Duration) Record {
	if run == "" || outcome == nil || rec.Outcome != OutcomeDone {
		return rec
	}
	ctx, cancel := context.WithTimeout(ctx, max)
	defer cancel()
	var readErr error
	for {
		done, ok, why, err := outcome(ctx, run)
		readErr = err
		if err == nil && done {
			if ok {
				rec.CheckResult = fmt.Sprintf("apply run %s succeeded", run)
			} else {
				rec.Outcome = OutcomeFailed
				rec.CheckResult = cut(fmt.Sprintf("apply run %s failed: %s", run, why))
			}
			return rec
		}
		select {
		case <-ctx.Done():
			if readErr != nil {
				rec.CheckResult = cut(fmt.Sprintf("apply run %s could not be read to its end: %v", run, readErr))
			} else {
				rec.CheckResult = fmt.Sprintf("apply run %s still running after %s; not followed further", run, max)
			}
			return rec
		case <-time.After(every):
		}
	}
}

// cut keeps a check within the column's 2000 characters.
func cut(s string) string {
	if len(s) > 1900 {
		return s[:1900] + "…"
	}
	return s
}
