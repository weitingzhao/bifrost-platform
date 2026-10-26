package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// FinishLandingDrill holds the second gate's rule and row locks through the
// final fresh external observation and the rehearsal record. It reserves no
// production quota and has no deployment or main-push callback.
func FinishLandingDrill(ctx context.Context, db DB, src LandingSource, op string, now time.Time, observe func(context.Context, landing.Operation) (landing.DrillObservation, error)) error {
	started := time.Now()
	return InTx(ctx, db, func(tx pgx.Tx) error {
		state, err := LandingState(ctx, tx, "landing:bifrost-platform")
		if err != nil {
			return err
		}
		o, exists := state.Operations[op]
		if !exists {
			return fmt.Errorf("unknown drill operation")
		}
		var tenant, step, mission string
		if err = tx.QueryRow(ctx, `SELECT m.tenant,COALESCE(t.plan_node_id,''),m.mission_id FROM agentrt.task t JOIN agentrt.mission m USING(mission_id) WHERE t.task_id=$1`, o.Task).Scan(&tenant, &step, &mission); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, capabilityLock); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1,hashtext($2))`, ruleLock, "landing:"+tenant); err != nil {
			return err
		}
		if step != "" {
			if _, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id=$1 FOR SHARE`, step); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.task WHERE task_id=$1 FOR SHARE`, o.Task); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.mission WHERE mission_id=$1 FOR SHARE`, mission); err != nil {
			return err
		}
		if err = lockStream(ctx, tx, "landing:bifrost-platform"); err != nil {
			return err
		}
		f, err := LandingFacts(ctx, tx, src, "stg", "dry_run", o.Task, op, "", now)
		if err != nil {
			return err
		}
		o = f.Operation
		if o.Phase == "rehearsed" {
			return nil
		}
		if o.Phase != "branch_pushed" || !o.DryRun {
			return fmt.Errorf("not a pushed drill")
		}
		state, err = LandingState(ctx, tx, "landing:bifrost-platform")
		if err != nil {
			return err
		}
		obs, err := observe(ctx, o)
		if err != nil {
			return err
		}
		obs.Facts = f
		if action := landing.NextDrill(state, op, obs, now.Add(time.Since(started))); action.Kind != "ready" {
			return fmt.Errorf("drill final gate: %s %s", action.Kind, action.Why)
		}
		for _, r := range []map[string]any{
			{"v": landing.Version, "kind": "ci_seen", "op": op, "run": obs.Candidate.Run, "status": obs.Candidate.Status, "R": o.R, "created_at": obs.Candidate.CreatedAt.Format(time.RFC3339Nano)},
			{"v": landing.Version, "kind": "rehearsed", "op": op, "R": o.R},
		} {
			if _, err = Append(ctx, tx, Event{Stream: "landing:bifrost-platform", Type: "note", Actor: "controller", TaskID: o.Task, MissionID: mission, Body: map[string]any{"text": fmt.Sprintf("STG drill %s; second gate rule version %d", r["kind"], f.Rule.Version), "landing": r}}); err != nil {
				return err
			}
		}
		return nil
	})
}
