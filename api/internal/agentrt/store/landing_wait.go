package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// appendAwaitingGrant records a preparation snapshot, not C-5's approval
// material. The latter must be recomputed from full evidence at request time;
// this digest is never accepted as a grant or permission to push.
func appendAwaitingGrant(ctx context.Context, tx pgx.Tx, f landing.Facts, op string, ci landing.CIRun, soft []rules.LandingRefusal) error {
	set := make([]string, 0, len(soft))
	for _, r := range soft {
		set = append(set, string(r))
	}
	slices.Sort(set)
	set = slices.Compact(set)
	lines := 0
	for _, c := range f.Changes {
		for _, n := range []int{c.Added, c.Deleted} {
			if n < 0 || n > int(^uint(0)>>1)-lines {
				return fmt.Errorf("landing line count overflow")
			}
			lines += n
		}
	}
	// Marshal structs before canonicalizing; canon accepts only JSON values.
	raw, err := json.Marshal(map[string]any{"v": landing.Version, "scope": "waiting_snapshot",
		"task": f.Task, "attempt": f.Attempt, "op": op, "A": f.A, "base": f.Base,
		"R": f.Operation.R, "M0": f.Operation.M0, "committer_date": f.Operation.CommitterDate,
		"changes": f.Changes, "ci": ci, "rule": f.Rule, "soft": set, "checks_version": rules.LandingChecksVersion})
	if err != nil {
		return err
	}
	v, err := canon.Parse(raw)
	if err != nil {
		return err
	}
	sum, err := canon.Sum(v)
	if err != nil {
		return err
	}
	_, err = Append(ctx, tx, Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", TaskID: f.Task, MissionID: f.Mission,
		Body: map[string]any{"text": "Landing awaits a D-tier grant", "landing": map[string]any{
			"v": landing.Version, "kind": "awaiting_grant", "op": op, "soft": set, "files": len(f.Changes), "lines": lines, "material": "sha256:" + sum}}})
	return err
}

// lockWaitingRoot takes task -> mission -> repository stream. No external
// service is called while these locks are held. The task lock fixes its
// mission/plan binding; a rebind cannot change what the following read sees.
func lockWaitingRoot(ctx context.Context, tx pgx.Tx, root string) (string, error) {
	var mission string
	if err := tx.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id=$1 FOR SHARE`, root).Scan(&mission); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.mission WHERE mission_id=$1 FOR SHARE`, mission); err != nil {
		return "", err
	}
	return mission, lockStream(ctx, tx, "landing:bifrost-platform")
}

// SettleWaitingLandingStale is independent of live, rules and Git. Pauses are
// recoverable; stopped/done/expired missions, cancelled tasks and dropped or
// superseded plan steps end a wait once, just as unclaimedEnd classifies them.
func SettleWaitingLandingStale(ctx context.Context, db DB) ([]string, error) {
	var state landing.State
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var e error
		state, e = LandingState(ctx, tx, "landing:bifrost-platform")
		return e
	})
	if err != nil {
		return nil, err
	}
	var ended []string
	for _, op := range latestOps(state) {
		o := state.Operations[op]
		if o.Phase != "awaiting_grant" {
			continue
		}
		wrote := false
		err = InTx(ctx, db, func(tx pgx.Tx) error {
			wrote = false
			mission, err := lockWaitingRoot(ctx, tx, o.Task)
			if err != nil {
				return err
			}
			s, err := LandingState(ctx, tx, "landing:bifrost-platform")
			if err != nil {
				return err
			}
			if s.LatestOperation(o.Task) != op || s.Operations[op].Phase != "awaiting_grant" {
				return nil
			}
			stale, err := waitingStaleDB(ctx, tx, o.Task)
			if err != nil {
				return err
			}
			if !stale {
				return nil
			}
			_, err = Append(ctx, tx, Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", MissionID: mission, TaskID: o.Task,
				Body: map[string]any{"text": "Landing authorization wait became stale", "landing": map[string]any{"v": landing.Version, "kind": "failed", "op": op, "why": "stale"}}})
			wrote = err == nil
			return err
		})
		if err != nil {
			return ended, err
		}
		if wrote {
			ended = append(ended, op)
		}
	}
	return ended, nil
}

