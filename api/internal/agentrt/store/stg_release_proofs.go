package store

import "strings"

func stgRunUID(id string) string {
	return strings.Split(id, "/")[2] // parsed evidence identity
}

func (s StgState) deploymentObservation(id string) StgEvent {
	if e := s.DeploymentChecks[id]; e.Seq > 0 {
		return e
	}
	return s.Evidence[id]
}

// Unblock references are recorded proofs, never free-form revision strings.
// The conservative table implements only changes this stream can prove.
// Rule versions, arbitrary Git ancestry and unrecorded CI observations need
// B5-c/e acquisition contracts before they can close an interval.
func (s StgState) checkUnblock(b StgBlock, r StgRecord) error {
	fail := func(why string) error { return stgError("transition", "unblocked", why) }
	original, why := b.Opened.Record, r.String("why")
	allowed := false
	switch original.String("why") {
	case "wrong_input", "ci_red", "ci_results", "ci_missing":
		allowed = why == "inputs_changed"
	case "diff_unlanded", "non_linear", "no_merge_after_landing", "baseline_moved", "baseline_unknown":
		allowed = why == "inputs_changed" || why == "external_release" || why == "evidence_changed"
	case "hard_floor":
		allowed = why == "external_release"
	case "rule":
		allowed = why == "rule_changed"
	case "budget_exhausted":
		allowed = why == "granted"
	}
	if !allowed {
		return fail("unblock reason does not resolve the original cause")
	}
	switch why {
	case "inputs_changed":
		seq, _ := cardNumber(r["ref"])
		proof := s.Records[seq]
		if proof.Seq <= b.Opened.Seq {
			return fail("input change requires a new recorded observation")
		}
		p := proof.Record
		if strings.HasPrefix(original.String("why"), "ci_") || original.String("why") == "wrong_input" && p.String("kind") == "ci_accepted" {
			ci := p.object("ci")
			if p.String("kind") != "ci_accepted" || p.String("P") != original.String("P") || ci.Time("read_at").Before(b.Opened.At) || ci.Time("created_at").Before(b.Opened.At) {
				return fail("CI unblock needs a newly accepted CI for the blocked P")
			}
			for _, old := range s.Records {
				if old.Seq <= b.Opened.Seq && old.Record.String("kind") == "ci_accepted" && old.Record.object("ci").String("uid") == ci.String("uid") {
					return fail("CI identity has not changed")
				}
			}
			return nil
		}
		// landing_main v2 is an existing, validated Git observation. Require
		// its actual head and read time, not an arbitrary SHA in ref.
		if p.String("v") != "2" || p.String("head") == "" || p.String("head") == original.String("P") || p.Time("read_at").Before(b.Opened.At) {
			return fail("input unblock needs an observed different main head")
		}
		return nil
	case "granted":
		g := s.Grants[r.String("ref")]
		if g.Seq <= b.Opened.Seq || g.Record.Int("trigger_seq") != b.Opened.Seq {
			return fail("unblock must name this interval's grant or withholding")
		}
		return nil
	case "external_release", "evidence_changed":
		ev := s.Evidence[r.String("ref")]
		base := original.object("base")
		// Equality to the blocked P proves the target was reached without
		// inventing ancestry. A different P needs a future Git proof.
		if ev.Seq <= b.Opened.Seq || ev.Record.String("source") != "external" || ev.Record.String("P") != original.String("P") || ev.Record.object("deployed").Time("at").Before(b.Opened.At) || ev.Record.String("evidence") == base.String("evidence") || ev.Record.String("P") == base.String("P") && ev.Record.object("content").String("api_digest") == base.String("api_digest") && ev.Record.object("content").String("console_digest") == base.String("console_digest") {
			return fail("external evidence must newly establish the blocked target as a changed baseline")
		}
		return nil
	default:
		return fail("no stored proof contract for this change")
	}
}

// Every permit uses the repository-wide blocking policy, including an already
// frozen SNAP and recovery of an unbound STARTED. Closed intervals also bind
// the replacement inputs/base: closing one must not enable the old identity
// in a later batch. Reconciliation edges deliberately do not call this gate.
func (s StgState) checkReleaseBlocks(b *StgBatch, kind string) error {
	fail := func(why string) error { return stgError("transition", kind, why) }
	for _, block := range s.Blocks {
		if block.IsOpen() {
			return fail("repository has an open blocking interval")
		}
	}
	snap := b.Tries[b.Try].Snapshot.Record
	inputs := snap.object("inputs")
	for _, block := range s.BlockHistory {
		if block.Closed.Seq == 0 || inputs.String("revision") != block.Opened.Record.String("P") {
			continue
		}
		close := block.Closed.Record
		switch close.String("why") {
		case "inputs_changed":
			seq, _ := cardNumber(close["ref"])
			p := s.Records[seq].Record
			ci := p.object("ci")
			if p.String("kind") != "ci_accepted" || b.CI[ci.String("uid")].Seq == 0 || inputs.String("uiRevision") != ci.String("ui_commit") || inputs.String("infraRevision") != ci.String("infra_commit") {
				return fail("permit reused inputs from a resolved blocking interval")
			}
		case "external_release", "evidence_changed":
			if snap.object("base").String("evidence") != close.String("ref") {
				return fail("permit reused the baseline from a resolved blocking interval")
			}
		}
	}
	return nil
}
