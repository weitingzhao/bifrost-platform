package operatequeue

const stateVersion = "2026-07-16"

const (
	StatusOpen   = "open"
	StatusClosed = "closed"
)

const (
	SourcePostCompletion    = "post_completion"
	SourceManual            = "manual"
	SourceChecklistDispatch = "checklist_dispatch"
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

// DismissRequest soft-closes a stale/resolved handoff without requiring a linked
// execution job to be done or post-fix verification. Evidence is still required.
type DismissRequest struct {
	CompletionEvidence []string `json:"completion_evidence"`
	Reason             string   `json:"reason,omitempty"` // stale | resolved | other
}

type ExecutionRequest struct {
	ExecutionJobID string `json:"execution_job_id"`
}

// Sweep / triage verdicts (deterministic Queue Drain Workflow).
const (
	VerdictStale         = "STALE"
	VerdictStillNeeded   = "STILL_NEEDED"
	VerdictNeedsDecision = "NEEDS_DECISION"
	VerdictInProgress    = "IN_PROGRESS"
	VerdictHeld          = "HELD"
)

const (
	DecisionApprovedRun = "approved_run"
	DecisionDismissed   = "dismissed"
	DecisionHold        = "hold"
)

const (
	SuggestionRUN     = "RUN"
	SuggestionDismiss = "DISMISS"
	SuggestionHold    = "HOLD"
)

type SweepRequest struct {
	AutoDrain bool `json:"auto_drain"` // true=start STILL_NEEDED serially; false=triage+dismiss stale only
}

type SweepResult struct {
	ItemID  string `json:"item_id"`
	Title   string `json:"title"`
	Verdict string `json:"verdict"` // STALE | STILL_NEEDED | NEEDS_DECISION | IN_PROGRESS | HELD
	Reason  string `json:"reason"`
}

type SweepResponse struct {
	Dismissed   []SweepResult   `json:"dismissed"`
	Queued      []SweepResult   `json:"queued"` // STILL_NEEDED (drain sequence)
	Decisions   []DecisionBrief `json:"decisions"`
	InProgress  []SweepResult   `json:"in_progress"`
	Held        []SweepResult   `json:"held,omitempty"`
	NextSweepAt string          `json:"next_sweep_at,omitempty"`
}

type DecisionBrief struct {
	ID               string `json:"id"`
	ItemID           string `json:"item_id"`
	Title            string `json:"title"`
	CreatedAt        string `json:"created_at"`
	FleetSignal      string `json:"fleet_signal"` // GO / NO-GO / unknown
	FleetDetail      string `json:"fleet_detail"`
	ItemAge          string `json:"item_age"`
	FixScope         string `json:"fix_scope"`
	RiskLevel        string `json:"risk_level"`
	Source           string `json:"source,omitempty"`
	Suggestion       string `json:"suggestion"` // RUN | DISMISS | HOLD
	SuggestionReason string `json:"suggestion_reason"`
	OpenQuestion     string `json:"open_question,omitempty"`
	FullBrief        string `json:"full_brief"`
	Decision         string `json:"decision,omitempty"` // approved_run | dismissed | hold | ""
	DecidedAt        string `json:"decided_at,omitempty"`
	HoldUntil        string `json:"hold_until,omitempty"` // RFC3339 when decision=hold
	FailingStandards string `json:"failing_standards,omitempty"`
}

type BriefsFileRecord struct {
	Version string          `json:"version"`
	Briefs  []DecisionBrief `json:"briefs"`
}

type DecideRequest struct {
	Decision string `json:"decision"` // approved_run | dismissed | hold
}

type DrainStatus struct {
	Active           bool     `json:"active"`
	CurrentItemID    string   `json:"current_item_id,omitempty"`
	CurrentJobID     string   `json:"current_job_id,omitempty"`
	CurrentTitle     string   `json:"current_title,omitempty"`
	QueuedCount      int      `json:"queued_count"`
	QueuedItemIDs    []string `json:"queued_item_ids,omitempty"`
	Paused           bool     `json:"paused,omitempty"`
	PauseReason      string   `json:"pause_reason,omitempty"`
	LastError        string   `json:"last_error,omitempty"`
	LastCompletedAt  string   `json:"last_completed_at,omitempty"`
	LastSweepSummary string   `json:"last_sweep_summary,omitempty"`
}
