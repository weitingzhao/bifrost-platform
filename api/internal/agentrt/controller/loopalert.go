package controller

import (
	"context"
	"errors"
	"regexp"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// A loop that keeps failing the same way is told (TD-411: STG's facts pass
// failed every 15 seconds for two hours on a check constraint the database's
// schema lacked, a warning each time and nothing else). After
// store.FailuresToAlert failures in a row with the same error the loop records
// an alert the Owner sees (store.AlertFailingLoop: a policy_alert, listed by
// Progress, said on Work and System); once it passes again, a note
// (store.NoteLoopPasses). Errors are the same when they differ only in
// numbers and ids. An alert that cannot be recorded is tried again on the
// next failure.

// errPanicked is a pass that panicked, as a failure of its loop.
var errPanicked = errors.New("a pass panicked")

// failureRun is a loop's failures in a row: how many, the last error's key,
// and whether the run was alerted.
type failureRun struct {
	n       int
	key     string
	alerted bool
}

// varying are the parts of an error that change from one failure to the
// next of the same kind: ULIDs, hex digests and ids, numbers.
var varying = regexp.MustCompile(`[0-9A-HJKMNP-TV-Z]{26}|[0-9a-f]{8,}|[0-9]+`)

func errorKey(err error) string { return varying.ReplaceAllString(err.Error(), "#") }

// track counts a pass of loop name: err nil passed.
func (c *Controller) track(ctx context.Context, name string, run *failureRun, err error) {
	if err == nil {
		if run.alerted {
			if nerr := store.NoteLoopPasses(ctx, c.DB, name, run.n); nerr != nil {
				c.logf("%s: passes again, and that could not be recorded: %v", name, nerr)
			}
		}
		*run = failureRun{}
		return
	}
	if key := errorKey(err); key != run.key {
		*run = failureRun{key: key}
	}
	run.n++
	if run.n < store.FailuresToAlert || run.alerted {
		return
	}
	if aerr := store.AlertFailingLoop(ctx, c.DB, name, run.n, err.Error()); aerr != nil {
		c.logf("%s: failed %d times in a row; the alert could not be recorded: %v", name, run.n, aerr)
		return
	}
	run.alerted = true
	c.logf("%s: failed %d times in a row the same way; alerted (policy_alert controller_failing)", name, run.n)
}
