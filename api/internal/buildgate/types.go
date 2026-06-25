package buildgate

import "time"

type GateCheck struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Status   string `json:"status"`
	Required bool   `json:"required"`
	Detail   string `json:"detail,omitempty"`
}

type PhaseGateResponse struct {
	Phase         string      `json:"phase"`
	TotalTasks    int         `json:"total_tasks"`
	DoneTasks     int         `json:"done_tasks"`
	Ready         bool        `json:"ready"`
	Result        string      `json:"result"`
	Checks        []GateCheck `json:"checks"`
	Blockers      []string    `json:"blockers,omitempty"`
	SignedAt      *time.Time  `json:"signed_at,omitempty"`
	SignedBy      string      `json:"signed_by,omitempty"`
	LastRunAt     *time.Time  `json:"last_run_at,omitempty"`
	LastRunResult string      `json:"last_run_result,omitempty"`
	GeneratedAt   time.Time   `json:"generated_at"`
}

type RunGateResponse struct {
	OK          bool              `json:"ok"`
	Action      string            `json:"action"`
	Target      string            `json:"target"`
	Changed     bool              `json:"changed"`
	Message     string            `json:"message"`
	Gate        PhaseGateResponse `json:"gate"`
	GeneratedAt time.Time         `json:"generated_at"`
}

type GateRecord struct {
	At          time.Time   `json:"at"`
	Phase       string      `json:"phase"`
	Result      string      `json:"result"`
	Checks      []GateCheck `json:"checks"`
	TriggeredBy string      `json:"triggered_by,omitempty"`
}

type SignoffRecord struct {
	At       time.Time `json:"at"`
	Phase    string    `json:"phase"`
	SignedBy string    `json:"signed_by"`
	Notes    string    `json:"notes,omitempty"`
	Result   string    `json:"result"`
}
