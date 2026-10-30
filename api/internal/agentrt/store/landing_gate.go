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
func FinishLandingDrill(ctx context.Context, db DB, src LandingSource, op string, now time.Time, observe func(context.Context, landing.Operation) (landing.DrillObservation, error), repos ...string) error {
	repo := landing.Repository(repos...)
	if repo == "" {
		return fmt.Errorf("unsupported landing repo")
	}
	stream := "landing:" + repo
	return InTx(ctx, db, func(tx pgx.Tx) error {
		state, err := LandingState(ctx, tx, stream)
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
		if err = lockLandingBinding(ctx, tx, o.Task, tenant, step, mission); err != nil {
			return err
		}
		if err = lockStream(ctx, tx, stream); err != nil {
			return err
		}
		at := DecisionAt() // CUT §9: the rule and the window as of these locks
		f, err := LandingFacts(ctx, tx, src, "stg", "dry_run", o.Task, op, "", at)
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
		state, err = LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		obs, err := observe(ctx, o)
		if err != nil {
			return err
		}
		// The facts and the observation read Git and CI under these locks,
		// for as long as they take: decide at the clock after them. The rule
		// is selected again at that time (a version that took effect during
		// the reads decides, review 656 B-B1) with the database half read
		// again; the external evidence read above stands, its keys held by
		// these locks.
		final := DecisionAt()
		g, _, err := landingFactsDB(ctx, tx, src, "stg", "dry_run", o.Task, op, final)
		if err != nil {
			return err
		}
		g.SingleCommit, g.Changes, g.History, g.DependenciesEffective = f.SingleCommit, f.Changes, f.History, f.DependenciesEffective
		obs.Facts = g
		if action := landing.NextDrill(state, op, obs, final); action.Kind != "ready" {
			return fmt.Errorf("drill final gate: %s %s", action.Kind, action.Why)
		}
		for _, r := range []map[string]any{
			landing.CIRecord(op, obs.Candidate),
			{"v": landing.Version, "kind": "rehearsed", "op": op, "R": o.R},
		} {
			if _, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: o.Task, MissionID: mission, Body: map[string]any{"text": fmt.Sprintf("STG drill %s; second gate rule version %d", r["kind"], g.Rule.Version), "landing": r}}); err != nil {
				return err
			}
		}
		return nil
	})
}
