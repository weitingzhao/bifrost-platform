package approvals

import (
	"encoding/json"
	"time"
)

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
	// with the same lease is accepted once. The Owner can close it with
	// POST /approvals/{id}/resolve.
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
	ApprovalLine string `json:"approval_line,omitempty"`
	// Track is filled by public(): "law" for a change of law (law_amend),
	// "daily" for every other card. Owner 2026-11-23: a change of law is
	// approved through its own channel on Ops, apart from the daily cards.
	Track     string    `json:"track,omitempty"`
	Status    string    `json:"status"`
	Reason    string    `json:"reason"`
	Rollback  string    `json:"rollback,omitempty"`
	Requester string    `json:"requester"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	DecidedAt time.Time `json:"decided_at,omitempty"`
	// DecidedBy is who made the decision: "owner" (an admin token on the
	// approve or reject route), the requester's session for a withdrawal,
	// "platform" for a superseded or expired card. Empty on records decided
	// before 2026-11-24 (Crew and the workshop count the Owner's steps here).
	DecidedBy string `json:"decided_by,omitempty"`
	Channel   string `json:"channel,omitempty"`
	// ConfirmedNumber is the number the approver repeated (confirm_number),
	// stored when it matched: what an executor reads back to know the
	// number was checked (landing grants, coordinator v3 §5.6 item 4).
	ConfirmedNumber int    `json:"confirmed_number,omitempty"`
	RejectReason    string `json:"reject_reason,omitempty"`
	Result          any    `json:"result,omitempty"`
	Error           string `json:"error,omitempty"`
	// Resolution is how the Owner closed an unknown result. Nil until then.
	Resolution *Resolution `json:"resolution,omitempty"`

	Env       string            `json:"env,omitempty"`
	Summary   string            `json:"summary,omitempty"`
	KeyParams map[string]string `json:"key_params,omitempty"`
	// Runner is who executes after approval: platform, host or owner.
	Runner          string `json:"runner,omitempty"`
	RequesterThread string `json:"requester_thread,omitempty"`
	WorkID          string `json:"work_id,omitempty"`
	// OwnerClass is the requester's claim that this card is one of the six
	// classes that stay the Owner's (SixClasses); empty for a daily card.
	// Owner 2026-11-18/11-19 (laws 31, 46): only a change of law and the six
	// classes should reach him; the readings count the rest as the gap.
	OwnerClass string `json:"owner_class,omitempty"`
	// Drill marks a request made to exercise the pages and the notices (v4
	// §20.3 rule 8, §20.5 scenario 9; W-31 2.5 gap 2, Owner 2026-10-21): it
	// is never approved (approve answers 409), so it never runs; it says so
	// in its notice and is sent no reminder. It can be rejected to close it.
	Drill bool `json:"drill,omitempty"`
	// A Console admin is sending the exact GPU approval in the same interaction.
	// Only the first notice is delayed, and only while the card is pending.
	InlineGPUShutdown bool       `json:"inline_gpu_shutdown,omitempty"`
	Execution         *Execution `json:"execution,omitempty"`
	Deliveries        []Delivery `json:"deliveries,omitempty"`
	// extra is the JSON object keys this binary does not know. The store
	// writes them back. They are not part of the API.
	extra map[string]json.RawMessage
}

// Resolution is the Owner's close of an unknown result.
type Resolution struct {
	Outcome string    `json:"outcome"`
	Note    string    `json:"note"`
	At      time.Time `json:"at"`
	By      string    `json:"by"`
}

// Execution is everything after the decision. It is one block so it can move
// to the release queue or PG as a unit.
type Execution struct {
	// Deadline: an approval still waiting at this time expires.
	Deadline      time.Time `json:"deadline,omitempty"`
	Attempts      int       `json:"attempts"`
	LastRefusal   string    `json:"last_refusal,omitempty"`
	NextAttemptAt time.Time `json:"next_attempt_at,omitempty"`

	// TicketHost and TicketUntil: a host card (params.host) is claimed only
	// with a ticket, which the one-time code earned (ticket.go). The claim uses
	// the ticket up.
	// A pointer: stored records of every other card keep their bytes (the
	// stored format is pinned by a golden test), which a zero time would change.
	TicketHost  string     `json:"ticket_host,omitempty"`
	TicketUntil *time.Time `json:"ticket_until,omitempty"`

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
	// extra is the JSON object keys this binary does not know. The store
	// writes them back. They are not part of the API.
	extra map[string]json.RawMessage
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
	// extra is the JSON object keys this binary does not know. The store
	// writes them back. They are not part of the API.
	extra map[string]json.RawMessage
}

// open is true while the record may still change without a new request.
func (a Approval) open() bool {
	switch a.Status {
	case StatusPending, StatusApproved, StatusRunning, StatusUnknown:
		return true
	}
	return false
}

// closedAt is the latest thing that happened. A resolution is that moment:
// the decision time is older, and prune would otherwise drop a long-unknown
// record in the same update that closed it.
func (a Approval) closedAt() time.Time {
	if a.Resolution != nil {
		return a.Resolution.At
	}
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
	a.Track = TrackOf(a.Action)
	return a
}

// SixClasses are the classes that stay the Owner's whatever the rules
// (agent-config/CLAUDE.md; OWNER_LAW H1-H6).
var SixClasses = map[string]string{
	"H1": "trading and money",
	"H2": "credentials and secrets",
	"H3": "deleting or changing data",
	"H4": "Trade PROD release",
	"H5": "signed rules and the trust root",
	"H6": "auto mode rules",
}

// The two tracks of cards.
const (
	TrackLaw   = "law"
	TrackDaily = "daily"
)

// TrackOf names the track of an action's cards.
func TrackOf(action string) string {
	if action == "law_amend" {
		return TrackLaw
	}
	return TrackDaily
}

// DailyCardNote is what the requesting thread reads back when it builds a
// card that is neither a law ticket nor one of the six classes. LAW-6 二:
// only those two reach the Owner's phone; everything else that can be undone
// runs by recipe or signed rule, with a record. The note tells the thread at
// the moment it decides, not afterwards in a reading.
const DailyCardNote = "daily card: it goes to the Owner's phone although it is neither a law ticket nor one of the six classes " +
	"(LAW-6 二, target 0 a day). If it is one of the six, withdraw it and create it again with owner_class H1-H6. " +
	"If it can be undone within an hour, use a tier-B action or a recipe instead, and tell 5 Approvals which one is missing."

// toOwnerNote is empty for law tickets, six-class cards, drills and the
// Owner's own inline shutdown; DailyCardNote otherwise.
func toOwnerNote(a Approval, ownerInline bool) string {
	if a.Drill || ownerInline || a.OwnerClass != "" || TrackOf(a.Action) == TrackLaw {
		return ""
	}
	return DailyCardNote
}
