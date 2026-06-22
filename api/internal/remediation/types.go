package remediation

import "time"

type Phase string

const (
	PhaseStarting          Phase = "starting"
	PhaseDiagnosing        Phase = "diagnosing"
	PhaseAwaitingApproval  Phase = "awaiting_approval"
	PhaseRemediating       Phase = "remediating"
	PhaseVerifying         Phase = "verifying"
	PhaseDone              Phase = "done"
	PhaseFailed            Phase = "failed"
	PhaseCancelled         Phase = "cancelled"
)

type EventType string

const (
	EventThinking         EventType = "thinking"
	EventToolCall         EventType = "tool_call"
	EventToolResult       EventType = "tool_result"
	EventStatus           EventType = "status"
	EventApprovalRequest  EventType = "approval_request"
	EventDone             EventType = "done"
	EventError            EventType = "error"
)

type Event struct {
	ID   string         `json:"id"`
	At   time.Time      `json:"at"`
	Type EventType      `json:"type"`
	Text string         `json:"text"`
	Meta map[string]any `json:"meta,omitempty"`
}

type JobStatus string

const (
	JobRunning   JobStatus = "running"
	JobDone      JobStatus = "done"
	JobFailed    JobStatus = "failed"
	JobCancelled JobStatus = "cancelled"
)

type Job struct {
	ID        string    `json:"id"`
	Phase     Phase     `json:"phase"`
	Status    JobStatus `json:"status"`
	Summary   string    `json:"summary,omitempty"`
	Error     string    `json:"error,omitempty"`
	Actor     string    `json:"actor,omitempty"`
	Scope     string    `json:"scope,omitempty"`
	InitBrief string    `json:"init_brief,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Events    []Event   `json:"events,omitempty"`
}

type StartRequest struct {
	Scope             string `json:"scope,omitempty"`
	ClusterSummary    any    `json:"cluster_summary,omitempty"`
	ServiceReadiness  any    `json:"service_readiness,omitempty"`
	Governance        any    `json:"governance,omitempty"`
	Issues            any    `json:"issues,omitempty"`
	Prompt            string `json:"prompt,omitempty"`
}

type RespondRequest struct {
	OptionID string `json:"option_id"`
	Note     string `json:"note,omitempty"`
}

type StartRunnerRequest struct {
	Scope            string `json:"scope,omitempty"`
	Actor            string `json:"actor,omitempty"`
	ClusterSummary   any    `json:"cluster_summary,omitempty"`
	ServiceReadiness any    `json:"service_readiness,omitempty"`
	Governance       any    `json:"governance,omitempty"`
	Issues           any    `json:"issues,omitempty"`
	Prompt           string `json:"prompt,omitempty"`
}
