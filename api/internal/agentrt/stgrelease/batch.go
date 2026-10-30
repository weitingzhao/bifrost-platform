package stgrelease

import (
	"context"
	"errors"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"k8s.io/apimachinery/pkg/types"
)

func (c *Controller) batch(ctx context.Context, s store.StgState, id string, b *store.StgBatch) error {
	t := b.Tries[b.Try]
	if t != nil && t.Occupied() {
		return c.observe(ctx, s, id, b, t)
	}
	if err := c.releaseWhenIdle(b); err != nil {
		return err
	}
	// A durable final-gate blocker may have committed immediately before a
	// crash. End this batch against that original interval, never create another.
	for _, block := range s.Blocks {
		if block.IsOpen() {
			return c.write(ctx, s, finish(s, id, b, block.Opened.Seq, false)...)
		}
	}
	p := c.Policy()
	p.Live = true
	if (b.Phase == "OPEN" || b.Phase == "SNAP") && b.Selected.Seq > 0 && !p.enabled(c.now()) {
		base, _ := store.StgTrustedBaseline(s)
		if t != nil {
			base = object(t.Snapshot.Record, "base")
		}
		if base == nil {
			base = store.StgRecord{"P": ""}
		}
		return errors.Join(c.release(), c.blockAndFinish(ctx, s, id, b, "rule", base, nil))
	}
	if b.Try == 0 {
		return c.prepare(ctx, s, id, b)
	}
	if t.RunEvidence.Seq > 0 && t.Verify.Seq == 0 {
		return c.verify(ctx, s, id, b, t)
	}
	if t.Verify.Seq > 0 && t.Retry.Seq == 0 {
		return c.conclude(ctx, s, id, b, t)
	}
	if t.QuarantineClosed.Seq > 0 || t.Unsent.Seq > 0 {
		return c.write(ctx, s, finish(s, id, b, max(t.QuarantineClosed.Seq, t.Unsent.Seq), false)...)
	}
	if t.Retry.Seq > 0 {
		if c.now().Before(t.Retry.Record.Time("not_before")) {
			return nil
		}
		r := batchRecord("snapshot", id, b.Try+1, "inputs", t.Snapshot.Record["inputs"], "base", t.Snapshot.Record["base"], "set", t.Snapshot.Record["set"], "not_before", t.Retry.Record["not_before"])
		return c.write(ctx, s, r)
	}
	if b.Phase == "SNAP" {
		if !c.Policy().Live || c.now().Before(t.Snapshot.Record.Time("not_before")) {
			return nil
		}
		if err := c.hold(ctx, s, id, b.Try); err != nil {
			return windowWait(err)
		}
		source := store.StgGateSource{ControlTenant: c.Tenant, Environment: c.Policy().Environment, Live: func() bool { return c.configured() && c.Policy().Live }, Generation: c.holder.Generation, Observe: func(ctx context.Context, snap store.StgRecord) (store.StgGateObservation, error) {
			obs, err := c.gateObservation(ctx, snap)
			if err != nil {
				return obs, err
			}
			obs.Lease, err = c.holder.Observe(ctx)
			return obs, err
		}}
		d, err := (Sender{ControlTenant: c.Tenant, DB: c.DB, Runs: c.Runs, Gate: source, Window: c.holder, FinalizerGuaranteed: c.FinalizerGuaranteed}).Start(ctx, id, b.Try)
		if err != nil {
			// Keep a dispatched intent's lease, but do not hold the window
			// through a failed pre-send observation or denied transaction.
			current, readErr := c.ledger().Read(ctx)
			if readErr == nil {
				if cb := current.Batches[id]; cb != nil && !cb.Tries[cb.Try].Occupied() {
					return errors.Join(err, c.release())
				}
			}
			return errors.Join(err, readErr)
		}
		if !d.Allowed {
			releaseErr := c.release()
			// The gate can have persisted a blocker. Reread before settlement.
			current, readErr := c.ledger().Read(ctx)
			if readErr != nil {
				return errors.Join(releaseErr, readErr)
			}
			if cb := current.Batches[id]; cb != nil && cb.Tries[cb.Try].Started.Seq == 0 {
				if d.Block != "" {
					return errors.Join(releaseErr, c.write(ctx, current, finish(current, id, cb, current.Blocks[d.Block].Opened.Seq, false)...))
				}
				if d.Handoff {
					return errors.Join(releaseErr, c.write(ctx, current, finish(current, id, cb, cb.Tries[cb.Try].Snapshot.Seq, false)...))
				}
				if d.Why == "ci_waiting" {
					return errors.Join(releaseErr, c.ciWait(ctx, current, id, cb))
				}
			}
			return releaseErr
		}
	}
	return nil
}
func windowWait(err error) error {
	var held *delivery.WindowHeld
	if errors.Is(err, ErrWindowWait) || errors.Is(err, ErrWindowDisabled) || errors.As(err, &held) {
		return nil
	}
	return err
}
func (c *Controller) releaseWhenIdle(b *store.StgBatch) error {
	if b.Phase == "OPEN" || b.Phase == "SNAP" && c.Policy().Live && !c.now().Before(b.Tries[b.Try].Snapshot.Record.Time("not_before")) {
		return nil
	}
	return c.release()
}
func (c *Controller) prepare(ctx context.Context, s store.StgState, id string, b *store.StgBatch) error {
	if !c.Policy().Live {
		return c.release()
	}
	if err := c.hold(ctx, s, id, 1); err != nil {
		return windowWait(err)
	}
	if c.Sources.Head == nil || c.Sources.CI == nil {
		return errors.Join(c.release(), errors.New("STG selection source unavailable"))
	}
	p, err := c.Sources.Head(ctx)
	if err != nil {
		return errors.Join(c.release(), err)
	}
	if b.Selected.Seq == 0 {
		return c.write(ctx, s, batchRecord("p_selected", id, 0, "P", p, "at", c.now().Format(time.RFC3339Nano)))
	}
	if b.Selected.Record.String("P") != p {
		return errors.Join(c.release(), c.write(ctx, s, finish(s, id, b, b.Selected.Seq, false)...))
	}
	base, known := store.StgTrustedBaseline(s)
	if !known {
		return errors.Join(c.release(), c.blockAndFinish(ctx, s, id, b, "baseline_unknown", store.StgRecord{"P": ""}, nil))
	}
	ci, err := c.Sources.CI(ctx, p)
	if err != nil {
		return errors.Join(c.release(), err)
	}
	accepted, err := ci.Record()
	if err != nil {
		why := ""
		switch {
		case ci.Status == "Failed":
			why = "ci_red"
		case ci.Reason != "":
			why = "ci_results"
		case ci.Status == "Missing" && c.now().Sub(b.Selected.Record.Time("at")) >= 30*time.Minute:
			why = "ci_missing"
		}
		if why != "" {
			return errors.Join(c.release(), c.blockAndFinish(ctx, s, id, b, why, base, &ci))
		}
		return errors.Join(c.release(), c.ciWait(ctx, s, id, b))
	}
	for _, old := range b.CI {
		prior := object(old.Record, "ci")
		if prior.String("ui_commit") != store.StgRecord(accepted).String("ui_commit") || prior.String("infra_commit") != store.StgRecord(accepted).String("infra_commit") {
			return errors.Join(c.release(), c.write(ctx, s, finish(s, id, b, b.Selected.Seq, false)...))
		}
	}
	if b.CI[ci.UID].Seq == 0 {
		return c.write(ctx, s, batchRecord("ci_accepted", id, 0, "P", p, "ci", accepted))
	}
	// Capture actual main validity while main is still P. These existing
	// landing_main proofs remain valid at P even if main advances during the
	// run; verification never mislabels a historical read as today's main.
	if wait, err := c.prepareMembers(ctx, s, b, p); wait || err != nil {
		return errors.Join(c.release(), err)
	}
	if c.Sources.Range == nil {
		return errors.Join(c.release(), errors.New("STG range source unavailable"))
	}
	commits, err := c.Sources.Range(ctx, base.String("P"), p)
	if err != nil {
		return errors.Join(c.release(), err)
	}
	set := []any{}
	due := c.now()
	// Derive the proposed debit set. B5-c rederives it under binding/rule locks
	// and authorizes every commit, including disposed originals and reversals.
	for _, commit := range commits {
		op := landinggit.LandedOperation(commit.Message)
		o := s.Legacy.Operations[op]
		if !o.EverLanded || o.R != commit.SHA {
			continue
		}
		m := store.StgMemberKey{Root: o.Task, Op: op, R: commit.SHA}
		old := s.Members[m]
		if old.To == "delivered" || old.To == "covered" || old.To == "disposed" || cover(s, m) != "" {
			continue
		}
		if _, ok := b.Members[m]; !ok {
			return errors.Join(c.release(), c.write(ctx, s, finish(s, id, b, b.Selected.Seq, false)...))
		}
		set = append(set, memberValue(m))
		if s.Debits[m.R].NotBefore.After(due) {
			due = s.Debits[m.R].NotBefore
		}
	}
	sortMembers(set)
	in := map[string]any{"namespace": delivery.STGReleaseNamespace, "pipeline": delivery.STGReleasePipeline, "revision": p, "uiRevision": accepted["ui_commit"], "infraRevision": accepted["infra_commit"], "params_canonical": map[string]any{"revision": p, "uiRevision": accepted["ui_commit"], "infraRevision": accepted["infra_commit"]}}
	return c.write(ctx, s, batchRecord("snapshot", id, 1, "inputs", in, "base", map[string]any(base), "set", set, "not_before", due.Format(time.RFC3339Nano)))
}
func (c *Controller) ciWait(ctx context.Context, s store.StgState, id string, b *store.StgBatch) error {
	r := diagnostic(b, id, "ci_waiting", 1)
	if r == nil {
		return nil
	}
	return c.write(ctx, s, r)
}
func (c *Controller) gateObservation(ctx context.Context, snap store.StgRecord) (store.StgGateObservation, error) {
	var o store.StgGateObservation
	if c.Sources.Head == nil || c.Sources.CI == nil || c.Sources.Range == nil || c.Runs == nil {
		return o, errors.New("STG gate readers unavailable")
	}
	var err error
	o.Main, err = c.Sources.Head(ctx)
	if err != nil {
		return o, err
	}
	o.BaseP = object(snap, "base").String("P")
	o.Commits, err = c.Sources.Range(ctx, o.BaseP, o.Main)
	if err != nil {
		return o, err
	}
	o.CI, err = c.Sources.CI(ctx, o.Main)
	if err != nil {
		return o, err
	}
	o.Workloads, err = c.Runs.ObserveWorkloads(ctx)
	o.ReadAt = c.now()
	return o, err
}
func (c *Controller) observe(ctx context.Context, s store.StgState, id string, b *store.StgBatch, t *store.StgTry) error {
	if c.Runs == nil {
		return errors.New("STG run observer unavailable")
	}
	// Recovery holds only its own recorded window. Failure to recover it does
	// not prevent reading the original run, and does not authorize another run.
	if c.Policy().Live && !t.IsQuarantined() && c.holder == nil {
		_ = c.hold(ctx, s, id, b.Try)
	}
	if t.IsQuarantined() {
		if err := c.release(); err != nil {
			return err
		}
	}
	if t.Bound.Seq == 0 {
		_, err := (Sender{ControlTenant: c.Tenant, DB: c.DB, Runs: c.Runs, FinalizerGuaranteed: c.FinalizerGuaranteed}).Recover(ctx, id, b.Try)
		return err
	}
	identity := delivery.STGRunIdentity{Namespace: delivery.STGReleaseNamespace, Name: t.Started.Record.String("run"), UID: types.UID(t.Bound.Record.String("uid"))}
	obs, err := c.Runs.ObserveRun(ctx, identity)
	if err != nil {
		// Reuse B5-c for bound 404/object-lost. Other read failures leave the
		// execution slot intact; a failed read is never terminal evidence.
		if c.DB != nil {
			_, recoverErr := (Sender{ControlTenant: c.Tenant, DB: c.DB, Runs: c.Runs, FinalizerGuaranteed: c.FinalizerGuaranteed}).Recover(ctx, id, b.Try)
			return errors.Join(err, recoverErr)
		}
		return err
	}
	if obs.Status != "Succeeded" && obs.Status != "Failed" {
		if c.now().Sub(t.Started.At) >= time.Hour {
			if r := diagnostic(b, id, "running_long", 1); r != nil {
				return c.write(ctx, s, r)
			}
		}
		return nil
	}
	in := input(id, b.Try, t.Started.Seq, object(t.Snapshot.Record, "inputs"))
	if obs.Identity != identity || delivery.MatchNamedDeliverRun(obs.Run, in, true) != nil {
		return delivery.ErrSTGCollision
	}
	at := c.now()
	if completed, _, _ := unstructuredTime(obs.Run.Object, "status", "completionTime"); !completed.IsZero() && !completed.Before(t.Started.At) && !completed.After(at) {
		at = completed
	}
	r := batchRecord("run_evidence", id, b.Try, "uid", string(identity.UID), "status", obs.Status, "raw", obs.Raw, "at", at.Format(time.RFC3339Nano))
	if err = c.write(ctx, s, r); err != nil {
		return err
	}
	return c.release() // even a stale CAS observed the real terminal run
}
func unstructuredTime(m map[string]any, path ...string) (time.Time, bool, error) {
	var value any = m
	for _, key := range path {
		obj, ok := value.(map[string]any)
		if !ok {
			return time.Time{}, false, nil
		}
		value = obj[key]
	}
	str, ok := value.(string)
	if !ok {
		return time.Time{}, false, nil
	}
	tm, err := time.Parse(time.RFC3339Nano, str)
	return tm, ok, err
}
