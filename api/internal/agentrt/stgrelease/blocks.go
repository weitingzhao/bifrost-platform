package stgrelease

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func memberValue(m store.StgMemberKey) map[string]any {
	return map[string]any{"root": m.Root, "op": m.Op, "R": m.R}
}
func sortMembers(ms []any) {
	sort.Slice(ms, func(i, j int) bool {
		a, _ := canon.Encode(ms[i])
		b, _ := canon.Encode(ms[j])
		return string(a) < string(b)
	})
}
func members(b *store.StgBatch) []any {
	out := []any{}
	for m := range b.Members {
		out = append(out, memberValue(m))
	}
	sortMembers(out)
	return out
}
func cover(s store.StgState, m store.StgMemberKey) string {
	ids := []string{}
	for key := range s.Covers {
		if key.Member == m {
			ids = append(ids, key.Evidence)
		}
	}
	sort.Strings(ids)
	if len(ids) > 0 {
		return ids[0]
	}
	return ""
}
func orderedEvidence(s store.StgState) []store.StgEvent {
	out := []store.StgEvent{}
	for _, e := range s.Evidence {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq > out[j].Seq })
	return out
}
func reversal(s store.StgState, m store.StgMemberKey) int64 {
	for _, r := range s.Records {
		if r.Record.String("kind") != "reverted" || r.Record.String("op") != m.Op || r.Record.String("R") != m.R {
			continue
		}
		for _, op := range s.Legacy.Operations {
			if op.EverLanded && op.R == r.Record.String("by") {
				return r.Seq
			}
		}
	}
	return 0
}

// finish emits independent member outcomes, then settles only when all have
// one. Unknown validity on a verified batch leaves that member waiting while
// other effective members receive their delivery records immediately.
func finish(s store.StgState, id string, b *store.StgBatch, ref int64, verified bool) []store.StgRecord {
	t := b.Tries[b.Try]
	consumeEvidence := !verified && t != nil && t.Verify.Record.String("outcome") == "superseded"
	return finishMembers(s, id, b, ref, verified, consumeEvidence)
}

func finishMembers(s store.StgState, id string, b *store.StgBatch, ref int64, verified, consumeEvidence bool) []store.StgRecord {
	rs := []store.StgRecord{}
	complete := true
	t := b.Tries[b.Try]
	for _, value := range members(b) {
		v := store.StgRecord(value.(map[string]any))
		m := store.StgMemberKey{Root: v.String("root"), Op: v.String("op"), R: v.String("R")}
		if s.Members[m].To != "" {
			continue
		}
		to := "released"
		var proof any = ref
		if covered := cover(s, m); covered != "" {
			to, proof = "covered", covered
		} else if evidence := uncoveredEvidence(s, m); consumeEvidence && evidence != "" {
			rs = append(rs, store.StgRecord{"v": "1", "scope": "ext", "kind": "covers", "evidence": evidence, "root": m.Root, "op": m.Op, "R": m.R})
			to, proof = "covered", evidence
		} else if rev := reversal(s, m); rev > 0 {
			to, proof = "disposed", rev
		} else if verified {
			p := object(t.Snapshot.Record, "inputs").String("revision")
			valid := s.Validity[store.StgValidityKey{Member: m, P: p}].Record
			if valid.String("validity") != "effective" || valid["has_r"] != true {
				complete = false
				continue
			}
			to, proof = "delivered", t.Verify.Seq
		} else if s.Withheld[m].Seq == 0 && s.Budget[store.StgBudgetKey{R: m.R, Gen: b.Members[m]}] >= 3 {
			to = "exhausted"
		}
		rs = append(rs, batchRecord("member", id, 0, "root", m.Root, "op", m.Op, "R", m.R, "to", to, "ref", proof))
	}
	if complete {
		rs = append(rs, batchRecord("settled", id, 0, "members", members(b), "why", "members_settled"))
	}
	return rs
}

