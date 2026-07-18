package operatequeue

const stateVersion = "2026-07-16"

const (
	StatusOpen   = "open"
	StatusClosed = "closed"
)

const (
	SourcePostCompletion = "post_completion"
	SourceManual         = "manual"
)

const (
	HandoffOneOff         = "one_off"
	HandoffRecurringSetup = "recurring_setup"
)

const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

// ValidLanes are optional operate lanes (D11).
var ValidLanes = map[string]bool{
	"governance":        true,
	"troubleshoot":      true,
	"release":           true,
	"business-advisory": true,
}

type Item struct {
	ID                 string   `json:"id"`
	ProgramID          string   `json:"program_id"`
	SourceLaneID       string   `json:"source_lane_id,omitempty"`
	Lane               string   `json:"lane,omitempty"` // legacy alias
	OperateLane        string   `json:"operate_lane,omitempty"`
	Title              string   `json:"title"`
	Description        string   `json:"description,omitempty"`
	HandoffKind        string   `json:"handoff_kind,omitempty"`
	Reason             string   `json:"reason,omitempty"`
	AgentTaskID        string   `json:"agent_task_id,omitempty"`
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	VerificationSteps  []string `json:"verification_steps,omitempty"`
	RiskLevel          string   `json:"risk_level,omitempty"`
	Owner              string   `json:"owner,omitempty"`
	DueAt              string   `json:"due_at,omitempty"`
	ExecutionJobID     string   `json:"execution_job_id,omitempty"`
	CompletionEvidence []string `json:"completion_evidence,omitempty"`
	Status             string   `json:"status"`
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          string   `json:"updated_at,omitempty"`
	ClosedAt           string   `json:"closed_at,omitempty"`
	Source             string   `json:"source,omitempty"`
	PendingID          string   `json:"pending_id,omitempty"`
	ApprovedBy         string   `json:"approved_by,omitempty"`
}

type FileRecord struct {
	Version string `json:"version"`
	Items   []Item `json:"items"`
}

type ListResponse struct {
	Open         []Item `json:"open"`
	RecentClosed []Item `json:"recent_closed"`
}

type EnqueueRequest struct {
	ProgramID          string   `json:"program_id"`
	SourceLaneID       string   `json:"source_lane_id,omitempty"`
	Lane               string   `json:"lane,omitempty"`
	OperateLane        string   `json:"operate_lane,omitempty"`
	Title              string   `json:"title"`
	Description        string   `json:"description,omitempty"`
	HandoffKind        string   `json:"handoff_kind,omitempty"`
	Reason             string   `json:"reason,omitempty"`
	AgentTaskID        string   `json:"agent_task_id,omitempty"`
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	VerificationSteps  []string `json:"verification_steps,omitempty"`
	RiskLevel          string   `json:"risk_level,omitempty"`
	Owner              string   `json:"owner,omitempty"`
	DueAt              string   `json:"due_at,omitempty"`
}

type ApprovalInjectParams struct {
	PendingID          string
	ProgramID          string
	SourceLaneID       string
	OperateLane        string
	Title              string
	Description        string
	HandoffKind        string
	Reason             string
	AgentTaskID        string
	AcceptanceCriteria []string
	VerificationSteps  []string
	RiskLevel          string
	Owner              string
	DueAt              string
	ApprovedBy         string
}

type CloseRequest struct {
	CompletionEvidence        []string `json:"completion_evidence"`
	PostFixVerificationPassed bool     `json:"post_fix_verification_passed,omitempty"`
}

type ExecutionRequest struct {
	ExecutionJobID string `json:"execution_job_id"`
}
