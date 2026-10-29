package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// StgGateSource has no write operations. Observe runs once, outside InTx,
// with a bounded context. Live/Generation read the current process switches
// and lease owner; they must not perform I/O or take database locks.
type StgGateSource struct {
	Environment string
	Live        func() bool
	Generation  func() int64
	Observe     func(context.Context, StgRecord) (StgGateObservation, error)
}

type StgSendPermit struct {
	Batch                                 string
	Try, IntentSeq, PermitSeq, Generation int64
	Deadline                              time.Time
	Inputs, Started                       StgRecord
}

// StgNotFoundProof comes only from a successful named GET returning 404. A
// retained-object capability is mandatory; ordinary errors are not absence.
// The exact permit read before GET is bound to the proof to reject stale reads.
type StgNotFoundProof struct {
	IntentSeq, PermitSeq int64
	ReadAt               time.Time
	FinalizerGuaranteed  bool
}

func ReadStgRelease(ctx context.Context, db DB) (StgState, error) {
	var s StgState
	err := InTx(ctx, db, func(tx pgx.Tx) error { var err error; s, err = StgReleaseState(ctx, tx); return err })
	return s, err
}

// AuthorizeStgRelease is the B5-c final gate. nil proof means first send;
// recovery requires the unbound, expired permit's retained-object 404 proof.
// A refusal persists its blocking interval (where applicable), and recovery
// records unsent in the same transaction. No network write occurs here.
func AuthorizeStgRelease(ctx context.Context, db DB, batch string, try int64, src StgGateSource, proof *StgNotFoundProof) (StgSendPermit, StgGateDecision, error) {
	var permit StgSendPermit
	var decision StgGateDecision
	if src.Observe == nil || src.Live == nil || src.Generation == nil {
		return permit, decision, errors.New("STG gate source unavailable")
	}
	initial, err := ReadStgRelease(ctx, db)
	if err != nil {
		return permit, decision, err
	}
	b := initial.Batches[batch]
	if b == nil || b.Try != try || b.Tries[try] == nil {
		return permit, StgGateDecision{Why: "not_ready"}, nil
	}
	snapshot := b.Tries[try].Snapshot
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	obs, err := src.Observe(readCtx, snapshot.Record)
	cancel()
	if err != nil {
		return permit, decision, err
	} // unknown never becomes unsent
	err = InTx(ctx, db, func(tx pgx.Tx) error {
		s, err := StgReleaseState(ctx, tx)
		if err != nil {
			return err
		}
		bindings, err := stgDiscoverBindings(ctx, tx, s, batch, obs)
		if err != nil {
			return err
		}
		hook("stg gate: bindings read", batch)
		if err = stgLockBindings(ctx, tx, bindings); err != nil {
			return err
		}
		if err = lockStream(ctx, tx, StgReleaseStream); err != nil {
			return err
		}
		s, err = StgReleaseState(ctx, tx)
		if err != nil {
			return err
		}
		again, err := stgDiscoverBindings(ctx, tx, s, batch, obs)
		if err != nil {
			return err
		}
		if !sameStgBindings(bindings, again) {
			return ErrLandingBindingChanged
		}
		b := s.Batches[batch]
		if b == nil || b.Try != try || b.Tries[try] == nil || b.Tries[try].Snapshot.Seq != snapshot.Seq {
			decision.Why = "not_ready"
			return nil
		}
		t := b.Tries[try]
		at := DecisionAt()
		if proof != nil && !stgValidNotFound(t, *proof, at) {
			decision.Why = "not_ready"
			return nil
		}
		facts, err := stgReadGateFacts(ctx, tx, s, again, at)
		if err != nil {
			return err
		}
		facts.Live, facts.Environment, facts.Generation = src.Live(), src.Environment, src.Generation()
		decision = stgEvaluateGate(s, batch, obs, facts, at, proof != nil)
		if !decision.Allowed {
			if err = stgRecordRefusal(ctx, tx, s, batch, obs, facts, at, &decision); err != nil {
				return err
			}
			if proof != nil {
				_, err = appendStg(ctx, tx, stgBatchRecord("unsent", batch, try, "proof", "gate_failed_before_send"))
			}
			return err
		}
		deadline := at.Add(2 * time.Minute)
		if obs.Lease.Expires.Before(deadline) {
			deadline = obs.Lease.Expires
		}
		lease := map[string]any{"who": obs.Lease.Who, "gen": obs.Lease.Generation}
		var r StgRecord
		if proof != nil {
			r = stgBatchRecord("resend", batch, try, "intent_seq", t.Started.Seq, "lease", lease, "deadline", deadline.Format(time.RFC3339Nano), "why", "not_found_unbound")
		} else {
			// Same-U CI succession is recorded without changing frozen inputs.
			if _, exists := b.CI[obs.CI.UID]; !exists {
				ci, _ := obs.CI.Record()
				_, err = appendStg(ctx, tx, StgRecord{"v": "1", "scope": "batch", "kind": "ci_accepted", "batch": batch, "P": obs.Main, "ci": ci})
				if err != nil {
					return err
				}
			}
			n := stgBatchNumber(batch)
			labels := map[string]any{"bifrost.io/stg-release": "true", "bifrost.io/stg-release-n": strconv.FormatInt(n, 10), "bifrost.io/stg-release-try": strconv.FormatInt(try, 10)}
			annotations := map[string]any{"bifrost.io/stg-release-batch": batch, "bifrost.io/stg-release-intent": "0"}
			r = stgBatchRecord("started", batch, try, "intent_seq", int64(0), "run", fmt.Sprintf("stgrel-%d-%d-%s", n, try, obs.Main[:8]), "labels", labels, "annotations", annotations, "fingerprint", stgFingerprint(snapshot.Record.object("inputs"), labels, annotations), "spend", []any{}, "lease", lease, "deadline", deadline.Format(time.RFC3339Nano))
		}
		hook("stg gate: before permit", batch)
		a, err := appendStg(ctx, tx, r)
		if err != nil {
			return err
		}
		s, err = StgReleaseState(ctx, tx)
		if err != nil {
			return err
		}
		t = s.Batches[batch].Tries[try]
		permit = StgSendPermit{Batch: batch, Try: try, IntentSeq: t.Started.Seq, PermitSeq: a.Seq, Generation: obs.Lease.Generation, Deadline: deadline, Inputs: t.Snapshot.Record.object("inputs"), Started: t.Started.Record}
		return nil
	})
	if err != nil {
		return StgSendPermit{}, StgGateDecision{}, err
	}
	return permit, decision, nil
}

