package store

import (
	"context"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// GrantEvidence is what B5 reads of a landed operation's grant (§8).
type GrantEvidence struct {
	GrantID, ApprovalID, Purpose    string
	STGRelease                      bool
	Task, Attempt, A, R, M0         string
	CandidateDigest, MaterialDigest string
	Waives                          []string
	ApprovedAt                      time.Time
	IntentSeq                       int64
	Action                          string
	ApprovalNumber                  int64
	ExpiresAt, ConsumedAt           time.Time
}

// LandingGrantEvidence is B5's read (§8): only an operation whose
// main_intent consumed a grant, and which then landed R (and was not
// reverted since), has evidence. B5 checks R and STGRelease itself, records
// its own release use elsewhere, and never writes grant records.
func LandingGrantEvidence(ctx context.Context, q pgx.Tx, repo, op string) (GrantEvidence, bool, error) {
	if !rules.GrantSupported(repo) {
		return GrantEvidence{}, false, nil
	}
	stream := "landing:" + repo
	s, err := LandingState(ctx, q, stream)
	if err != nil {
		return GrantEvidence{}, false, err
	}
	o, ok := s.Operations[op]
	if !ok || !o.EverLanded || o.Reverted || o.DryRun {
		return GrantEvidence{}, false, nil
	}
	_, order, err := readLandingGrants(ctx, q, stream)
	if err != nil {
		return GrantEvidence{}, false, err
	}
	for _, g := range order {
		if g.Op != op || g.ConsumedSeq == 0 || g.ConsumedOp != op || g.R != o.R {
			continue
		}
		return grantEvidenceOf(g), true, nil
	}
	return GrantEvidence{}, false, nil
}

// grantEvidenceOf is a consumed grant as evidence: the grant of the
// operation that consumed it (its lead's, for a batch member).
func grantEvidenceOf(g *grantView) GrantEvidence {
	approved, _ := grantTime(g.ApprovedAt)
	return GrantEvidence{GrantID: g.GrantID, ApprovalID: g.ApprovalID, Purpose: g.Purpose, STGRelease: g.STGRelease,
		Task: g.Task, Attempt: g.Attempt, A: g.A, R: g.R, M0: g.M0, CandidateDigest: g.CandidateDigest,
		MaterialDigest: g.MaterialDigest, Waives: slices.Clone(g.Waives), ApprovedAt: approved, IntentSeq: g.ConsumedSeq,
		Action: g.Action, ApprovalNumber: g.ApprovalNumber, ExpiresAt: g.exp, ConsumedAt: g.ConsumedAt}
}

// LandingGrantStatus is one grant as the status API shows it (C-6 reads it).
type LandingGrantStatus struct {
	GrantID        string    `json:"grant_id"`
	ApprovalID     string    `json:"approval_id"`
	ApprovalNumber int64     `json:"approval_number"`
	Action         string    `json:"action"`
	Op             string    `json:"op"`
	Waives         []string  `json:"waives"`
	STGRelease     bool      `json:"stg_release"`
	ApprovedAt     time.Time `json:"approved_at"`
	GrantedAt      time.Time `json:"granted_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	Revoked        bool      `json:"revoked"`
	RevokedAt      time.Time `json:"revoked_at,omitzero"`
	Consumed       bool      `json:"handed_to_push"`
	Refused        bool      `json:"refused"`
	RefusedReason  string    `json:"refused_reason,omitempty"`
	RefusedAt      time.Time `json:"refused_at,omitzero"`
	IntentSeq      int64     `json:"main_intent_seq,omitempty"`
	// Valid: the grant can be consumed now — usable on the stream, and its
	// operation still the root's latest, waiting on the same candidate.
	// Ended says why an unspent grant cannot be (review 698 R-03):
	// operation_discarded, operation_failed, operation_superseded, ...
	Valid         bool   `json:"valid"`
	Ended         string `json:"ended,omitempty"`
	CandidateHash string `json:"candidate_digest"`
	MaterialHash  string `json:"material_digest"`
}

// status is the grant as shown, beside its operation as the stream has it.
func (g *grantView) status(now time.Time, o landing.Operation, latest bool) LandingGrantStatus {
	st := g.statusOnly(now)
	if st.Valid {
		switch {
		case o.Phase != "awaiting_grant" && !o.AtMain():
			st.Valid, st.Ended = false, "operation_"+o.Phase
		case !latest:
			st.Valid, st.Ended = false, "operation_superseded"
		case o.R != g.R || o.M0 != g.M0 || o.A != g.A:
			st.Valid, st.Ended = false, "operation_changed"
		}
	}
	return st
}

func (g *grantView) statusOnly(now time.Time) LandingGrantStatus {
	return LandingGrantStatus{GrantID: g.GrantID, ApprovalID: g.ApprovalID, ApprovalNumber: g.ApprovalNumber, Action: g.Action, Op: g.Op,
		Waives: slices.Clone(g.Waives), STGRelease: g.STGRelease, ApprovedAt: g.approved, GrantedAt: g.granted, ExpiresAt: g.exp,
		Revoked: !g.RevokedAt.IsZero(), RevokedAt: g.RevokedAt, Consumed: g.ConsumedSeq != 0, IntentSeq: g.ConsumedSeq,
		Refused: !g.RefusedAt.IsZero(), RefusedReason: g.RefusedWhy, RefusedAt: g.RefusedAt,
		Valid: g.usableAt(now), CandidateHash: g.CandidateDigest, MaterialHash: g.MaterialDigest}
}

// Body is the grant as an event body value (the executor's, and tests').
func (g LandingGrant) Body() map[string]any { return toBody(g) }
