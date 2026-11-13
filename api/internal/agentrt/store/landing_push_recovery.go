package store

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

const RecoverBatchPushAction = "recover_batch_landing_push"

type pushRecoveryKey struct{}

type PushRecoveryParams struct{ Env, Repo, Op, R, M0, GrantID, CIRun string }

func DecodePushRecoveryParams(m map[string]any) (PushRecoveryParams, error) {
	p := PushRecoveryParams{}
	keys := []string{"env", "repo", "op", "R", "M0", "grant_id", "ci_run"}
	vals := []*string{&p.Env, &p.Repo, &p.Op, &p.R, &p.M0, &p.GrantID, &p.CIRun}
	if len(m) != len(keys) {
		return p, invalid("recovery requires exact parameters")
	}
	for i, k := range keys {
		v, ok := m[k].(string)
		if !ok || v == "" {
			return p, invalid("missing recovery parameter %s", k)
		}
		*vals[i] = v
	}
	if p.Env != "prod" || p.Repo != "bifrost-platform" || !landing.ValidSHA(p.R) || !landing.ValidSHA(p.M0) {
		return p, invalid("recovery is PROD platform only with full SHAs")
	}
	return p, nil
}

// RecoverBatchPush reopens the same candidate, never a new replay or CI. The
// controller still performs its full final gate before creating new push intents.
// The original grant's expiry, waivers, CI identity and STG choice are unchanged.
func RecoverBatchPush(ctx context.Context, db DB, src GrantSources, p PushRecoveryParams, approvalID string) error {
	if src.Main == nil || src.Facts.GrantCI == nil {
		return invalid("recovery evidence unavailable")
	}
	return InTx(ctx, db, func(tx pgx.Tx) error {
		if err := lockStream(ctx, tx, landingRepoStream); err != nil {
			return err
		}
		var previous string
		err := tx.QueryRow(ctx, `SELECT body_json->'landing'->>'approval_id' FROM agentrt.event WHERE event_stream_id=$1 AND body_json->'landing'->>'kind'='push_recovered' AND body_json->'landing'->>'op'=$2 ORDER BY seq DESC LIMIT 1`, landingRepoStream, p.Op).Scan(&previous)
		if err == nil {
			if approvalID != "" && previous == approvalID {
				return nil
			}
			return invalid("batch already recovered; no repeat")
		}
		if err != pgx.ErrNoRows {
			return err
		}
		scan, err := scanLandingStream(ctx, tx, landingRepoStream)
		if err != nil {
			return err
		}
		state, err := landing.FoldWithGrants(scan.entries, grantPeriods(scan.order))
		if err != nil {
			return err
		}
		batches, _, err := readLandingBatches(ctx, tx, landingRepoStream)
		if err != nil {
			return err
		}
		b, ok := findBatch(batches, landing.BatchID(p.Op))
		if !ok || b.Lead != p.Op || len(b.Members) < 2 || b.R != p.R || b.M0 != p.M0 {
			return invalid("recovery batch differs")
		}
		g := scan.byID[p.GrantID]
		now := DecisionAt(ctx)
		if g == nil || g.Op != p.Op || g.R != p.R || g.M0 != p.M0 || g.CI.Run != p.CIRun || g.ConsumedSeq == 0 || g.ConsumedOp != p.Op || !g.RevokedAt.IsZero() || !g.RefusedAt.IsZero() || !now.Before(g.exp) || g.Batch == nil || !slices.Equal(g.Batch.Members, b.Members) {
			return invalid("original batch grant is not recoverable")
		}
		for _, op := range b.Members {
			o := state.Operations[op]
			if state.LatestOperation(o.Task) != op || o.R != p.R || o.M0 != p.M0 || o.EverLanded || !((o.Phase == "main_result" && o.Outcome == "unknown") || (o.Phase == "failed" && o.Outcome == "bound")) {
				return invalid("member %s is not an unsent latest candidate", op)
			}
			var intent time.Time
			if err := tx.QueryRow(ctx, `SELECT max(at) FROM agentrt.event WHERE event_stream_id=$1 AND body_json->'landing'->>'op'=$2 AND body_json->'landing'->>'kind'='main_intent'`, landingRepoStream, op).Scan(&intent); err != nil {
				return err
			}
			if intent.IsZero() || now.Before(intent.Add(MainFence)) {
				return invalid("old push fence has not elapsed")
			}
		}
		head, err := src.Main(ctx)
		if err != nil {
			return err
		}
		if head != p.M0 {
			return invalid("main changed; recovery refused")
		}
		key, ok := scan.ciKeys[p.Op]
		if !ok || key.Revision != p.R || key.Base != p.M0 {
			return invalid("original CI key differs")
		}
		ci, err := readGrantCI(ctx, src.Facts, key, state.Operations[p.Op].PreparedAt)
		if err != nil {
			return err
		}
		created, valid := grantTime(g.CI.CreatedAt)
		if !valid {
			return invalid("invalid original CI time")
		}
		if ci.Status != "Succeeded" || ci.Run != g.CI.Run || ci.UID != g.CI.UID || ci.Pipeline != g.CI.Pipeline || ci.Candidate != p.R || !ci.CreatedAt.Equal(created) {
			return invalid("original green CI no longer matches")
		}
		if !DecisionAt(ctx).Before(g.exp) {
			return invalid("original grant expired during read")
		}
		if approvalID == "" {
			return nil
		} // creation preflight is read-only
		ctx = context.WithValue(ctx, pushRecoveryKey{}, approvalID)
		for i, op := range b.Members {
			resume := "branch_pushed"
			if i == 0 {
				resume = "awaiting_grant"
			}
			_, err = Append(ctx, tx, Event{Stream: landingRepoStream, Type: "note", Actor: "controller", TaskID: state.Operations[op].Task, Body: map[string]any{"text": fmt.Sprintf("Recover same batch push under approval %s; preserve CI %s and grant %s", approvalID, p.CIRun, p.GrantID), "landing": map[string]any{"v": landing.Version, "kind": "push_recovered", "op": op, "R": p.R, "M0": p.M0, "grant_id": p.GrantID, "approval_id": approvalID, "resume": resume}}})
			if err != nil {
				return err
			}
		}
		return nil
	})
}
