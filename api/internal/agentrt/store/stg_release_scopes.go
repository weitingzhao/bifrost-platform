package store

import (
	"strings"
)

func (s *StgState) applyOther(e StgEvent) error {
	r := e.Record
	k := r.String("kind")
	fail := func(code, why string) error { return stgError(code, k, why) }
	switch k {
	case "evidence":
		id := r.String("evidence")
		if _, ok := s.Evidence[id]; ok {
			return fail("duplicate", "evidence identity already exists")
		}
		if r.object("deployed").Time("at").After(e.At) {
			return fail("invalid", "deployment observation is in the future")
		}
		// Identity, not matching digests, decides provenance. A known local
		// run cannot be relabelled external by the other observer.
		var automatic *StgTry
		var batchID string
		uid := stgRunUID(id)
		for bid, b := range s.Batches {
			for _, t := range b.Tries {
				// Cluster is not stored in bound v1. Conservatively reserve a
				// known UID across clusters/namespaces until B5-b binds it.
				if t.Bound.Record.String("uid") == uid {
					if automatic != nil {
						return fail("conflict", "run UID is bound to multiple intents")
					}
					automatic = t
					batchID = bid
				}
			}
		}
		if automatic != nil && r.String("source") != "automatic" {
			return fail("conflict", "local automatic run is not external")
		}
		if r.String("source") == "automatic" {
			if automatic == nil || r.String("batch") != batchID || automatic.Started.Record.String("run") != r.String("run") || automatic.RunEvidence.Record.String("status") != "Succeeded" {
				return fail("transition", "automatic evidence needs its bound successful run")
			}
			inputs := automatic.Snapshot.Record.object("inputs")
			if r.object("deployed").Time("at").Before(automatic.RunEvidence.Record.Time("at")) {
				return fail("conflict", "deployment evidence predates the terminal run")
			}
			if strings.Split(id, "/")[1] != inputs.String("namespace") || r.String("P") != inputs.String("revision") || r.String("U") != inputs.String("uiRevision") || r.String("infra") != inputs.String("infraRevision") {
				return fail("conflict", "automatic evidence changed frozen inputs")
			}
		}
		s.Evidence[id] = e
	case "deployment_checked":
		// Append a fresh observation of an immutable evidence identity. The
		// adapter must recheck pin-sync, workloads and health against its
		// content; an old observation cannot be refreshed by changing its ID.
		id := r.String("evidence")
		proof, ok := s.Evidence[id]
		if !ok || !stgSame(r["content"], proof.Record["content"]) {
			return fail("conflict", "deployment check must match existing evidence content")
		}
		at := r.object("deployed").Time("at")
		if at.After(e.At) || !at.After(s.deploymentObservation(id).Record.object("deployed").Time("at")) {
			return fail("invalid", "deployment check must be a newer, non-future observation")
		}
		s.DeploymentChecks[id] = e
	case "covers":
		id := r.String("evidence")
		proof, ok := s.Evidence[id]
		m := stgMember(r)
		if !ok || !s.effective(m, proof.Record.String("P")) {
			return fail("transition", "coverage requires trusted evidence and effective R in its P")
		}
		key := StgCoverKey{id, m}
		if _, ok := s.Covers[key]; ok {
			return fail("duplicate", "coverage relation already exists")
		}
		if old := s.Members[m]; old.To == "disposed" {
			return fail("transition", "disposed member cannot be delivered")
		}
		if _, ok := s.Withheld[m]; ok {
			return fail("transition", "withheld member cannot be delivered")
		}
		s.Covers[key] = e
	case "granted", "withheld":
		id := r.String("grant")
		if _, ok := s.Grants[id]; ok {
			return fail("duplicate", "grant identity already used")
		}
		trigger := s.Records[r.Int("trigger_seq")]
		if trigger.Seq == 0 {
			return fail("transition", "trigger does not exist")
		}
		ms := r.members("members")
		if k == "granted" || r.String("why") == "owner" {
			choice := "do_not_release"
			if k == "granted" {
				choice = "retry_release"
			}
			if trigger.Record.String("kind") != "blocked" || trigger.Record.String("why") != "budget_exhausted" || !s.Blocks[trigger.Record.String("block")].IsOpen() || s.Blocks[trigger.Record.String("block")].Opened.Seq != trigger.Seq {
				return fail("transition", "Owner budget decision needs the current exhaustion interval")
			}
			if err := s.ownerAnswer(r.String("question_id"), trigger.Seq, choice, ms); err != nil {
				return err
			}
		}
		for _, m := range ms {
			key := m.StgMemberKey
			old, exists := s.Members[key]
			if !exists || s.covered(key) || old.To == "delivered" || old.To == "covered" || old.To == "disposed" {
				return fail("transition", "decision cannot revive an absent or disposed member")
			}
			if _, ok := s.Withheld[key]; ok {
				return fail("duplicate", "member already withheld")
			}
			if k == "withheld" && r.String("why") == "owner" && old.To != "exhausted" {
				return fail("transition", "Owner withholding requires an exhausted member")
			}
			if b := s.Batches[old.Batch]; k == "granted" && b != nil && b.Phase != "CLOSED" {
				return fail("transition", "member still belongs to an open batch")
			}
			if k == "granted" {
				if old.To != "exhausted" || r.Int("gen") != s.generation(m.R)+1 {
					return fail("budget", "grant requires exhausted members and their next generation")
				}
			} else if r.String("why") == "hard_floor" {
				// The matrix leaves detail opaque. Conservatively require a
				// controller's hard-floor observation naming this exact set;
				// a temporary rule/floor refusal never becomes permanent here.
				set := map[StgMemberKey]int64{}
				for _, member := range ms {
					set[member.StgMemberKey] = 0
				}
				detail := trigger.Record.object("detail")
				if trigger.Record.String("kind") != "blocked" || trigger.Record.String("why") != "hard_floor" || !stgMembers(detail["members"], false, false) || !stgSameMembers(detail.members("members"), set) {
					return fail("permission", "withholding requires a hard-floor observation of the exact members")
				}
			}
		}
		for _, m := range ms {
			key := m.StgMemberKey
			if k == "granted" {
				old := s.Members[key]
				old.Gen = r.Int("gen")
				old.To = "released"
				s.Members[key] = old
				s.Generations[m.R] = r.Int("gen")
			} else {
				s.Withheld[key] = e
			}
		}
		s.Grants[id] = e
	case "blocked":
		id := r.String("block")
		base := r.object("base")
		if id != StgBlockID(r.String("P"), base.String("P"), r.String("why"), r.String("evidence_version")) {
			return fail("conflict", "block identity does not match its inputs")
		}
		if s.Blocks[id].IsOpen() {
			return fail("duplicate", "blocking interval is already open")
		}
		if r.object("detail").object("ci").Time("read_at").After(e.At) {
			return fail("invalid", "blocked CI observation is in the future")
		}
		b := StgBlock{Opened: e}
		s.Blocks[id] = b
		s.BlockHistory[e.Seq] = b
	case "ci_observed":
		// Independent evidence survives settlement; it never enters a batch's
		// accepted CI set or changes its immutable snapshot.
		b := s.Blocks[r.String("block")]
		if !b.IsOpen() || b.Opened.Seq != r.Int("interval_seq") || !stgCICause(b.Opened.Record.String("why")) {
			return fail("transition", "CI observation requires its open CI blocking interval")
		}
		uid := r.object("ci").String("uid")
		if s.CIObservations[b.Opened.Seq][uid].Seq > 0 {
			return fail("duplicate", "CI UID already observed for this interval")
		}
		if err := s.checkCIProof(b, e); err != nil {
			return err
		}
		if s.CIObservations[b.Opened.Seq] == nil {
			s.CIObservations[b.Opened.Seq] = map[string]StgEvent{}
		}
		s.CIObservations[b.Opened.Seq][uid] = e
	case "unblocked":
		id := r.String("block")
		b := s.Blocks[id]
		if !b.IsOpen() {
			return fail("transition", "blocking interval is not open")
		}
		if err := s.checkUnblock(b, r); err != nil {
			return err
		}
		b.Closed = e
		s.Blocks[id] = b
		s.BlockHistory[b.Opened.Seq] = b
	default:
		return fail("invalid", "unknown scope kind")
	}
	return nil
}

// ownerAnswer fails closed until B5-g supplies a stored, applied STG card
// outcome. question_id/by alone are never accepted as Owner authorization.
func (s StgState) ownerAnswer(question string, trigger int64, choice string, members []StgMember) error {
	a := s.Answers[question].Record
	t := s.Records[trigger].Record
	wantKind := "stg_failed"
	if choice == "accept_unreconciled" {
		wantKind = "stg_blocked"
	}
	if a.String("kind") != wantKind || a.Int("trigger_seq") != trigger || a.String("outcome") != "applied" || a.String("choice") != choice || a.String("channel") != "console" || a.String("by") != "owner" || a.String("block") != t.String("block") || a.String("batch") != t.String("batch") {
		return stgError("permission", "owner_decision", "missing matching applied STG card answer")
	}
	// Budget answers bind the exact member set. Quarantine answers instead
	// bind the batch and quarantine sequence; neither can authorize a sibling.
	if members != nil {
		set := map[StgMemberKey]int64{}
		for _, m := range members {
			set[m.StgMemberKey] = 0
		}
		if !stgMembers(a["members"], false, false) || !stgSameMembers(a.members("members"), set) {
			return stgError("permission", "owner_decision", "answer members differ")
		}
	}
	return nil
}
