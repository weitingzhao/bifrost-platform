package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// LandingSource reads trusted external evidence anew. It must distinguish a
// full history with no result from an unreadable or reverted history. It does
// not use MainResults, whose effective-only projection loses that distinction.
// S2 supplies git/CI adapters; S3 supplies the full STG snapshot diff.
type LandingSource struct {
	Result         func(ctx context.Context, repo, task, attempt, base, commit string) (singleCommit bool, changes []rules.LandingChange, err error)
	History        func(ctx context.Context, repo, task, commit, snapshot string) (landing.MainHistory, error)
	DeploymentDiff func(ctx context.Context, repo, snapshot string) ([]rules.LandingChange, error)
}

// LandingFacts is the common reader for all three gates. The caller supplies
// a fresh transaction; the final gate must first hold the locks specified by
// the cut (capabilities, rule, step, task, mission, quota, repository stream).
// This function opens no network and never replaces an error with Never.
func LandingFacts(ctx context.Context, tx pgx.Tx, src LandingSource, environment, mode, taskID, op, snapshot string, now time.Time) (landing.Facts, error) {
	f, deps, err := landingFactsDB(ctx, tx, src, environment, mode, taskID, op, now)
	if err != nil {
		return f, err
	}
	err = landingFactsExternal(ctx, src, &f, deps, snapshot)
	return f, err
}

// landingDep is one dependency of the task as the database binds it: the
// task, its repository and the commit its accepted attempt pushed.
type landingDep struct{ task, repo, commit string }

