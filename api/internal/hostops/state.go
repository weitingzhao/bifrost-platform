package hostops

import (
	"errors"
	"time"
)

// SafetyState is an authenticated, durable snapshot. Zero/missing state is
// frozen. S1 only computes transitions; S2/S3 must commit them with fsync/CAS
// according to STATE_PROTOCOL.md before returning any success/permit.
type SafetyState struct {
	Present        bool      `json:"present"`
	Fresh          bool      `json:"fresh"`
	Environment    string    `json:"environment"`
	Epoch          uint64    `json:"epoch"`
	MaxEpoch       uint64    `json:"max_epoch"`
	Frozen         bool      `json:"frozen"`
	LocalStop      bool      `json:"local_stop"`
	MaxRuleVersion uint64    `json:"max_rule_version"`
	PolicyHead     string    `json:"policy_head"`
	Generation     uint64    `json:"generation"`
	Circuits       []Circuit `json:"circuits"`
}

func (s SafetyState) Check(environment string, version uint64, head string) error {
	if s.Frozen || s.LocalStop {
		return errors.New("safety state frozen or locally stopped")
	}
	return s.integrity(environment, version, head)
}

func (s SafetyState) integrity(environment string, version uint64, head string) error {
	if !s.Present || !s.Fresh || s.Epoch == 0 || s.Epoch != s.MaxEpoch || s.Generation == 0 || s.Environment != environment || s.MaxRuleVersion == 0 || version < s.MaxRuleVersion || head != s.PolicyHead || !validDigest(head) {
		return errors.New("frozen, stale, missing or rolled-back safety state")
	}
	return validateCircuits(s.Circuits)
}

func (s SafetyState) Freeze(authenticatedOperator bool, local bool) (SafetyState, error) {
	if !authenticatedOperator {
		return s, errors.New("operator authentication required")
	}
	n := s
	n.Frozen = true
	if local {
		n.LocalStop = true
	}
	// Stop remains available with damaged state. Preserve the evidence that
	// requires independent recovery: normalizing either epoch would let an
	// ordinary unfreeze signature turn this stop-only transition into repair.
	if s.Epoch == 0 || s.Epoch != s.MaxEpoch {
		return n, nil
	}
	if n.Epoch == ^uint64(0) {
		return s, errors.New("epoch exhausted; recovery required")
	}
	n.Epoch++
	n.MaxEpoch = n.Epoch
	return n, nil
}

type Unfreeze struct {
	SchemaVersion  int    `json:"schema_version"`
	Environment    string `json:"environment"`
	Epoch          uint64 `json:"epoch"`
	Generation     uint64 `json:"generation"`
	MinRuleVersion uint64 `json:"min_rule_version"`
	PolicyHead     string `json:"policy_head"`
	Reason         string `json:"reason"`
	EffectiveAt    string `json:"effective_at"`
	ExpiresAt      string `json:"expires_at"`
}

func (t Trust) Unfreeze(s SafetyState, signed Signed, p *Policy, now time.Time, clock ClockObservation) (SafetyState, error) {
	if p == nil || s.integrity(p.bundle.Meta.Environment, p.version, p.head) != nil || clock.Check(now) != nil {
		return s, errors.New("unfreeze requires intact state and healthy clock; recovery is separate")
	}
	if err := t.verify(signed, NamespaceUnfreeze); err != nil {
		return s, err
	}
	var u Unfreeze
	if err := Decode([]byte(signed.Text), &u); err != nil {
		return s, err
	}
	if p == nil || !s.Present || !s.Fresh || !s.Frozen || s.Epoch == 0 || s.Epoch != s.MaxEpoch || s.Generation == 0 || u.SchemaVersion != 1 || u.Environment != s.Environment || u.Environment != p.bundle.Meta.Environment || u.Epoch != s.Epoch || u.Generation != s.Generation || u.MinRuleVersion < s.MaxRuleVersion || p.version < u.MinRuleVersion || u.PolicyHead != s.PolicyHead || u.PolicyHead != p.head || u.Reason == "" || !current(u.EffectiveAt, u.ExpiresAt, now) || !current(p.bundle.Meta.EffectiveAt, p.bundle.Meta.ExpiresAt, now) {
		return s, errors.New("invalid, stale or replayed unfreeze")
	}
	n := s
	n.Frozen = false
	n.LocalStop = false
	n.MaxRuleVersion = p.version
	return n, nil
}

// Consumption survives output collection, restarts and the approval UI's
// closed-record cap. It binds one nonce to one immutable plan/execution.
type Consumption struct {
	Nonce             string `json:"nonce"`
	ExecutionID       string `json:"execution_id"`
	PlanHash          string `json:"plan_hash"`
	Generation        uint64 `json:"generation"`
	ApprovalExpiresAt string `json:"approval_expires_at"`
	TicketsExpireAt   string `json:"tickets_expire_at"`
	Terminal          bool   `json:"terminal"`
	RollbackComplete  bool   `json:"rollback_complete"`
	Unknown           bool   `json:"unknown"`
	ResultArchived    bool   `json:"result_archived"`
	AuditArchived     bool   `json:"audit_archived"`
}

func (c Consumption) CanCollect(now time.Time) bool {
	a, e1 := time.Parse(time.RFC3339, c.ApprovalExpiresAt)
	b, e2 := time.Parse(time.RFC3339, c.TicketsExpireAt)
	return e1 == nil && e2 == nil && c.Nonce != "" && c.ExecutionID != "" && validDigest(c.PlanHash) && c.Generation > 0 && now.After(a.Add(5*time.Second)) && now.After(b.Add(5*time.Second)) && c.Terminal && c.RollbackComplete && !c.Unknown && c.ResultArchived && c.AuditArchived
}

// BindNonce is a transaction proposal. An identical replay is lookup-only;
// never a new start. The caller must also keep execution/nonce uniqueness in
// its durable ledger and check all plan/epoch/generation facts before commit.
func BindNonce(records []Consumption, next Consumption) (bool, error) {
	if next.Nonce == "" || next.ExecutionID == "" || !validDigest(next.PlanHash) || next.Generation == 0 {
		return false, errors.New("invalid nonce binding")
	}
	for _, old := range records {
		if old.Nonce == next.Nonce || old.ExecutionID == next.ExecutionID {
			if old.Nonce == next.Nonce && old.ExecutionID == next.ExecutionID && old.PlanHash == next.PlanHash && old.Generation == next.Generation {
				return false, nil
			}
			return false, errors.New("nonce/execution already bound")
		}
	}
	return true, nil
}

// Permit is a short online revocation check, not an authorization ticket.
// Unknown or disconnected revocation state must not create one.
type Permit struct {
	PlanHash            string
	Epoch, Generation   uint64
	IssuedAt, ExpiresAt time.Time
}

func (p Permit) Check(s SafetyState, planHash string, now time.Time, clock ClockObservation) error {
	if clock.Check(now) != nil || s.Check(s.Environment, s.MaxRuleVersion, s.PolicyHead) != nil || p.PlanHash != planHash || !validDigest(planHash) || p.Epoch != s.Epoch || p.Generation != s.Generation || p.IssuedAt.After(now) || !now.Before(p.ExpiresAt) || !p.ExpiresAt.After(p.IssuedAt) || p.ExpiresAt.Sub(p.IssuedAt) > 5*time.Second {
		return errors.New("write permit missing, stale or revoked")
	}
	return nil
}
