package store

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// PrepareS5Evidence fixes the requirement and key in the operation stream,
// before Ensure. A local trusted replay may supply its patch before the land
// branch exists remotely. Every binding is re-read under the same lock order
// as the final gate; no quota reservation or main intent is written here.
func PrepareS5Evidence(ctx context.Context, db DB, src LandingSource, env, op string, local *landing.PatchEvidence) (landing.Operation, landing.Decision, error) {
	var out landing.Operation
	var decision landing.Decision
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		state, err := LandingState(ctx, tx, landingRepoStream)
		if err != nil {
			return err
		}
		o, ok := state.Operations[op]
		if !ok {
			return fmt.Errorf("unknown S5 operation")
		}
		var tenant, step, mission string
		if err = tx.QueryRow(ctx, `SELECT m.tenant,COALESCE(t.plan_node_id,''),t.mission_id FROM agentrt.task t JOIN agentrt.mission m USING(mission_id) WHERE t.task_id=$1`, o.Task).Scan(&tenant, &step, &mission); err != nil {
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
		if err = lockStream(ctx, tx, landingRepoStream); err != nil {
			return err
		}
		state, err = LandingState(ctx, tx, landingRepoStream)
		if err != nil {
			return err
		}
		o = state.Operations[op]
		out = o
		if o.Terminal() || o.AtMain() || o.R == "" || o.Phase == "awaiting_grant" {
			decision = landing.Decision{Reason: "S5_PREPARATION_ONLY"}
			return nil
		}
		if why := landing.RootBound(state, o.Task, DecisionAt(), false); why != "" {
			decision = landing.Decision{Reason: why}
			return nil
		}
		if stopped, e := repoStopped(ctx, tx, landingRepoStream); e != nil || stopped {
			decision = landing.Decision{Reason: "stopped"}
			return e
		}
		read := src
		if local != nil {
			read.S5Patch = func(_ context.Context, repo, task, base, a, m0, r string) (landing.PatchEvidence, error) {
				if repo != "bifrost-platform" || task != o.Task || base != local.Base || a != local.A || m0 != local.M0 || r != local.R {
					return landing.PatchEvidence{}, fmt.Errorf("S5_BINDING: local replay")
				}
				return *local, nil
			}
		}
		mode := "live"
		if env == "stg" {
			mode = "dry_run"
		}
		f, err := LandingFacts(ctx, tx, read, env, mode, o.Task, op, "", DecisionAt())
		if err != nil {
			return err
		}
		// Slow Git reads cannot extend rule validity or hide a newer review.
		g, _, err := landingFactsDB(ctx, tx, read, env, mode, o.Task, op, DecisionAt())
		if err != nil {
			return err
		}
		g.SingleCommit, g.Changes, g.History, g.DependenciesEffective = f.SingleCommit, f.Changes, f.History, f.DependenciesEffective
		g.S5Patch = f.S5Patch
		bindS5ReviewedPatch(&g)
		q := landing.ClassifyS5PathsAndRelations(g, g.S5Policy)
		if !q.NeedsS5Evidence {
			decision = landing.Decision{Allowed: true}
			return nil
		}
		decision = landing.MayPrepareS5(g, q, DecisionAt()).Decision
		if !decision.Allowed {
			return nil
		}
		if g.A != o.A || g.Attempt != o.Attempt {
			decision = landing.Decision{Reason: "S5_REVIEW_BINDING"}
			return nil
		}
		generation := o.S5.Key.Generation
		if generation == 0 {
			generation = 1
		}
		key, why := landing.NewS5CIKey(g, op, generation)
		if why != "" {
			decision = landing.Decision{Reason: why}
			return nil
		}
		if key == o.S5.Key {
			if o.S5.Failure != "" {
				decision = landing.Decision{Reason: o.S5.Failure}
			}
			return nil
		}
		if o.S5.Key.Generation != 0 {
			if generation == math.MaxInt64 {
				decision = landing.Decision{Reason: "S5_GENERATION_STALE"}
				return nil
			}
			key.Generation++
		}
		kd, err := key.Digest()
		if err != nil {
			return err
		}
		_, err = Append(ctx, tx, Event{Stream: landingRepoStream, Type: "note", Actor: "controller", TaskID: o.Task, MissionID: mission,
			Body: map[string]any{"text": "S5 evidence key fixed; prior generation superseded (S5_BINDING)", "landing": map[string]any{"v": landing.S5Version, "kind": "s5_key", "op": op, "key": toBody(key), "key_digest": kd}}})
		if err != nil {
			return err
		}
		state, err = LandingState(ctx, tx, landingRepoStream)
		out = state.Operations[op]
		return err
	})
	return out, decision, err
}

