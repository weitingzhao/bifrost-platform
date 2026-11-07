package stgrelease

import (
	"context"
	"fmt"
	"maps"
	"slices"
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
	return c.blockAndFinishDetail(ctx, s, id, b, why, base, ci, nil, checks...)
}

// blockRule opens (or ends the batch against) a "rule" block. reason is
// WindowPolicy.disabled as the batch read it: detail.rule_disabled, logged
// once at WARN when the interval opens.
func (c *Controller) blockRule(ctx context.Context, s store.StgState, id string, b *store.StgBatch, base store.StgRecord, reason map[string]any) error {
	return c.blockAndFinishDetail(ctx, s, id, b, "rule", base, nil, map[string]any{"rule_disabled": reason})
}

func (c *Controller) blockAndFinishDetail(ctx context.Context, s store.StgState, id string, b *store.StgBatch, why string, base store.StgRecord, ci *store.StgCIObservation, extra map[string]any, checks ...store.StgRecord) error {
	p := b.Selected.Record.String("P")
	settlement := func(ref int64) []store.StgRecord {
		if why == "budget_exhausted" {
			return finishMembers(s, id, b, ref, false, true)
		}
		return finish(s, id, b, ref, false)
	}
	r, err := c.blockedRecord(b, why, base, ci, extra)
	if err != nil {
		return err
	}
	// Match normalized protocol identities, preserving the original interval
	// and trigger even when a later scan has different diagnostic detail.
	for _, block := range s.Blocks {
		prior := block.Opened.Record
		if block.IsOpen() && prior.String("P") == p && prior.String("why") == r.String("why") && object(prior, "base").String("P") == object(r, "base").String("P") {
			return c.write(ctx, s, append(checks, settlement(block.Opened.Seq)...)...)
		}
	}
	rs := append(checks, r)
	rs = append(rs, settlement(s.LastSeq+int64(len(rs)))...)
	if err := c.write(ctx, s, rs...); err != nil {
		return err
	}
	if reason, ok := extra["rule_disabled"].(map[string]any); ok {
		args := []any{"batch", id, "P", p, "block", r.String("block")}
		for _, k := range slices.Sorted(maps.Keys(reason)) {
			args = append(args, k, reason[k])
		}
		c.logNotice("STG release blocked: the STG window policy reads not enabled", args...)
	}
	return nil
}

