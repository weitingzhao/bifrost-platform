package store

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// ClaimLandingDrill claims one task under the authoritative repository stream
// lock. No environment flag from a task can enable a production operation.
// The caller orders tasks by their acceptance event time (CUT §2.5.1).
//
// It reads Git with no transaction open (TD-447 5; STG's pool has two
// connections, and a claim holding one through its fetches stalled the
// runtime on 2026-10-27): a short transaction reads the bindings and settles
// what the database alone decides (bound, rehearsed, in progress); the
// external evidence is read with no lock held; a second transaction takes the
// same locks in the same order, reads the bindings again, claims only if the
// evidence's keys did not move (else "changed": the next pass tries again),
// and judges eligibility on this second read.
func ClaimLandingDrill(ctx context.Context, db DB, src LandingSource, task string, now time.Time) (op string, decision landing.Decision, err error) {
	var first drillBinding
	if err = InTx(ctx, db, func(tx pgx.Tx) error {
		var e error
		first, e = bindDrillClaim(ctx, tx, src, task, now)
		return e
	}); err != nil || first.settled {
		return "", first.decision, err
	}
	facts := first.facts
	if err = landingFactsExternal(ctx, src, &facts, first.deps, ""); err != nil {
		return "", landing.Decision{}, err
	}
	err = InTx(ctx, db, func(tx pgx.Tx) error {
		op = ""
		again, e := bindDrillClaim(ctx, tx, src, task, now)
		decision = again.decision
		if e != nil || again.settled {
			return e
		}
		// The evidence read in step 2 is bound to these keys; if any moved it
		// is stale and nothing is claimed. Everything else eligibility reads
		// (mission, plan, rule, in-flight and daily counts) is taken from
		// this locked read, so a close, a no_merge or a tightened rule since
		// step 1 refuses, and a slot freed meanwhile does not (review 646).
		if !reflect.DeepEqual(evidenceOf(again), evidenceOf(first)) {
			decision = landing.Decision{Reason: "changed"}
			return nil
		}
		current := again.facts
		current.SingleCommit, current.Changes, current.History, current.DependenciesEffective =
			facts.SingleCommit, facts.Changes, facts.History, facts.DependenciesEffective
		facts = current
		decision = landing.EligibleToClaim(facts, now)
		if !decision.Allowed {
			return nil
		}
		op = fmt.Sprintf("land:%s:%d", task, again.claims+1)
		_, e = Append(ctx, tx, Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", MissionID: facts.Mission, TaskID: task, Body: map[string]any{
			"text": "STG landing drill claimed", "landing": map[string]any{"v": landing.Version, "kind": "claimed", "op": op, "task": task, "attempt": facts.Attempt, "A": facts.A, "rule_version": facts.Rule.Version},
		}})
		if e != nil {
			op = ""
		}
		return e
	})
	if err != nil {
		op = ""
	}
	return
}

// drillEvidence is what the external evidence of a claim is read for: the
// result (repository, task, attempt, A, Base) and each dependency's history
// (task, repository, commit, in task order).
type drillEvidence struct {
	repo, task, attempt, a, base string
	deps                         []landingDep
}

func evidenceOf(b drillBinding) drillEvidence {
	return drillEvidence{b.facts.Repo, b.facts.Task, b.facts.Attempt, b.facts.A, b.facts.Base, b.deps}
}

// drillBinding is what one locked read of a drill claim found: either it is
// settled from the database alone (decision says why; a bound may have been
// recorded), or the database half of the facts, the dependencies and how
// many operations the root already has.
type drillBinding struct {
	settled  bool
	decision landing.Decision
	facts    landing.Facts
	deps     []landingDep
	claims   int
}

// bindDrillClaim takes the claim's locks and reads its bindings. The task,
// then its mission, before the stream (the kernel's lock order). The task
// lock keeps it in this mission (a step rebind waits); the mission lock
// orders this claim against a close: the close decides first and the facts
// read the mission done, or this claim commits first and CloseMissions sees
// it (landingHolds).
func bindDrillClaim(ctx context.Context, tx pgx.Tx, src LandingSource, task string, now time.Time) (drillBinding, error) {
	var b drillBinding
	var mission string
	if err := tx.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id = $1 FOR SHARE`, task).Scan(&mission); err != nil {
		return b, err
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.mission WHERE mission_id = $1 FOR SHARE`, mission); err != nil {
		return b, err
	}
	if err := lockStream(ctx, tx, "landing:bifrost-platform"); err != nil {
		return b, err
	}
	state, err := LandingState(ctx, tx, "landing:bifrost-platform")
	if err != nil {
		return b, err
	}
	first, n := landing.RootBudget(state, task)
	b.claims = n
	if landing.Bound(first, n, now) {
		b.settled, b.decision = true, landing.Decision{Reason: "bound"}
		return b, failAwaitingBound(ctx, tx, state, task, now)
	}
	// Completed rehearsals are not repeated by the STG scanner. They
	// remain non-blocking to production's common eligibility reader.
	// A parked operation is resumed, not replaced with a duplicate claim.
	for _, o := range state.Operations {
		if o.Task != task {
			continue
		}
		if o.Phase == "rehearsed" {
			b.settled, b.decision = true, landing.Decision{Reason: "rehearsed"}
			return b, nil
		}
		if !o.Terminal() {
			b.settled, b.decision = true, landing.Decision{Reason: "in_progress"}
			return b, nil
		}
	}
	b.facts, b.deps, err = landingFactsDB(ctx, tx, src, "stg", "dry_run", task, "", now)
	return b, err
}