// No rule, quota, approval or Git read can block a stale ending.
func waitingStaleDB(ctx context.Context, tx pgx.Tx, root string) (bool, error) {
	var stale bool
	err := tx.QueryRow(ctx, `SELECT m.state IN ('done','stopped','expired')
	  OR COALESCE(m.scope_json->>'no_merge','') <> '' OR t.state='cancelled'
	  OR COALESCE(p.state,'')='dropped'
	  OR (COALESCE(t.off_plan_reason,'') LIKE 'planned by task %'
	    AND latest.task_id IS NOT NULL
	    AND substring(t.off_plan_reason FROM length('planned by task ')+1) <> latest.task_id)
	  FROM agentrt.task t JOIN agentrt.mission m USING(mission_id)
	  LEFT JOIN agentrt.plan_node p ON p.plan_node_id=t.plan_node_id
	  LEFT JOIN LATERAL (SELECT e.task_id FROM agentrt.event e JOIN agentrt.task pt ON pt.task_id=e.task_id
	    WHERE pt.mission_id=m.mission_id AND pt.role='plan' AND pt.state='accepted'
	    AND e.event_type='task_state_changed' AND e.body_json->>'to'='accepted'
	    AND e.body_json->>'reason' LIKE 'plan used:%' ORDER BY e.event_id DESC LIMIT 1) latest ON true
	  WHERE t.task_id=$1`, root).Scan(&stale)
	return stale, err
}

// RefreshLanding retires exactly the latest waiting generation. main is read
// by a trusted server adapter before taking locks. The discarded ref is never
// touched; the existing claim/replay protocol owns the next generation.
func RefreshLanding(ctx context.Context, db DB, task, op, host string, readMain func(context.Context, string) (string, error)) (bool, error) {
	if task == "" || op == "" || host == "" {
		return false, invalid("refresh needs task, op and executor")
	}
	if readMain == nil {
		return false, fmt.Errorf("main reader unavailable")
	}
	// Authenticate ownership before external work. Rechecked under locks.
	checkHost := func(tx pgx.Tx) error {
		var permitted bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.attempt a JOIN agentrt.host h USING(host_id)
		  JOIN agentrt.task t USING(task_id) WHERE a.task_id=$1 AND a.host_id=$2
		  AND COALESCE(to_jsonb(h)->>'host_kind','')='external'
		  AND COALESCE(to_jsonb(t)->>'executor_pin','') <> ''
		  AND a.attempt_id=(SELECT v.attempt_id FROM agentrt.verification v WHERE v.task_id=t.task_id AND v.accepts_attempt ORDER BY v.created_at DESC,v.verification_id DESC LIMIT 1))`, task, host).Scan(&permitted)
		if err != nil {
			return err
		}
		if !permitted {
			return invalid("refresh_not_permitted")
		}
		return nil
	}
	if err := InTx(ctx, db, checkHost); err != nil {
		return false, err
	}
	head, err := readMain(ctx, "bifrost-platform")
	if err != nil {
		return false, err
	}
	if !landing.ValidSHA(head) {
		return false, fmt.Errorf("main head unreadable")
	}
	wrote := false
	err = InTx(ctx, db, func(tx pgx.Tx) error {
		wrote = false
		mission, err := lockWaitingRoot(ctx, tx, task)
		if err != nil {
			return err
		}
		if err := checkHost(tx); err != nil {
			return err
		}
		wrote, err = refreshWaitingLocked(ctx, tx, task, op, mission, head)
		return err
	})
	return wrote, err
}

// Shared generation transition; callers already hold task, mission and stream.
func refreshWaitingLocked(ctx context.Context, tx pgx.Tx, task, op, mission, head string) (bool, error) {
	s, err := LandingState(ctx, tx, "landing:bifrost-platform")
	if err != nil {
		return false, err
	}
	o := s.Operations[op]
	if s.LatestOperation(task) != op || o.Task != task || o.Phase != "awaiting_grant" {
		return false, invalid("refresh_not_waiting")
	}
	if !landing.ValidSHA(head) {
		return false, invalid("main_unreadable")
	}
	if head == o.M0 {
		return false, invalid("main_unchanged")
	}
	if why := landing.RootBound(s, task, DecisionAt(), false); why != "" {
		return false, failLandingLimit(ctx, tx, op, why)
	}
	_, err = Append(ctx, tx, Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", MissionID: mission, TaskID: task,
		Body: map[string]any{"text": "Landing refreshed after main advanced to " + head, "landing": map[string]any{"v": landing.Version, "kind": "discarded", "op": op, "why": "main_changed"}}})
	return err == nil, err
}