func stgDiscoverBindings(ctx context.Context, tx pgx.Tx, s StgState, batch string, obs StgGateObservation) (map[string]stgBinding, error) {
	roots := map[string]bool{}
	if b := s.Batches[batch]; b != nil {
		for m := range b.Members {
			roots[m.Root] = true
		}
	}
	for _, c := range obs.Commits {
		if o, ok := s.Legacy.Operations[landinggit.LandedOperation(c.Message)]; ok && o.Task != "" {
			roots[o.Task] = true
		}
	}
	out := map[string]stgBinding{}
	for _, root := range sortedStgKeys(roots) {
		b := stgBinding{Root: root}
		err := tx.QueryRow(ctx, `SELECT m.tenant,COALESCE(t.plan_node_id,''),t.mission_id,COALESCE(t.repo,''),COALESCE(p.state,''),COALESCE(m.scope_json->>'no_merge','') <> '' FROM agentrt.task t JOIN agentrt.mission m USING(mission_id) LEFT JOIN agentrt.plan_node p ON p.plan_node_id=t.plan_node_id WHERE t.task_id=$1`, root).Scan(&b.Tenant, &b.Step, &b.Mission, &b.Repo, &b.StepState, &b.NoMerge)
		if err != nil {
			return nil, err
		}
		out[root] = b
	}
	return out, nil
}

// Compare identities, then use the locked reread's mutable policy fields.
func sameStgBindings(a, b map[string]stgBinding) bool {
	if len(a) != len(b) {
		return false
	}
	for root, x := range a {
		y, ok := b[root]
		if !ok || x.Root != y.Root || x.Tenant != y.Tenant || x.Step != y.Step || x.Mission != y.Mission || x.Repo != y.Repo {
			return false
		}
	}
	return true
}

func sortedStgKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func stgLockBindings(ctx context.Context, tx pgx.Tx, bs map[string]stgBinding) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, capabilityLock); err != nil {
		return err
	}
	tenants, steps, missions := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, b := range bs {
		tenants[b.Tenant] = true
		missions[b.Mission] = true
		if b.Step != "" {
			steps[b.Step] = true
		}
	}
	for _, tenant := range sortedStgKeys(tenants) {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1,hashtext($2))`, ruleLock, "landing:"+tenant); err != nil {
			return err
		}
	}
	for _, step := range sortedStgKeys(steps) {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id=$1 FOR SHARE`, step); err != nil {
			return err
		}
	}
	for _, task := range sortedStgKeys(bs) {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.task WHERE task_id=$1 FOR SHARE`, task); err != nil {
			return err
		}
	}
	for _, mission := range sortedStgKeys(missions) {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.mission WHERE mission_id=$1 FOR SHARE`, mission); err != nil {
			return err
		}
	}
	return nil
}

