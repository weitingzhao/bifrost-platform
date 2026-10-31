package store

import (
	"context"
	"fmt"
	"reflect"
	"slices"
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
//
// A root never claimed ends on the stream when the claim refuses it for good
// (TD-447 4): stale, result, or history with A already in main's history;
// or, read from the database alone, when its claim window has passed since
// its acceptance (bound). That record (land:<root>:1, born failed) is what
// releases its mission (landingHolds); any other refusal leaves it queued for
// the next pass.
func ClaimLandingDrill(ctx context.Context, db DB, src LandingSource, task string, now time.Time) (op string, decision landing.Decision, err error) {
	return ClaimLanding(ctx, db, src, "stg", task, now)
}

// ClaimLanding is ClaimLandingDrill for an environment: "stg" claims for the
// drill (dry_run), "prod" for the live landing (S3); the same three steps.
func ClaimLanding(ctx context.Context, db DB, src LandingSource, env, task string, now time.Time) (op string, decision landing.Decision, err error) {
	var first drillBinding
	if err = InTx(ctx, db, func(tx pgx.Tx) error {
		var e error
		first, e = bindDrillClaim(ctx, tx, src, env, task, now)
		return e
	}); err != nil || first.settled {
		return "", first.decision, err
	}
	facts := first.facts
	if err = landingClaimExternal(ctx, src, &facts, first.deps); err != nil {
		return "", landing.Decision{}, err
	}
	err = InTx(ctx, db, func(tx pgx.Tx) error {
		op = ""
		again, e := bindDrillClaim(ctx, tx, src, env, task, now)
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
		current.DependenciesEffective = current.DependenciesEffective && landingDependenciesAllowed(current, again.deps)
		current.S5Patch = facts.S5Patch
		bindS5ReviewedPatch(&current)
		facts = current
		decision = landing.PreparationDecision(facts, again.at)
		if !decision.Allowed {
			if why, ends := unclaimedEnd(decision, facts); ends && again.claims == 0 {
				_, e = failUnclaimedLocked(ctx, tx, "landing:"+facts.Repo, facts.Mission, task, why, refusalEvidence(facts), decision.Detail)
				return e
			}
			return nil
		}
		// Claim-only priority under the repository lock: awaiting_grant is
		// not Active, and EligibleToClaim is also used by the final gate.
		// Counting waiting grants there would block the very push we favor.
		if env == "prod" {
			waiting, err := grantedLandingWaiting(ctx, tx, facts.Repo)
			if err != nil {
				return err
			}
			if waiting {
				decision = landing.Decision{Reason: "limit"}
				return nil
			}
		}
		op = fmt.Sprintf("land:%s:%d", task, again.claims+1)
		_, e = Append(ctx, tx, Event{Stream: "landing:" + facts.Repo, Type: "note", Actor: "controller", MissionID: facts.Mission, TaskID: task, Body: map[string]any{
			"text": "Landing claimed (" + env + ")", "landing": map[string]any{"v": landing.Version, "kind": "claimed", "op": op, "task": task, "attempt": facts.Attempt, "A": facts.A, "rule_version": facts.Rule.Version},
		}})
		if e == nil && landing.S5AutomaticRoute(facts) {
			_, e = Append(ctx, tx, Event{Stream: "landing:" + facts.Repo, Type: "note", Actor: "controller", MissionID: facts.Mission, TaskID: task, Body: map[string]any{"text": "S5 evidence required before authorization", "landing": map[string]any{"v": landing.S5Version, "kind": "s5_required", "op": op}}})
		}
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
	review                       landing.ReviewBinding
}

func evidenceOf(b drillBinding) drillEvidence {
	return drillEvidence{b.facts.Repo, b.facts.Task, b.facts.Attempt, b.facts.A, b.facts.Base, b.deps, b.facts.S5Review}
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
	at       time.Time // the decision time, read once the locks are held (CUT §9)
}

// bindDrillClaim takes capability and rule shared locks, then the plan
// step, task, mission and repository stream, matching the final gate's
// order without its quota reservation lock. The task
// lock keeps it in this mission (a step rebind waits); the mission lock
// orders this claim against a close: the close decides first and the facts
// read the mission done, or this claim commits first and CloseMissions sees
// it (landingHolds).
func bindDrillClaim(ctx context.Context, tx pgx.Tx, src LandingSource, env, task string, now time.Time) (drillBinding, error) {
	var b drillBinding
	var mission, repo, tenant, step string
	if err := tx.QueryRow(ctx, `SELECT t.mission_id, COALESCE(t.repo,''), m.tenant, COALESCE(t.plan_node_id,'')
      FROM agentrt.task t JOIN agentrt.mission m USING(mission_id) WHERE t.task_id=$1`, task).Scan(&mission, &repo, &tenant, &step); err != nil {
		return b, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, capabilityLock); err != nil {
		return b, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1,hashtext($2))`, ruleLock, "landing:"+tenant); err != nil {
		return b, err
	}
	if err := lockLandingBinding(ctx, tx, task, tenant, step, mission); err != nil {
		return b, err
	}
	if !landing.RepoSupported(repo) {
		b.settled, b.decision = true, landing.Decision{Reason: "repo"}
		return b, nil
	}
	if repo != "bifrost-platform" {
		ready, err := landingRuleIncludes(ctx, tx, tenant, repo)
		if err != nil || !ready {
			b.settled, b.decision = true, landing.Decision{Reason: "rule"}
			return b, err
		}
	}
	stream := "landing:" + repo
	if err := lockStream(ctx, tx, stream); err != nil {
		return b, err
	}
	now = DecisionAt()
	b.at = now
	// A repository stopped by a denied push takes no claim until the Owner
	// resumes it (B6); nothing is recorded.
	if stopped, err := repoStopped(ctx, tx, stream); err != nil || stopped {
		b.settled, b.decision = true, landing.Decision{Reason: "stopped"}
		return b, err
	}
	state, err := LandingState(ctx, tx, stream)
	if err != nil {
		return b, err
	}
	// PROD claims only after its live mode is recorded, under this lock
	// (CUT §11.8 v3 item 1): the fence and the claim agree.
	if env == "prod" && state.LiveAt.IsZero() {
		b.settled, b.decision = true, landing.Decision{Reason: "mode"}
		return b, nil
	}
	_, n := landing.RootBudget(state, task)
	b.claims = n
	if why := landing.RootBound(state, task, now, true); why != "" {
		b.settled, b.decision = true, landing.Decision{Reason: why}
		return b, failAwaitingBound(ctx, tx, state, task, now, repo)
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
	mode := "dry_run"
	if env == "prod" {
		mode = "live"
	}
	b.facts, b.deps, err = landingFactsDB(ctx, tx, src, env, mode, task, "", now)
	if err == nil && repo == "bifrost-trade-infra" {
		if b.facts.Rule == nil || b.facts.Rule.Landing == nil {
			b.settled, b.decision = true, landing.Decision{Reason: "rule"}
			return b, nil
		}
		if _, ok := b.facts.Rule.Landing.Repo(repo); !ok {
			b.settled, b.decision = true, landing.Decision{Reason: "rule"}
			return b, nil
		}
	}
	if err != nil || n > 0 || !inHandoff(b.facts) || now.Before(b.facts.AcceptedAt.Add(landing.ClaimWindow)) {
		return b, err
	}
	// Never claimed and its window is over: the loop never got to it (a
	// controller down, a repository busy for six hours). Recorded, not read
	// from Git.
	b.settled, b.decision = true, landing.Decision{Reason: "bound"}
	_, err = failUnclaimedLocked(ctx, tx, stream, b.facts.Mission, task, "bound", "its window ended unclaimed")
	return b, err
}

// unclaimedEnd: the record a refusal of a root never claimed leaves, when
// no later pass can turn it around without the Owner (CUT §7.7): stale only
// for what does not recover (a mission done, stopped or expired, a dropped
// step, a goal whose producing plan another plan replaced); result;
// in_main or reverted when A is already in main's history. A paused mission,
// a stopped step, unknown history, dependencies, rule and limit can change by
// themselves: no record, the next pass tries again, the window ends it.
func unclaimedEnd(d landing.Decision, f landing.Facts) (string, bool) {
	if !inHandoff(f) {
		return "", false
	}
	switch d.Reason {
	case "stale":
		switch {
		case slices.Contains([]string{"done", "stopped", "expired"}, f.MissionState), f.StepState == "dropped",
			f.GoalTask && f.ProducingPlan != "" && f.LatestUsedPlan != "" && f.ProducingPlan != f.LatestUsedPlan:
			return "stale", true
		}
	case "result":
		return "result", true
	case "floor":
		return "floor", true
	case "history":
		switch f.History.State {
		case landing.Effective:
			return "in_main", true
		case landing.Reverted:
			return "reverted", true
		}
	}
	return "", false
}

// inHandoff is the first-claim handoff read from the claim's locked facts,
// the durable facts Close (landingHolds) and SettleUnclaimed read: an
// accepted task, landing enabled on its repository's stream, its mission
// created and the task accepted no earlier, the mission not no_merge. Only a
// root in it is ever recorded unclaimed (review 653 R653-1): one rebound to
// an older mission, or never in it, is refused as before and left alone.
func inHandoff(f landing.Facts) bool {
	return f.TaskState == "accepted" && !f.EnabledAt.IsZero() && !f.AcceptedAt.IsZero() && !f.MissionCreated.IsZero() &&
		!f.AcceptedAt.Before(f.EnabledAt) && !f.MissionCreated.Before(f.EnabledAt) && !f.NoMerge
}

// refusalEvidence is what the claim saw, kept in the record's text (the
// failed record's fields are closed): the bindings, and for a history ending
// the commit main holds for A and the main commit it was read at, which can
// differ from A (a cherry-pick) and can change later (review 652 R652-3).
func refusalEvidence(f landing.Facts) string {
	e := fmt.Sprintf("attempt %s, A %s, base %s, mission %s", f.Attempt, f.A, f.Base, f.MissionState)
	if f.History.State == landing.Effective || f.History.State == landing.Reverted {
		e += fmt.Sprintf("; main history %s at commit %s, read at %s", f.History.State, f.History.Commit, f.History.Snapshot)
	}
	return e
}

// failUnclaimedLocked records that a root never claimed will not be: its
// first operation, born failed. It takes the stream's lock, reads the stream
// again and records nothing when the root has an operation by now (a claim,
// or another pass's record): false. Every caller holds the task's mission
// (Close, the claim) or nothing else, so the stream stays the last lock.
func failUnclaimedLocked(ctx context.Context, tx pgx.Tx, stream, mission, task, why, evidence string, detail ...map[string]any) (bool, error) {
	if err := lockStream(ctx, tx, stream); err != nil {
		return false, err
	}
	state, err := LandingState(ctx, tx, stream)
	if err != nil {
		return false, err
	}
	if state.LatestOperation(task) != "" {
		return false, nil
	}
	op := fmt.Sprintf("land:%s:1", task)
	body := map[string]any{"text": "the landing loop will not claim this task: " + why + " (" + evidence + ")", "landing": map[string]any{"v": landing.Version, "kind": "failed", "op": op, "why": why}}
	if len(detail) > 0 && detail[0] != nil {
		body["landing_floor"] = detail[0]
	}
	_, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", MissionID: mission, TaskID: task, Body: body})
	return err == nil, err
}
