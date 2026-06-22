package driftproposal

import "time"

type Status string

const (
	StatusPendingApproval Status = "pending_approval"
	StatusApproved        Status = "approved"
	StatusRejected        Status = "rejected"
	StatusRunning         Status = "running"
	StatusDone            Status = "done"
	StatusFailed          Status = "failed"
)

type Proposal struct {
	ID               string    `json:"id"`
	Status           Status    `json:"status"`
	Host             string    `json:"host,omitempty"`
	PlatformAPI      string    `json:"platform_api,omitempty"`
	ReportSource     string    `json:"report_source,omitempty"`
	LayersFailed     []string  `json:"layers_failed"`
	FindingsCount    int       `json:"findings_count"`
	Summary          string    `json:"summary"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	RemediationJobID string    `json:"remediation_job_id,omitempty"`
	ApprovedBy       string    `json:"approved_by,omitempty"`
	ApprovedAt       *time.Time `json:"approved_at,omitempty"`
	RejectedBy       string    `json:"rejected_by,omitempty"`
	RejectedAt       *time.Time `json:"rejected_at,omitempty"`
	RejectNote       string    `json:"reject_note,omitempty"`
	Error            string    `json:"error,omitempty"`
}

type CreateRequest struct {
	Host          string   `json:"host,omitempty"`
	PlatformAPI   string   `json:"platform_api,omitempty"`
	ReportSource  string   `json:"report_source,omitempty"`
	LayersFailed  []string `json:"layers_failed"`
	FindingsCount int      `json:"findings_count"`
	Summary       string   `json:"summary"`
}

type RejectRequest struct {
	Note string `json:"note,omitempty"`
}
