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
// and whether this run of that error is alerted; apart from it, whether an
// alert of the loop stands in the database with no note answering it (open,
// known once read or written: review 466 A-1, an alert outlives a change of
// error, a note that could not be written and a restart).
type failureRun struct {
	n       int
	key     string
	alerted bool
	open    bool
	known   bool
}

// varying are the parts of an error that change from one failure to the
// next of the same kind: ULIDs, hex digests and ids, numbers.
var varying = regexp.MustCompile(`[0-9A-HJKMNP-TV-Z]{26}|[0-9a-f]{8,}|[0-9]+`)

func errorKey(err error) string { return varying.ReplaceAllString(err.Error(), "#") }

// track counts a pass of loop name: err nil passed. A pass after an alert
// writes the note, and until that is written the alert stays open; a loop
// started again reads whether its alert is open at its first pass.
func (c *Controller) track(ctx context.Context, name string, run *failureRun, err error) {
	if err == nil {
		failures := run.n
		run.n, run.key, run.alerted = 0, "", false
		if !run.known {
			open, rerr := store.LoopAlertOpen(ctx, c.DB, name)
			if rerr != nil {
				c.logf("%s: whether its alert is answered could not be read: %v", name, rerr)
				return
			}
			run.open, run.known = open, true
		}
		if !run.open {
			return
		}
		if nerr := store.NoteLoopPasses(ctx, c.DB, name, failures); nerr != nil {
			c.logf("%s: passes again, and that could not be recorded: %v", name, nerr)
			return
		}
		run.open = false
		return
	}
	if key := errorKey(err); key != run.key {
		run.n, run.key, run.alerted = 0, key, false
	}
	run.n++
	if run.n < store.FailuresToAlert || run.alerted {
		return
	}
	if aerr := store.AlertFailingLoop(ctx, c.DB, name, run.n, err.Error()); aerr != nil {
		c.logf("%s: failed %d times in a row; the alert could not be recorded: %v", name, run.n, aerr)
		return
	}
	run.alerted, run.open, run.known = true, true, true
	c.logf("%s: failed %d times in a row the same way; alerted (policy_alert controller_failing)", name, run.n)
}
