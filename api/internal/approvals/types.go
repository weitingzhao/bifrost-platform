package approvals

import "time"

const (
	StatusPending  = "pending"
	StatusExecuted = "executed"
	StatusFailed   = "failed"
	StatusRejected = "rejected"
	StatusExpired  = "expired"

	ttl = 24 * time.Hour
	// keepClosed is how many terminal approvals the state file retains.
	keepClosed = 500
)

// Approval is one request stored under statefile key "approvals".
type Approval struct {
	ID           string         `json:"id"`
	Action       string         `json:"action"`
	Tier         string         `json:"tier"`
	Params       map[string]any `json:"params"`
	ParamsHash   string         `json:"params_hash"`
	Status       string         `json:"status"`
	Reason       string         `json:"reason"`
	Rollback     string         `json:"rollback,omitempty"`
	Requester    string         `json:"requester"`
	CreatedAt    time.Time      `json:"created_at"`
	ExpiresAt    time.Time      `json:"expires_at"`
	DecidedAt    time.Time      `json:"decided_at,omitempty"`
	Channel      string         `json:"channel,omitempty"`
	RejectReason string         `json:"reject_reason,omitempty"`
	Result       any            `json:"result,omitempty"`
	Error        string         `json:"error,omitempty"`
}

func (a Approval) open() bool { return a.Status == StatusPending }

func (a Approval) closedAt() time.Time {
	if !a.DecidedAt.IsZero() {
		return a.DecidedAt
	}
	return a.CreatedAt
}
