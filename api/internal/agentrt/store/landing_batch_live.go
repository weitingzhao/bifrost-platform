package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// errBatchRefused rolls the batch gate back when a member other than the
// lead is refused: nothing of the batch is appended.
var errBatchRefused = errors.New("landing batch gate: a member is refused")

// AuthorizeBatchPush is the final gate of a batch (landing_batch.go): ops
// are its members in order, the lead first. In one transaction it takes
// every member's locks in the global order (capability, each tenant's rule,
// each member's step, task and mission, each tenant's quota stream, the
// repository stream), then judges every member with the gate's per-operation
// judgment (authorizeLandingPushTx) and appends each member's reserved and
// main_intent, the lead's with its grant use when it awaited one. One
// Authorization per member, all for the same R and M0: the caller pushes R
// once. Any member refused, the decision is its refusal (Detail names the
// op) and nothing is appended; only the lead's own records of a refusal
// stand, as for a single operation (awaiting_grant, a refused grant, a
// discard).
func AuthorizeBatchPush(ctx context.Context, db DB, src LandingSource, ops []string, observe func(context.Context, landing.Operation) (LiveObservation, error), repos ...string) ([]Authorization, landing.Decision, error) {
	return authorizeBatchPush(ctx, db, src, ops, observe, false, landing.Repository(repos...))
}

// RecheckWaitingBatch is AuthorizeBatchPush with the lead, waiting with no
// usable grant, judged as RecheckWaitingLanding judges it.
func RecheckWaitingBatch(ctx context.Context, db DB, src LandingSource, ops []string, observe func(context.Context, landing.Operation) (LiveObservation, error), repos ...string) ([]Authorization, landing.Decision, error) {
	return authorizeBatchPush(ctx, db, src, ops, observe, true, landing.Repository(repos...))
}

func authorizeBatchPush(ctx context.Context, db DB, src LandingSource, ops []string, observe func(context.Context, landing.Operation) (LiveObservation, error), recheck bool, repo string) ([]Authorization, landing.Decision, error) {
	stream := "landing:" + repo
	if repo != "bifrost-platform" || len(ops) < 2 {
		return nil, landing.Decision{Reason: "repo"}, nil
	}
	var auths []Authorization
	var decision landing.Decision
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		auths, decision = nil, landing.Decision{}
		state, err := LandingState(ctx, tx, stream)
		if err != nil {
			return err
		}
		batches, _, err := readLandingBatches(ctx, tx, stream)
		if err != nil {
			return err
		}
		if b, ok := findBatch(batches, landing.BatchID(ops[0])); !ok || !slices.Equal(b.Members, ops) {
			decision = landing.Decision{Reason: "not_ready", Detail: map[string]any{"op": ops[0]}}
			return nil
		}
		type binding struct{ task, tenant, step, mission string }
		var binds []binding
		for _, op := range ops {
			o, exists := state.Operations[op]
			if !exists {
				return fmt.Errorf("unknown landing operation")
			}
			b := binding{task: o.Task}
			if err = tx.QueryRow(ctx, `SELECT m.tenant, COALESCE(t.plan_node_id,''), m.mission_id FROM agentrt.task t JOIN agentrt.mission m USING (mission_id) WHERE t.task_id = $1`, o.Task).Scan(&b.tenant, &b.step, &b.mission); err != nil {
				return err
			}
			binds = append(binds, b)
		}
		var tenants []string
		for _, b := range binds {
			tenants = append(tenants, b.tenant)
		}
		slices.Sort(tenants)
		tenants = slices.Compact(tenants)
		day := DecisionAt().UTC().Format("2006-01-02")
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, capabilityLock); err != nil {
			return err
		}
		for _, tenant := range tenants {
			if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1, hashtext($2))`, ruleLock, "landing:"+tenant); err != nil {
				return err
			}
		}
		slices.SortFunc(binds, func(a, b binding) int { return strings.Compare(a.task, b.task) })
		for _, b := range binds {
			if err = lockLandingBinding(ctx, tx, b.task, b.tenant, b.step, b.mission); err != nil {
				return err
			}
		}
		for _, tenant := range tenants {
			if err = lockStream(ctx, tx, "landing-quota:"+tenant+":"+day); err != nil {
				return err
			}
		}
		if err = lockStream(ctx, tx, stream); err != nil {
			return err
		}
		for i, op := range ops {
			a, d, err := authorizeLandingPushTx(ctx, tx, src, op, observe, recheck && i == 0, repo)
			if err != nil {
				return err
			}
			if !d.Allowed {
				detail := map[string]any{"op": op}
				for k, v := range d.Detail {
					detail[k] = v
				}
				d.Detail = detail
				decision = d
				if i == 0 {
					return nil
				}
				return errBatchRefused
			}
			if len(auths) > 0 && (a.R != auths[0].R || a.M0 != auths[0].M0) {
				return fmt.Errorf("landing batch members authorized for different commits")
			}
			auths = append(auths, a)
		}
		decision = landing.Decision{Allowed: true}
		return nil
	})
	if errors.Is(err, errBatchRefused) {
		return nil, decision, nil
	}
	if err != nil {
		return nil, landing.Decision{}, err
	}
	if !decision.Allowed {
		auths = nil
	}
	return auths, decision, nil
}