// landingFactsDB is LandingFacts' database half: every field but the
// external evidence (SingleCommit, Changes, History, DependenciesEffective,
// SnapshotHistory, DeployChanges), and the dependencies whose history the
// external half reads. It opens no network.
func landingFactsDB(ctx context.Context, tx pgx.Tx, src LandingSource, environment, mode, taskID, op string, now time.Time) (landing.Facts, []landingDep, error) {
	f := landing.Facts{Environment: environment, Mode: mode, Task: taskID}
	var mission, step, offPlan, role, class string
	err := tx.QueryRow(ctx, `SELECT t.mission_id, COALESCE(t.plan_node_id,''), COALESCE(t.off_plan_reason,''), t.role,t.task_class,
  COALESCE(t.repo,''),COALESCE(t.base_commit,''),t.state,m.tenant,m.state,m.version,m.created_at,
  COALESCE(m.scope_json->>'no_merge','') <> '',COALESCE(p.state,''),COALESCE(p.version,0)
  FROM agentrt.task t JOIN agentrt.mission m USING(mission_id) LEFT JOIN agentrt.plan_node p ON p.plan_node_id=t.plan_node_id
  WHERE t.task_id=$1`, taskID).Scan(&mission, &step, &offPlan, &role, &class, &f.Repo, &f.Base, &f.TaskState, &f.Tenant, &f.MissionState, &f.MissionVersion, &f.MissionCreated, &f.NoMerge, &f.StepState, &f.StepVersion)
	if err != nil {
		return f, nil, err
	}
	f.Mission = mission
	if role != "implement" {
		return f, nil, fmt.Errorf("landing only implements")
	}
	if step != "" && f.StepState == "" {
		return f, nil, fmt.Errorf("unreadable plan node")
	}
	f.GoalTask = class == "goal" || strings.HasPrefix(offPlan, "planned by task ")
	if f.GoalTask {
		f.ProducingPlan = strings.TrimPrefix(offPlan, "planned by task ")
		if f.ProducingPlan == offPlan {
			f.ProducingPlan = ""
		}
		// Acceptance of a used plan orders adoption, not task creation time.
		err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT e.task_id FROM agentrt.event e JOIN agentrt.task t ON t.task_id=e.task_id
   WHERE t.mission_id=$1 AND t.role='plan' AND t.state='accepted' AND e.event_type='task_state_changed'
   AND e.body_json->>'to'='accepted' AND e.body_json->>'reason' LIKE 'plan used:%' ORDER BY e.event_id DESC LIMIT 1),'')`, mission).Scan(&f.LatestUsedPlan)
		if err != nil {
			return f, nil, err
		}
	}
	var accepted *time.Time
	err = tx.QueryRow(ctx, `SELECT COALESCE(a.attempt_id,''),COALESCE(a.pushed_commit,''),
  (SELECT max(e.at) FROM agentrt.event e WHERE e.task_id=t.task_id AND e.event_type='task_state_changed' AND e.body_json->>'to'='accepted')
  FROM agentrt.task t LEFT JOIN LATERAL (SELECT attempt_id FROM agentrt.verification WHERE task_id=t.task_id AND accepts_attempt ORDER BY created_at DESC,verification_id DESC LIMIT 1) v ON true
  LEFT JOIN agentrt.attempt a ON a.attempt_id=v.attempt_id WHERE t.task_id=$1`, taskID).Scan(&f.Attempt, &f.A, &accepted)
	if err != nil {
		return f, nil, err
	}
	if err = tx.QueryRow(ctx, `SELECT COALESCE(string_agg(verifier_attempt_id, ',' ORDER BY verifier_attempt_id),'') FROM agentrt.verification WHERE task_id=$1 AND attempt_id=$2 AND method='review' AND verdict='pass'`, taskID, f.Attempt).Scan(&f.Reviewers); err != nil {
		return f, nil, err
	}
	if accepted != nil {
		f.AcceptedAt = *accepted
	}
	current, err := CurrentRule(ctx, tx, "landing:"+f.Tenant, now)
	if err != nil {
		return f, nil, err
	}
	if current != nil {
		f.Rule = &current.Rule
	}
	state, err := LandingState(ctx, tx, "landing:"+f.Repo)
	if err != nil {
		return f, nil, err
	}
	f.EnabledAt = state.EnabledAt
	f.Operation = state.Operations[op]
	for id, o := range state.Operations {
		if id != op && o.Task != "" && o.Active() {
			f.OtherInFlight++
		}
		if o.Task == taskID && o.BlocksClaim() {
			f.Terminal = true
		}
	}
	quota, err := LandingState(ctx, tx, "landing-quota:"+f.Tenant+":"+now.UTC().Format("2006-01-02"))
	if err != nil {
		return f, nil, err
	}
	f.ReservedToday = len(quota.Reservations)
	if _, reserved := quota.Reservations[taskID]; reserved {
		f.ReservedToday--
	}
	if src.Result == nil || src.History == nil {
		return f, nil, fmt.Errorf("landing source unavailable")
	}
	rows, err := tx.Query(ctx, `SELECT t.task_id,COALESCE(t.repo,''),COALESCE(a.pushed_commit,'') FROM agentrt.task_dependency d JOIN agentrt.task t ON t.task_id=d.depends_on_task_id
  LEFT JOIN LATERAL (SELECT attempt_id FROM agentrt.verification WHERE task_id=t.task_id AND accepts_attempt ORDER BY created_at DESC,verification_id DESC LIMIT 1) v ON true
  LEFT JOIN agentrt.attempt a ON a.attempt_id=v.attempt_id WHERE d.task_id=$1 ORDER BY t.task_id`, taskID)
	if err != nil {
		return f, nil, err
	}
	deps, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (landingDep, error) {
		var d landingDep
		err := r.Scan(&d.task, &d.repo, &d.commit)
		return d, err
	})
	if err != nil {
		return f, nil, err
	}
	return f, deps, nil
}

// landingFactsExternal is LandingFacts' external half: it reads the result,
// main's history and each dependency's history through src, and needs no
// transaction (a claim reads it with no lock held, TD-447 5).
// landingClaimExternal is the claim's read (no snapshot): it stops at the
// first answer that already refuses for good, so a read error after it
// cannot hide it (CUT §7.7): a result that is not one commit reads no
// history, and A already in main's history reads no dependency.
// A result the database already shows unclaimable (no accepted attempt, A
// or Base not a commit id) reads nothing: the adapter would fail on it with
// an error that is not a refusal (review 652 R652-2). Today the task table's
// base_commit check keeps Base a commit id; this guards the claim, not that.
func landingClaimExternal(ctx context.Context, src LandingSource, f *landing.Facts, deps []landingDep) error {
	if f.Attempt == "" || !landing.ValidSHA(f.A) || !landing.ValidSHA(f.Base) {
		return nil
	}
	return landingFactsRead(ctx, src, f, deps, "", true)
}

func landingFactsExternal(ctx context.Context, src LandingSource, f *landing.Facts, deps []landingDep, snapshot string) error {
	return landingFactsRead(ctx, src, f, deps, snapshot, false)
}

func landingFactsRead(ctx context.Context, src LandingSource, f *landing.Facts, deps []landingDep, snapshot string, claim bool) error {
	var err error
	f.SingleCommit, f.Changes, err = src.Result(ctx, f.Repo, f.Task, f.Attempt, f.Base, f.A)
	if err != nil {
		return err
	}
	if claim && !f.SingleCommit {
		return nil
	}
	f.History, err = src.History(ctx, f.Repo, f.Task, f.A, "")
	if err != nil {
		return err
	}
	if claim && (f.History.State == landing.Effective || f.History.State == landing.Reverted) {
		return nil
	}
	f.DependenciesEffective = true
	for _, d := range deps {
		if d.repo == "" || d.commit == "" {
			f.DependenciesEffective = false
			continue
		}
		h, err := src.History(ctx, d.repo, d.task, d.commit, "")
		if err != nil {
			return err
		}
		if h.State != landing.Effective || h.Commit == "" {
			f.DependenciesEffective = false
		}
	}
	if snapshot != "" {
		f.SnapshotHistory, err = src.History(ctx, f.Repo, f.Task, f.A, snapshot)
		if err != nil {
			return err
		}
		if src.DeploymentDiff == nil {
			return fmt.Errorf("deployment diff unavailable")
		}
		f.DeployChanges, err = src.DeploymentDiff(ctx, f.Repo, snapshot)
		if err != nil {
			return err
		}
	}
	return nil
}
