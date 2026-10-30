package stgrelease

import (
	"context"
	"errors"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func (c *Controller) prepareMembers(ctx context.Context, s store.StgState, b *store.StgBatch, p string) (bool, error) {
	var errs []error
	waiting := false
	for m := range b.Members {
		proof := s.Validity[store.StgValidityKey{Member: m, P: p}].Record
		// A definite negative must reach the complete-range final gate. It
		// is not permission to release, nor proof of a legal disposal.
		validity := proof.String("validity")
		if validity == "effective" && proof["has_r"] == true || validity == "reverted" || validity == "absent" || reversal(s, m) > 0 {
			continue
		}
		waiting = true
		if c.Sources.MainEvidence == nil || c.DB == nil {
			continue
		}
		o := s.Legacy.Operations[m.Op]
		readAt := store.DecisionAt()
		ev, err := c.Sources.MainEvidence(ctx, "bifrost-platform", m.Root, o.A, m.R)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if ev.Head != p {
			errs = append(errs, errors.New("STG main changed during member observation"))
			continue
		}
		if !c.configured() {
			return true, errors.Join(errs...)
		}
		ev.ReadAt = readAt
		candidate := store.MainCandidate{Op: m.Op, Root: m.Root, Phase: o.Phase, Outcome: o.Outcome, R: m.R, M0: o.M0, A: o.A}
		_, err = store.ReconcileStgMainOne(ctx, c.DB, c.Tenant, "bifrost-platform", candidate, ev)
		if err != nil {
			errs = append(errs, err)
		}
	}
	// Any append invalidates the caller's stream version. A new pass rereads
	// every member independently; an unknown member never fabricates validity.
	return waiting, errors.Join(errs...)
}

// A block can outlive B4's recent-mission observation window. Acquire a real
// landing_main proof ourselves when main advances, then consume it next pass.
// Merely accepting a SHA from Head would bypass the stored proof contract.
func (c *Controller) readBlockHead(ctx context.Context, s store.StgState, block store.StgBlock) (bool, error) {
	if c.Sources.Head == nil || c.Sources.MainEvidence == nil || c.DB == nil {
		return false, nil
	}
	p, err := c.Sources.Head(ctx)
	if err != nil || p == block.Opened.Record.String("P") {
		return false, err
	}
	ms, _ := object(block.Opened.Record, "detail")["members"].([]any)
	for _, value := range ms {
		m, _ := value.(map[string]any)
		op, _ := m["op"].(string)
		o := s.Legacy.Operations[op]
		if !o.EverLanded || o.Phase != "landed" || s.Legacy.LatestOperation(o.Task) != op {
			continue
		}
		at := store.DecisionAt()
		ev, err := c.Sources.MainEvidence(ctx, "bifrost-platform", o.Task, o.A, o.R)
		if err != nil {
			return false, err
		}
		if ev.Unreadable || ev.Head != p || !c.configured() {
			return false, nil
		}
		ev.ReadAt = at
		did, err := store.ReconcileStgMainOne(ctx, c.DB, c.Tenant, "bifrost-platform", store.MainCandidate{Op: op, Root: o.Task, Phase: o.Phase, Outcome: o.Outcome, R: o.R, M0: o.M0, A: o.A}, ev)
		if err != nil || did != "" {
			return did != "", err
		}
	}
	return false, nil
}
