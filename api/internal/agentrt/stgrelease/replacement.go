package stgrelease

import (
	"context"
	"errors"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Both successful and failed runs use the same provenance and current
// deployment checks. Matching digests alone cannot establish another release.
func (c *Controller) checkReplacement(ctx context.Context, s store.StgState, t *store.StgTry, workloads map[string]any) (store.StgRecord, error) {
	for _, ev := range orderedEvidence(s) {
		e := ev.Record
		if e.String("evidence") == c.Cluster+"/"+object(t.Snapshot.Record, "inputs").String("namespace")+"/"+t.Bound.Record.String("uid") {
			continue
		}
		ec := object(e, "content")
		if !workloadsMatch(workloads, ec.String("api_digest"), ec.String("console_digest")) {
			continue
		}
		if _, err := c.Runs.ReadPin(ctx, ec.String("api_digest"), ec.String("console_digest")); err != nil {
			if !definite(err) {
				return nil, err
			}
			continue
		}
		health := c.Runs.ReadHealth(ctx)
		if health.Outcome == "unknown" {
			return nil, errors.New("replacement health is unknown")
		}
		if health.Outcome != "passed" {
			continue
		}
		return store.StgRecord{"v": "1", "kind": "deployment_checked", "scope": "ext", "evidence": e["evidence"], "content": e["content"], "deployed": map[string]any{"pin_sync_ok": true, "workloads": workloads, "at": c.now().Format(time.RFC3339Nano)}, "health": health.Evidence}, nil
	}
	return nil, nil
}

func (c *Controller) concludeOther(ctx context.Context, s store.StgState, id string, b *store.StgBatch, t *store.StgTry) error {
	if c.Runs == nil {
		return c.unknown(ctx, s, id, b)
	}
	workloads, err := c.Runs.ObserveWorkloads(ctx)
	if err != nil && !definite(err) {
		return c.unknown(ctx, s, id, b)
	}
	if err == nil {
		check, err := c.checkReplacement(ctx, s, t, workloads)
		if err != nil {
			return c.unknown(ctx, s, id, b)
		}
		if check != nil {
			// The original verify{failed} remains immutable. The fresh
			// replacement check and each member's disposition commit together.
			return c.concludeRemaining(ctx, s, id, b, t, true, check)
		}
	}
	return c.blockAndFinish(ctx, s, id, b, "baseline_moved", object(t.Snapshot.Record, "base"), nil)
}