func stgReadGateFacts(ctx context.Context, tx pgx.Tx, s StgState, bindings map[string]stgBinding, at time.Time) (stgGateFacts, error) {
	f := stgGateFacts{Bindings: bindings, Rules: map[string]*rules.Rule{}, Authority: map[string]stgLandingAuthority{}}
	for _, b := range bindings {
		if _, seen := f.Rules[b.Tenant]; seen {
			continue
		}
		r, err := CurrentRule(ctx, tx, "landing:"+b.Tenant, at)
		if err != nil {
			return f, err
		}
		f.Rules[b.Tenant] = nil
		if r != nil {
			f.Rules[b.Tenant] = &r.Rule
		}
	}
	rows, err := tx.Query(ctx, `SELECT body_json->'landing'->>'op',body_json->'landing_gate',body_json ? 'landing_grant_use' FROM agentrt.event WHERE event_stream_id=$1 AND body_json->'landing'->>'kind'='main_intent' ORDER BY seq`, StgReleaseStream)
	if err != nil {
		return f, err
	}
	for rows.Next() {
		var op string
		var raw []byte
		var used bool
		if err = rows.Scan(&op, &raw, &used); err != nil {
			break
		}
		a := stgLandingAuthority{UsedGrant: used}
		if len(raw) > 0 {
			var v any
			v, err = canon.Parse(raw)
			if err != nil {
				break
			}
			m, _ := v.(map[string]any)
			if badLandingGate(m) == "" && m["op"] == op {
				a.RuleVersion = StgRecord(m).Int("rule_version")
			}
		}
		f.Authority[op] = a
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return f, err
	}
	for op, a := range f.Authority {
		if !a.UsedGrant {
			continue
		}
		if _, needed := bindings[s.Legacy.Operations[op].Task]; !needed {
			continue
		}
		a.Grant, a.HasGrant, err = LandingGrantEvidence(ctx, tx, "bifrost-platform", op)
		if err != nil {
			return f, err
		}
		// The C-5 public read intentionally hides subsequently reverted
		// operations. B5 still authorizes every original commit in base..P,
		// including one undone by another authorized landed commit. Retain
		// that original, consumed grant as provenance; never revive or consume
		// it again. The whole-range gate also checks the reversal commit.
		if !a.HasGrant {
			o := s.Legacy.Operations[op]
			if o.EverLanded && o.Reverted && !o.DryRun {
				_, order, e := readLandingGrants(ctx, tx, StgReleaseStream)
				if e != nil {
					return f, e
				}
				a.Grant, a.HasGrant = stgRevertedGrant(s, op, order)
			}
		}
		f.Authority[op] = a
	}
	return f, nil
}

func stgRevertedGrant(s StgState, op string, grants []*grantView) (GrantEvidence, bool) {
	o := s.Legacy.Operations[op]
	if !o.EverLanded || !o.Reverted || o.DryRun {
		return GrantEvidence{}, false
	}
	for _, g := range grants {
		if g.Op == op && g.ConsumedOp == op && g.ConsumedSeq > 0 && g.R == o.R {
			return GrantEvidence{GrantID: g.GrantID, ApprovalID: g.ApprovalID, Purpose: g.Purpose, STGRelease: g.STGRelease, Task: g.Task, Attempt: g.Attempt, A: g.A, R: g.R, M0: g.M0, IntentSeq: g.ConsumedSeq}, true
		}
	}
	return GrantEvidence{}, false
}

func stgBatchRecord(kind, batch string, try int64, kv ...any) StgRecord {
	r := StgRecord{"v": "1", "kind": kind, "scope": "batch", "batch": batch, "try": try}
	for i := 0; i < len(kv); i += 2 {
		r[kv[i].(string)] = kv[i+1]
	}
	return r
}
func appendStg(ctx context.Context, tx pgx.Tx, r StgRecord) (Appended, error) {
	return Append(ctx, tx, Event{Stream: StgReleaseStream, Type: "note", Actor: "controller", Body: map[string]any{"text": "STG release " + r.String("kind"), "stg_release": map[string]any(r)}})
}
func stgMemberValues(ms []StgMemberKey) []any {
	out := []any{}
	for _, m := range ms {
		out = append(out, map[string]any{"root": m.Root, "op": m.Op, "R": m.R})
	}
	return out
}