// blockedRecord is the blocked{why} record for batch b's selected P: its
// members, rule versions and the CI identity the refusal names (the batch's
// latest accepted CI when ci is nil). sent, when not nil, marks a refusal
// found after that send (detail.sent: batch, try, permit, runs); a "rule"
// refusal names the part of the policy that failed (detail.rule_disabled).
// extra is merged into detail and is not part of the interval's identity.
func (c *Controller) blockedRecord(b *store.StgBatch, why string, base store.StgRecord, ci *store.StgCIObservation, extra map[string]any) (store.StgRecord, error) {
	p := b.Selected.Record.String("P")
	detail := map[string]any{"members": members(b)}
	for k, v := range extra {
		if v != nil {
			detail[k] = v
		}
	}
	versions := map[string]any{}
	if rule := c.Policy().Rule; rule != nil {
		versions[rule.Landing.Tenant] = rule.Version
	}
	detail["rules"] = versions
	identity := map[string]any{"rules": versions, "base": base.String("evidence")}
	if b.Try > 0 {
		identity["inputs"] = b.Tries[b.Try].Snapshot.Record["inputs"]
	}
	if ci != nil {
		// Not part of the interval's identity: a later read of the same
		// wait has another age.
		if scope := ci.ScopeDetail(); scope != nil {
			detail["ci_scope"] = scope
		}
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
	return store.StgBlockedRecord(p, why, base, c.now(), version, detail)
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
			for _, r := range []store.StgRecord{unblockRecord(id, "inputs_changed", ev.Seq), unblockRecord(id, "rule_changed", ev.Seq), unblockRecord(id, "external_release", ev.Record.String("evidence")), unblockRecord(id, "granted", ev.Record.String("grant")),
				unblockRecord(id, "superseded", ev.Seq), unblockRecord(id, "superseded", ev.Record.String("evidence"))} {
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
	// A ci_* block on a P that main has moved past lifts as superseded
	// (unblockRecorded) once a newer head is recorded: acquire that proof.
	if why == "wrong_input" || strings.HasPrefix(why, "ci_") || why == "diff_unlanded" || why == "non_linear" || why == "no_merge_after_landing" || why == "baseline_moved" || why == "baseline_unknown" {
		if changed, err := c.readBlockHead(ctx, s, block); changed || err != nil {
			return err
		}
	}
	if why == "rule" {
		// Read as batch reads it before opening a "rule" block: live is
		// the send's own check. An enabled read lifts the block even for the
		// blocked rule version; a disabled one never does.
		p := c.Policy()
		p.Live = true
		if rule := p.Rule; rule != nil {
			proof := store.StgRecord{"v": "1", "scope": "block", "kind": "rule_observed", "block": r["block"], "interval_seq": block.Opened.Seq, "rule_id": rule.ID, "rule_version": rule.Version, "read_at": c.now().Format(time.RFC3339Nano), "enabled": p.enabled(c.now())}
			if store.StgCanObserveRule(s, proof, c.now()) {
				return c.write(ctx, s, proof, unblockRecord(r.String("block"), "rule_changed", s.LastSeq+1))
			}
		}
	}
	if why != "wrong_input" && !strings.HasPrefix(why, "ci_") || c.Sources.CI == nil {
		return nil
	}
	ci, readErr := c.Sources.CI(ctx, r.String("P"))
	if readErr == nil {
		accepted, err := ci.Record()
		if err == nil {
			proof := store.StgRecord{"v": "1", "scope": "block", "kind": "ci_observed", "block": r["block"], "interval_seq": block.Opened.Seq, "P": r["P"], "ci": accepted, "status": "Succeeded"}
			if !store.StgCanObserveCI(s, proof, c.now()) {
				return nil
			}
			return c.write(ctx, s, proof, unblockRecord(r.String("block"), "inputs_changed", s.LastSeq+1))
		}
		if ci.Reason == "" {
			return nil // red, missing or running: a verdict, not an unreadable record
		}
		_, err = c.ciUnreadable(ctx, s, block, fmt.Errorf("%s: %w", ci.Reason, err))
		return err
	}
	if lifted, err := c.ciUnreadable(ctx, s, block, readErr); lifted || err != nil {
		return err
	}
	return readErr
}

// ciUnreadable: the CI record of a ci_* block's P cannot be read. It is
// retried for store.StgCIUnreadableAfter from the block's opening, then the
// block lifts with ci_unreadable, keeping the error, and the next batch
// evaluates main's P again instead of waiting forever.
func (c *Controller) ciUnreadable(ctx context.Context, s store.StgState, block store.StgBlock, readErr error) (bool, error) {
	r := block.Opened.Record
	now := c.now()
	if !strings.HasPrefix(r.String("why"), "ci_") || now.Sub(block.Opened.At) < store.StgCIUnreadableAfter {
		return false, nil
	}
	text := strings.TrimSpace(readErr.Error())
	if len(text) > store.StgCIErrorMax {
		text = strings.ToValidUTF8(text[:store.StgCIErrorMax], "")
	}
	if text == "" {
		text = "CI record unreadable"
	}
	proof := store.StgRecord{"v": "1", "scope": "block", "kind": "ci_unreadable", "block": r["block"], "interval_seq": block.Opened.Seq, "P": r["P"], "error": text, "read_at": now.Format(time.RFC3339Nano)}
	if !store.StgCanObserveCIUnreadable(s, proof, now) {
		return false, nil
	}
	if err := c.write(ctx, s, proof, unblockRecord(r.String("block"), "ci_unreadable", s.LastSeq+1)); err != nil {
		return false, err
	}
	c.logNotice("STG release block lifted: the CI of P could not be read for an interval", "block", r.String("block"), "why", r.String("why"), "P", r.String("P"), "error", text)
	return true, nil
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