// BindS5Run is idempotent only for the SAME UID and complete persisted key.
func BindS5Run(ctx context.Context, db DB, key landing.S5CIKey, uid string) error {
	if uid == "" {
		return fmt.Errorf("S5_CI_IDENTITY")
	}
	return InTx(ctx, db, func(tx pgx.Tx) error {
		if err := lockStream(ctx, tx, landingRepoStream); err != nil {
			return err
		}
		s, err := LandingState(ctx, tx, landingRepoStream)
		if err != nil {
			return err
		}
		o := s.Operations[key.Op]
		if o.S5.Key != key || o.Terminal() || o.AtMain() {
			return fmt.Errorf("S5_GENERATION_STALE")
		}
		if o.S5.UID != "" {
			if o.S5.UID == uid {
				return nil
			}
			return fmt.Errorf("S5_CI_IDENTITY")
		}
		fields := map[string]any{"v": landing.S5Version, "kind": "s5_run", "op": key.Op, "key_digest": o.S5.KeyDigest, "uid": uid}
		_, err = Append(ctx, tx, Event{Stream: landingRepoStream, Type: "note", Actor: "controller", TaskID: o.Task, Body: map[string]any{"text": "S5 evidence s5_run", "landing": fields}})
		return err
	})
}

// FailS5Landing ends the operation with an S5 code (§5.1): a terminal CI
// result for its generation, or a deterministic S5 refusal (path, change
// kind, review binding/provider, patch). The generation's failure (when a
// key exists) and the operation's end are one transaction, so the
// repository's in-flight slot is released with the reason recorded. A
// generation superseded meanwhile is not ended on the old key's behalf.
func FailS5Landing(ctx context.Context, db DB, op string, key landing.S5CIKey, why string) error {
	if !landing.S5Code(why) {
		return fmt.Errorf("S5 landing failure needs an S5 code")
	}
	return InTx(ctx, db, func(tx pgx.Tx) error {
		stream := landingRepoStream
		if err := lockStream(ctx, tx, stream); err != nil {
			return err
		}
		s, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		o, ok := s.Operations[op]
		if !ok || o.Terminal() || o.AtMain() {
			return nil
		}
		if o.S5.Key != key {
			return fmt.Errorf("S5_GENERATION_STALE")
		}
		if key.Valid() && o.S5.Failure == "" {
			fields := map[string]any{"v": landing.S5Version, "kind": "s5_failed", "op": op, "key_digest": o.S5.KeyDigest, "why": why}
			if _, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: o.Task, Body: map[string]any{"text": "S5 evidence s5_failed", "landing": fields}}); err != nil {
				return err
			}
		}
		fields := map[string]any{"v": landing.S5Version, "kind": "failed", "op": op, "why": why}
		_, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: o.Task, Body: map[string]any{"text": "S5 refusal ends the landing: " + why, "landing": fields}})
		return err
	})
}

// RetryS5Generation opens the next generation with unchanged material after
// the bound run failed for an infrastructure reason (the release freeze
// check refused it). The protocol bounds the count and backoff
// (landing.S5InfraRetries); nothing else may rerun a generation.
func RetryS5Generation(ctx context.Context, db DB, key landing.S5CIKey) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		if err := lockStream(ctx, tx, landingRepoStream); err != nil {
			return err
		}
		s, err := LandingState(ctx, tx, landingRepoStream)
		if err != nil {
			return err
		}
		o := s.Operations[key.Op]
		if o.S5.Key != key || o.Terminal() || o.AtMain() {
			return fmt.Errorf("S5_GENERATION_STALE")
		}
		next := key
		next.Generation++
		kd, err := next.Digest()
		if err != nil {
			return err
		}
		_, err = Append(ctx, tx, Event{Stream: landingRepoStream, Type: "note", Actor: "controller", TaskID: o.Task,
			Body: map[string]any{"text": "S5 run refused by the release freeze check; next generation (infrastructure retry)", "landing": map[string]any{"v": landing.S5Version, "kind": "s5_key", "op": key.Op, "key": toBody(next), "key_digest": kd, "retry": "infra"}}})
		return err
	})
}

// VerifyLandingS5 is used at preliminary and final gates, always reading the
// trusted CI service again. Persisted success is audit, never authorization.
func VerifyLandingS5(ctx context.Context, src LandingSource, f landing.Facts, now time.Time) (landing.VerifiedS5, landing.S5CIResult, string, error) {
	if src.S5CI == nil || !f.Operation.S5.Key.Valid() || f.Operation.S5.UID == "" {
		return landing.VerifiedS5{}, landing.S5CIResult{}, "S5_EVIDENCE_MISSING", nil
	}
	if f.Operation.S5.Failure != "" {
		return landing.VerifiedS5{}, landing.S5CIResult{}, f.Operation.S5.Failure, nil
	}
	ci, err := src.S5CI(ctx, f.Operation.S5.Key, f.Operation.S5.UID)
	if err != nil {
		return landing.VerifiedS5{}, ci, "S5_EVIDENCE_MISSING", err
	}
	if ci.CreatedAt.After(now) || ci.CompletedAt.After(now) {
		return landing.VerifiedS5{}, ci, "S5_CI_IDENTITY", nil
	}
	v, why := landing.VerifyS5Evidence(ci, f, f.S5Policy, f.S5Review)
	return v, ci, why, nil
}