func stgRecordRefusal(ctx context.Context, tx pgx.Tx, s StgState, batch string, obs StgGateObservation, f stgGateFacts, at time.Time, d *StgGateDecision) error {
	if !slices.Contains([]string{"rule", "diff_unlanded", "non_linear", "no_merge_after_landing", "baseline_unknown", "baseline_moved", "budget_exhausted", "hard_floor", "wrong_input", "ci_red", "ci_results"}, d.Why) {
		return nil
	}
	b := s.Batches[batch]
	snap := b.Tries[b.Try].Snapshot.Record
	base := snap.object("base")
	versions := map[string]any{}
	for tenant, r := range f.Rules {
		if r != nil {
			versions[tenant] = r.Version
		} else {
			versions[tenant] = 0
		}
	}
	version, _ := canon.Sum(map[string]any{"rules": versions, "ci": obs.CI.UID, "baseline": d.Evidence})
	id := StgBlockID(snap.object("inputs").String("revision"), base.String("P"), d.Why, version)
	d.Block = id
	if s.Blocks[id].IsOpen() {
		return nil
	}
	detail := map[string]any{"members": stgMemberValues(d.Set), "rules": versions}
	if d.Why == "hard_floor" {
		detail["members"] = stgMemberValues(d.HardFloor)
	}
	// CI blockers require the rejected selection's precise identity when it
	// is available. Unknown observations still block without invented facts.
	if ci, err := obs.CI.Record(); err == nil {
		detail["ci"] = map[string]any{"uid": ci["uid"], "created_at": ci["created_at"], "read_at": ci["read_at"]}
	} else if obs.CI.UID != "" && !obs.CI.CreatedAt.IsZero() && !obs.CI.CreatedAt.After(obs.CI.ReadAt) {
		detail["ci"] = map[string]any{"uid": obs.CI.UID, "created_at": obs.CI.CreatedAt.Format(time.RFC3339Nano), "read_at": obs.CI.ReadAt.Format(time.RFC3339Nano)}
	}
	a, err := appendStg(ctx, tx, StgRecord{"v": "1", "kind": "blocked", "scope": "block", "block": id, "why": d.Why, "P": snap.object("inputs")["revision"], "base": map[string]any(base), "first_at": at.Format(time.RFC3339Nano), "evidence_version": version, "detail": detail})
	if err != nil {
		return err
	}
	if d.Why == "hard_floor" {
		// Only members already known to the ledger can be withheld. A later
		// unbatched hard result remains blocked until B5-e admits its identity.
		known := true
		for _, m := range d.HardFloor {
			_, ok := s.Members[m]
			known = known && ok && s.Withheld[m].Seq == 0
		}
		if known {
			_, err = appendStg(ctx, tx, StgRecord{"v": "1", "kind": "withheld", "scope": "grant", "grant": "hard-floor:" + id, "members": stgMemberValues(d.HardFloor), "why": "hard_floor", "trigger_seq": a.Seq})
		}
	}
	return err
}

// StgPermitCurrent is read-only and deliberately independent of current
// rules: a committed licence lasts until its deadline or lease generation
// ends. A stale/replaced/settled permit cannot start a delayed Create.
func StgPermitCurrent(ctx context.Context, db DB, p StgSendPermit, generation int64, now time.Time) (bool, error) {
	if generation != p.Generation || !now.Before(p.Deadline) {
		return false, nil
	}
	s, err := ReadStgRelease(ctx, db)
	if err != nil {
		return false, err
	}
	b := s.Batches[p.Batch]
	if b == nil || b.Phase == "CLOSED" || b.Try != p.Try {
		return false, nil
	}
	t := b.Tries[p.Try]
	return t != nil && t.Occupied() && !t.IsQuarantined() && t.Bound.Seq == 0 && t.Started.Seq == p.IntentSeq && t.Permit.Seq == p.PermitSeq && t.Permit.Record.Time("deadline").Equal(p.Deadline) && t.Permit.Record.object("lease").Int("gen") == p.Generation && reflect.DeepEqual(t.Snapshot.Record.object("inputs"), p.Inputs) && reflect.DeepEqual(t.Started.Record, p.Started), nil
}