func (c *Controller) blockAndFinish(ctx context.Context, s store.StgState, id string, b *store.StgBatch, why string, base store.StgRecord, ci *store.StgCIObservation, checks ...store.StgRecord) error {
	p := b.Selected.Record.String("P")
	settlement := func(ref int64) []store.StgRecord {
		if why == "budget_exhausted" {
			return finishMembers(s, id, b, ref, false, true)
		}
		return finish(s, id, b, ref, false)
	}
	// Reuse an interval already opened by the final gate, preserving its time
	// and trigger even when a later scan has different diagnostic detail.
	for _, block := range s.Blocks {
		r := block.Opened.Record
		if block.IsOpen() && r.String("P") == p && r.String("why") == why && object(r, "base").String("P") == base.String("P") {
			return c.write(ctx, s, append(checks, settlement(block.Opened.Seq)...)...)
		}
	}
	detail := map[string]any{"members": members(b)}
	versions := map[string]any{}
	if rule := c.Policy().Rule; rule != nil {
		versions[rule.Landing.Tenant] = rule.Version
	}
	detail["rules"] = versions
	identity := map[string]any{"rules": versions, "base": base.String("evidence")}
	if b.Try > 0 {
		identity["inputs"] = b.Tries[b.Try].Snapshot.Record["inputs"]
	}
	if ci == nil {
		var latest store.StgEvent
		for _, accepted := range b.CI {
			if accepted.Seq > latest.Seq {
				latest = accepted
			}
		}
		if latest.Seq > 0 {
			cr := object(latest.Record, "ci")
			ci = &store.StgCIObservation{UID: cr.String("uid"), CreatedAt: cr.Time("created_at"), ReadAt: cr.Time("read_at")}
		}
	}
	if ci != nil && ci.UID != "" && !ci.CreatedAt.IsZero() && !ci.ReadAt.IsZero() {
		identity["ci"] = ci.UID
		detail["ci"] = map[string]any{"uid": ci.UID, "created_at": ci.CreatedAt.Format(time.RFC3339Nano), "read_at": ci.ReadAt.Format(time.RFC3339Nano)}
	}
	version, _ := canon.Sum(identity)
	block := store.StgBlockID(p, base.String("P"), why, version)
	r := store.StgRecord{"v": "1", "scope": "block", "kind": "blocked", "block": block, "why": why, "P": p, "base": map[string]any(base), "first_at": c.now().Format(time.RFC3339Nano), "evidence_version": version, "detail": detail}
	rs := append(checks, r)
	rs = append(rs, settlement(s.LastSeq+int64(len(rs)))...)
	return c.write(ctx, s, rs...)
}

func unblockRecord(id, why string, ref any) store.StgRecord {
	return store.StgRecord{"v": "1", "scope": "block", "kind": "unblocked", "block": id, "why": why, "ref": ref}
}
func unblockRecorded(s store.StgState) []store.StgRecord {
	ids := []string{}
	for id, b := range s.Blocks {
		if b.IsOpen() {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		for _, ev := range orderedRecords(s) {
			if ev.Seq <= s.Blocks[id].Opened.Seq {
				continue
			}
			for _, r := range []store.StgRecord{unblockRecord(id, "inputs_changed", ev.Seq), unblockRecord(id, "rule_changed", ev.Seq), unblockRecord(id, "external_release", ev.Record.String("evidence")), unblockRecord(id, "granted", ev.Record.String("grant"))} {
				if store.StgCanUnblock(s, r) {
					return []store.StgRecord{r}
				}
			}
		}
	}
	return nil
}
func orderedRecords(s store.StgState) []store.StgEvent {
	out := []store.StgEvent{}
	for _, r := range s.Records {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}
func (c *Controller) readBlock(ctx context.Context, s store.StgState, block store.StgBlock) error {
	r := block.Opened.Record
	why := r.String("why")
	if why == "wrong_input" || why == "diff_unlanded" || why == "non_linear" || why == "no_merge_after_landing" || why == "baseline_moved" || why == "baseline_unknown" {
		if changed, err := c.readBlockHead(ctx, s, block); changed || err != nil {
			return err
		}
	}
	if why == "rule" {
		rule := c.Policy().Rule
		if rule != nil {
			proof := store.StgRecord{"v": "1", "scope": "block", "kind": "rule_observed", "block": r["block"], "interval_seq": block.Opened.Seq, "rule_id": rule.ID, "rule_version": rule.Version, "read_at": c.now().Format(time.RFC3339Nano)}
			if store.StgCanObserveRule(s, proof, c.now()) {
				return c.write(ctx, s, proof, unblockRecord(r.String("block"), "rule_changed", s.LastSeq+1))
			}
		}
	}
	if why != "wrong_input" && !strings.HasPrefix(why, "ci_") || c.Sources.CI == nil {
		return nil
	}
	ci, err := c.Sources.CI(ctx, r.String("P"))
	if err != nil {
		return err
	}
	accepted, err := ci.Record()
	if err != nil {
		return nil
	}
	proof := store.StgRecord{"v": "1", "scope": "block", "kind": "ci_observed", "block": r["block"], "interval_seq": block.Opened.Seq, "P": r["P"], "ci": accepted, "status": "Succeeded"}
	if !store.StgCanObserveCI(s, proof, c.now()) {
		return nil
	}
	return c.write(ctx, s, proof, unblockRecord(r.String("block"), "inputs_changed", s.LastSeq+1))
}

// Consume existing facts only. Fetching new external runs and historic Git
// ancestry is B5-f; a member without an effective proof at this evidence's P
// is released independently, never inferred from another member.
func uncoveredEvidence(s store.StgState, m store.StgMemberKey) string {
	if s.Withheld[m].Seq > 0 || reversal(s, m) > 0 {
		return ""
	}
	for _, ev := range orderedEvidence(s) {
		valid := s.Validity[store.StgValidityKey{Member: m, P: ev.Record.String("P")}].Record
		if valid.String("validity") == "effective" && valid["has_r"] == true {
			return ev.Record.String("evidence")
		}
	}
	return ""
}
