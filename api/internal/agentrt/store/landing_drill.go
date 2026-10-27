package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// ClaimLandingDrill claims one task under the authoritative repository stream
// lock. No environment flag from a task can enable a production operation.
// The caller orders tasks by their acceptance event time (CUT §2.5.1).
func ClaimLandingDrill(ctx context.Context, db DB, src LandingSource, task string, now time.Time) (op string, decision landing.Decision, err error) {
	err = InTx(ctx, db, func(tx pgx.Tx) error {
		// The mission before the stream (the kernel's lock order): a close
		// holding it decides first and the facts below then read it done, or
		// this claim commits first and CloseMissions sees it (landingInFlight).
		if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.mission m JOIN agentrt.task t USING (mission_id) WHERE t.task_id = $1 FOR SHARE OF m`, task); err != nil {
			return err
		}
		if err := lockStream(ctx, tx, "landing:bifrost-platform"); err != nil {
			return err
		}
		state, err := LandingState(ctx, tx, "landing:bifrost-platform")
		if err != nil {
			return err
		}
		first, n := landing.RootBudget(state, task)
		if landing.Bound(first, n, now) {
			decision = landing.Decision{Reason: "bound"}
			return failDiscardedBound(ctx, tx, state, task, now)
		}
		f, err := LandingFacts(ctx, tx, src, "stg", "dry_run", task, "", "", now)
		if err != nil {
			return err
		}
		decision = landing.EligibleToClaim(f, now)
		if !decision.Allowed {
			return nil
		}
		// Completed rehearsals are not repeated by the STG scanner. They
		// remain non-blocking to production's common eligibility reader.
		for _, o := range state.Operations {
			if o.Task == task && o.Phase == "rehearsed" {
				decision = landing.Decision{Reason: "rehearsed"}
				return nil
			}
		}
		// A parked operation is resumed, not replaced with a duplicate claim.
		for _, o := range state.Operations {
			if o.Task == task && !o.Terminal() {
				decision = landing.Decision{Reason: "in_progress"}
				return nil
			}
		}
		op = fmt.Sprintf("land:%s:%d", task, n+1)
		_, err = Append(ctx, tx, Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", MissionID: f.Mission, TaskID: task, Body: map[string]any{
			"text": "STG landing drill claimed", "landing": map[string]any{"v": landing.Version, "kind": "claimed", "op": op, "task": task, "attempt": f.Attempt, "A": f.A, "rule_version": f.Rule.Version},
		}})
		return err
	})
	return
}
