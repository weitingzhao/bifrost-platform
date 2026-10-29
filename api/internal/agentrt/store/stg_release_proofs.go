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
// Rule versions and arbitrary Git ancestry need B5-c/e acquisition contracts
// before they can close an interval. CI observations have an independent edge.
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
		if strings.HasPrefix(original.String("why"), "ci_") || original.String("why") == "wrong_input" && stgCIProofKind(p.String("kind")) {
			return s.checkCIProof(b, proof)
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

func stgCICause(why string) bool {
	return why == "wrong_input" || why == "ci_red" || why == "ci_results" || why == "ci_missing"
}

func stgCIProofKind(kind string) bool { return kind == "ci_accepted" || kind == "ci_observed" }

// read_at is the successful terminal/result read, not object creation. A run
// may start before the blocked transaction and finish after it. The interval
// binds the rejected observation and every accepted identity in its prefix.
func (s StgState) checkCIProof(b StgBlock, proof StgEvent) error {
	fail := func(why string) error { return stgError("transition", "ci_proof", why) }
	r, original := proof.Record, b.Opened.Record
	ci := r.object("ci")
	if !stgCIProofKind(r.String("kind")) || proof.Seq <= b.Opened.Seq || r.String("P") != original.String("P") || ci.String("platform_commit") != r.String("P") || !ci.Time("read_at").After(b.Opened.At) || ci.Time("read_at").After(proof.At) {
		return fail("CI unblock needs a new successful read for the blocked P")
	}
	if r.String("kind") == "ci_observed" && (r.String("block") != original.String("block") || r.Int("interval_seq") != b.Opened.Seq || r.String("status") != "Succeeded") {
		return fail("CI observation belongs to a different blocking interval")
	}
	for _, interval := range s.BlockHistory {
		rejected := interval.Opened.Record.object("detail").object("ci")
		if rejected.String("uid") == ci.String("uid") {
			return fail("CI identity has not changed")
		}
		if interval.Opened.Record.String("P") == r.String("P") && !ci.Time("created_at").After(rejected.Time("created_at")) {
			return fail("CI does not supersede the rejected selection")
		}
	}
	for _, old := range s.Records {
		if !stgCIProofKind(old.Record.String("kind")) || old.Seq == proof.Seq {
			continue
		}
		prior := old.Record.object("ci")
		if prior.String("uid") == ci.String("uid") {
			if old.Seq <= b.Opened.Seq {
				return fail("CI identity has not changed")
			}
			if !stgCIIdentityEqual(ci, prior) {
				return fail("CI observation changed an already recorded identity")
			}
			continue
		}
		if old.Record.String("P") == r.String("P") {
			if ci.Time("created_at").Before(prior.Time("created_at")) {
				return fail("CI predates an already recorded selection")
			}
			if ci.Time("created_at").Equal(prior.Time("created_at")) && (ci.String("ui_commit") != prior.String("ui_commit") || ci.String("infra_commit") != prior.String("infra_commit") || ci.String("name") >= prior.String("name")) {
				return fail("CI selection is ambiguous or loses the name tie-break")
			}
		}
	}
	return nil
}

// Re-reading an immutable CI may advance read_at, but cannot repurpose its UID
// with other commits, pipeline, repository, name or creation time.
func stgSameCI(a, b StgRecord) bool {
	return stgCIIdentityEqual(a, b) && !a.Time("read_at").Before(b.Time("read_at"))
}

func stgCIIdentityEqual(a, b StgRecord) bool {
	for _, key := range []string{"uid", "name", "created_at", "platform_commit", "ui_commit", "infra_commit", "pipeline_ref", "repo_url"} {
		if a.String(key) != b.String(key) {
			return false
		}
	}
	return true
}

// Every permit checks open intervals. Closed intervals remain immutable audit
// history. For a target P, input and baseline repairs are independent: only a
// newer verified proof in the SAME dimension supersedes an older constraint.
// Order by proof seq, not close seq (overlapping intervals may close backwards).
// Each effective proof still binds its full CI or baseline identity; an old
// UID/base cannot satisfy it merely by appearing in the same batch or P.
func (s StgState) checkReleaseBlocks(b *StgBatch, kind string) error {
	fail := func(why string) error { return stgError("transition", kind, why) }
	for _, block := range s.Blocks {
		if block.IsOpen() {
			return fail("repository has an open blocking interval")
		}
	}
	snap := b.Tries[b.Try].Snapshot.Record
	inputs := snap.object("inputs")
	effective := map[string]StgEvent{}
	for _, block := range s.BlockHistory {
		if block.Closed.Seq == 0 || inputs.String("revision") != block.Opened.Record.String("P") {
			continue
		}
		close := block.Closed.Record
		var proof StgEvent
		switch close.String("why") {
		case "inputs_changed":
			seq, _ := cardNumber(close["ref"])
			proof = s.Records[seq]
		case "external_release", "evidence_changed":
			proof = s.Evidence[close.String("ref")]
		default:
			continue // grant/withholding affects budget, never input identity
		}
		dimension := "base"
		if stgCICause(block.Opened.Record.String("why")) {
			dimension = "inputs"
		}
		if proof.Seq > effective[dimension].Seq {
			effective[dimension] = proof
		}
	}
	if p := effective["inputs"].Record; p != nil {
		ci := p.object("ci")
		accepted := b.CI[ci.String("uid")].Record.object("ci")
		if !stgCIProofKind(p.String("kind")) || !stgSameCI(accepted, ci) || inputs.String("uiRevision") != ci.String("ui_commit") || inputs.String("infraRevision") != ci.String("infra_commit") {
			return fail("permit reused inputs from a resolved blocking interval")
		}
	}
	if p := effective["base"].Record; p != nil {
		base := snap.object("base")
		if p.String("kind") != "evidence" || base.String("evidence") != p.String("evidence") || base.String("P") != p.String("P") || !stgSame(p["content"], map[string]any{"api_digest": base["api_digest"], "console_digest": base["console_digest"]}) {
			return fail("permit reused the baseline from a resolved blocking interval")
		}
	}
	return nil
}
