package approvals

import "time"

const (
	StatusPending = "pending"
	// StatusApproved: decided, waiting for an executor. A transient refusal
	// (release window held, executor not reachable) also lands here and is
	// retried until execution.deadline; the Owner does not click again.
	StatusApproved = "approved"
	// StatusRunning: an executor holds a lease and has started.
	StatusRunning  = "running"
	StatusExecuted = "executed"
	StatusFailed   = "failed"
	StatusRejected = "rejected"
	StatusExpired  = "expired"
	// StatusUnknown: the lease lapsed past unknownGrace with no result. A
	// started run is never retried automatically; the first result posted
	// with the same lease is accepted once.
	StatusUnknown = "unknown"

	ttl = 24 * time.Hour
	// keepClosed is how many terminal approvals the state file retains.
	keepClosed = 500
	// keepTails is how many terminal approvals keep execution.output_tail.
	keepTails = 50
)

// Approval is one request stored under statefile key "approvals".
type Approval struct {
	ID string `json:"id"`
	// Number is the short global #n (execution card 4). 0 on records created
	// before numbering.
	Number     int            `json:"number,omitempty"`
	Action     string         `json:"action"`
	Tier       string         `json:"tier"`
	Params     map[string]any `json:"params"`
	ParamsHash string         `json:"params_hash"`
	// ApprovedLine is the canonical line the caller echoed when this request
	// was approved. Empty until then.
	ApprovedLine string `json:"approved_line,omitempty"`
	// ApprovalLine is filled by public() for readers. It is not stored.
	ApprovalLine string    `json:"approval_line,omitempty"`
	Status       string    `json:"status"`
	Reason       string    `json:"reason"`
	Rollback     string    `json:"rollback,omitempty"`
	Requester    string    `json:"requester"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	DecidedAt    time.Time `json:"decided_at,omitempty"`
	Channel      string    `json:"channel,omitempty"`
	RejectReason string    `json:"reject_reason,omitempty"`
	Result       any       `json:"result,omitempty"`
	Error        string    `json:"error,omitempty"`

	Env       string            `json:"env,omitempty"`
	Summary   string            `json:"summary,omitempty"`
	KeyParams map[string]string `json:"key_params,omitempty"`
	// Runner is who executes after approval: platform, host or owner.
	Runner          string     `json:"runner,omitempty"`
	RequesterThread string     `json:"requester_thread,omitempty"`
	WorkID          string     `json:"work_id,omitempty"`
	Execution       *Execution `json:"execution,omitempty"`
	Deliveries      []Delivery `json:"deliveries,omitempty"`
}

// Execution is everything after the decision. It is one block so it can move
// to the release queue or PG as a unit.
type Execution struct {
	// Deadline: an approval still waiting at this time expires.
	Deadline      time.Time `json:"deadline,omitempty"`
	Attempts      int       `json:"attempts"`
	LastRefusal   string    `json:"last_refusal,omitempty"`
	NextAttemptAt time.Time `json:"next_attempt_at,omitempty"`

	ExecutorID     string    `json:"executor_id,omitempty"`
	LeaseID        string    `json:"lease_id,omitempty"`
	LeaseExpiresAt time.Time `json:"lease_expires_at,omitempty"`
	ClaimedAt      time.Time `json:"claimed_at,omitempty"`

	StartedAt    time.Time `json:"started_at,omitempty"`
	FinishedAt   time.Time `json:"finished_at,omitempty"`
	ExitCode     *int      `json:"exit_code,omitempty"`
	DurationMS   int64     `json:"duration_ms,omitempty"`
	OutputSHA256 string    `json:"output_sha256,omitempty"`
	// OutputTail is at most 2 KB, redacted on write (execution card 6).
	OutputTail string `json:"output_tail,omitempty"`
	Error      string `json:"error,omitempty"`
	LateResult bool   `json:"late_result,omitempty"`
}

// Delivery is one notification attempt to one target (filled by S0-0b).
// Result is accepted (the push service took it), failed or skipped (no relay
// configured). Kind is which notice: created, failed, unknown, not_executed,
// reminder_waiting, reminder_expiring.
type Delivery struct {
	Kind    string    `json:"kind,omitempty"`
	Channel string    `json:"channel"`
	Target  string    `json:"target,omitempty"`
	At      time.Time `json:"at"`
	Result  string    `json:"result"`
	Error   string    `json:"error,omitempty"`
	// ClaimID and ClaimExpiresAt belong to a reminder claim. An expired claim
	// can be taken again. Finish and release match ClaimID.
	ClaimID        string    `json:"claim_id,omitempty"`
	ClaimExpiresAt time.Time `json:"claim_expires_at,omitempty"`
}

// open is true while the record may still change without a new request.
func (a Approval) open() bool {
	switch a.Status {
	case StatusPending, StatusApproved, StatusRunning, StatusUnknown:
		return true
	}
	return false
}

func (a Approval) closedAt() time.Time {
	if a.Execution != nil && !a.Execution.FinishedAt.IsZero() {
		return a.Execution.FinishedAt
	}
	if !a.DecidedAt.IsZero() {
		return a.DecidedAt
	}
	return a.CreatedAt
}

// public is the API view: the lease id is the executor's capability and is
// not shown to readers. approval_line is the canonical line for this record.
func (a Approval) public() Approval {
	if a.Execution != nil {
		e := *a.Execution
		e.LeaseID = ""
		a.Execution = &e
	}
	a.ApprovalLine = CanonicalApprovalLine(a)
	return a
}
