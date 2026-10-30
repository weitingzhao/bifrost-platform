package store

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type StgRootStatus struct {
	Batch       string        `json:"batch"`
	Try         int64         `json:"try"`
	Phase       string        `json:"phase"`
	BudgetUsed  int64         `json:"budget_used"`
	BudgetGen   int64         `json:"budget_gen"`
	Blocked     *StgRootBlock `json:"blocked,omitempty"`
	Quarantined bool          `json:"quarantined"`
}

type StgRootBlock struct {
	Why   string    `json:"why"`
	Since time.Time `json:"since"`
}

func stgRootStatus(s StgState, m StgMemberKey) *StgRootStatus {
	member := s.Members[m]
	gen := s.generation(m.R)
	status := &StgRootStatus{Batch: member.Batch, Phase: "pending", BudgetGen: gen, BudgetUsed: s.Budget[StgBudgetKey{m.R, gen}]}
	if b := s.Batches[member.Batch]; b != nil {
		status.Try, status.Phase = b.Try, strings.ToLower(b.Phase)
		status.Quarantined = b.Tries[b.Try].IsQuarantined()
	}
	// Repository-wide blockers: choose the oldest, deterministically.
	var first StgEvent
	for _, b := range s.Blocks {
		if b.IsOpen() && (first.Seq == 0 || b.Opened.Seq < first.Seq) {
			first = b.Opened
		}
	}
	if first.Seq > 0 {
		status.Blocked = &StgRootBlock{Why: first.Record.String("why"), Since: first.Record.Time("first_at")}
	}
	switch {
	case member.To == "delivered" || s.covered(m):
		status.Phase = "delivered"
	case s.Withheld[m].Seq > 0:
		status.Phase = "withheld"
	case member.To == "disposed" || stgLandedReversal(s, m) > 0:
		status.Phase = "disposed"
	case status.Quarantined:
		status.Phase = "quarantined"
	case status.Blocked != nil:
		status.Phase = "blocked"
	case status.BudgetUsed >= 3:
		status.Phase = "exhausted"
	case member.To == "released":
		status.Phase = "pending"
	}
	return status
}

// Only stable disposition/proof/interval identities belong in a closing
// snapshot. A new read, debit, lease diagnostic or retry is not a new root.
func stgSnapshotKey(s StgState, m StgMemberKey) string {
	parts := []string{}
	member := s.Members[m]
	// member{covered} only consumes its already-recorded covers relation;
	// recording that consumption must not invalidate a round a second time.
	if member.To == "delivered" || member.To == "disposed" {
		parts = append(parts, fmt.Sprintf("%s:%v", member.To, member.Ref))
	} else if ref := stgLandedReversal(s, m); ref > 0 {
		parts = append(parts, fmt.Sprintf("disposed:%d", ref))
	}
	for key := range s.Covers {
		if key.Member == m {
			parts = append(parts, "covers:"+key.Evidence)
		}
	}
	if w := s.Withheld[m]; w.Seq > 0 {
		parts = append(parts, fmt.Sprintf("withheld:%s:%d", w.Record.String("why"), w.Seq))
	}
	if d := s.Disposals[m.Op]; d.Seq > 0 {
		parts = append(parts, fmt.Sprintf("disposed:%s:%d", d.Record.String("why"), d.Seq))
	}
	for id, b := range s.Blocks {
		if b.IsOpen() {
			parts = append(parts, fmt.Sprintf("block:%s:%d", id, b.Opened.Seq))
		}
	}
	sort.Strings(parts)
	return m.Root + "|" + m.Op + "|stg|" + strings.Join(parts, "|")
}

// classifyStgRoot implements §13's fallback order. Promotion to delivered
// from fresh main evidence is still performed against the entire mission's
// closing snapshot, after all repositories have been read.
func classifyStgRoot(c LandingRootClass, s StgState, m StgMemberKey, owner, dropped, ruleKnown bool, ev mainObservation, found bool) LandingRootClass {
	c.STG = stgRootStatus(s, m)
	c.key = stgSnapshotKey(s, m)
	member, withheld := s.Members[m], s.Withheld[m]
	delivery := member.To == "delivered" || s.covered(m)
	switch {
	case owner || withheld.Record.String("why") == "owner":
		c.Class, c.Reason = RootDisposed, "owner"
	case dropped:
		c.Class, c.Reason = RootDisposed, "dropped"
	case withheld.Record.String("why") == "hard_floor":
		c.Class, c.Reason = RootDisposed, "stg_withheld"
	case member.To == "disposed" || stgLandedReversal(s, m) > 0:
		c.Class, c.Reason = RootDisposed, "reverted"
	case !ruleKnown:
		c.Reason = "rule"
	case delivery:
		c.Reason, c.read = "landed_unverified", true
		if !found || ev.validity == "effective" {
			c.Reason, c.NeedsEvidence = "evidence", true
		}
	case c.STG.Quarantined || c.STG.Blocked != nil:
		c.Reason = "stg_blocked"
	case c.STG.BudgetUsed >= 3:
		c.Reason = "stg_exhausted"
	case s.Batches[member.Batch] != nil && s.Batches[member.Batch].Phase != "CLOSED":
		c.Reason = "stg_running"
	default:
		c.Reason = "stg_pending"
	}
	// Row 1 wins over an existing disposition or missing rule only when the
	// combined closing round proves current validity. A stale read cannot win.
	if delivery && found && ev.validity == "effective" {
		c.read, c.evidenceAt = true, ev.readAt
	}
	return c
}

// A root reversed before admission can never acquire an STG member. Consume
// the same durable proof required for member{disposed}, including the landed
// identity of the reversing commit. A manual revert alone is not a disposal.
// Admitted roots still wait for normal member settlement and execution checks.
func stgLandedReversal(s StgState, m StgMemberKey) int64 {
	if _, admitted := s.Members[m]; admitted {
		return 0
	}
	o := s.Legacy.Operations[m.Op]
	if !o.EverLanded || o.Task != m.Root || o.R != m.R {
		return 0
	}
	var ref int64
	for _, e := range s.Records {
		r := e.Record
		if r.String("kind") != "reverted" || r.String("op") != m.Op || r.String("R") != m.R {
			continue
		}
		for _, by := range s.Legacy.Operations {
			if by.EverLanded && by.R == r.String("by") && (ref == 0 || e.Seq < ref) {
				ref = e.Seq
			}
		}
	}
	return ref
}

func classifyStgInMain(c LandingRootClass, s StgState, ownerDelivered, ruleKnown bool) LandingRootClass {
	m := StgMemberKey{c.Root, c.Op, s.InMain[c.Op]}
	c.STG, c.key = stgRootStatus(s, m), stgSnapshotKey(s, m)
	if ownerDelivered && m.R != "" && s.covered(m) {
		c.Class, c.Reason = RootDelivered, "in_main"
	} else if !ruleKnown {
		c.Reason = "rule"
	} else if ownerDelivered {
		c.Reason = "stg_pending"
	} else {
		c.Reason = "in_main_unverified"
	}
	return c
}
