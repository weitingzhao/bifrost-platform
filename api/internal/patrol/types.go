package patrol

import "time"

const (
	TrustL0 TrustLevel = "L0"
	TrustL1 TrustLevel = "L1"
	TrustL2 TrustLevel = "L2"

	TriggerCron    Trigger = "cron"
	TriggerManual  Trigger = "manual"
	TriggerWebhook Trigger = "webhook"

	ResultSuccess   RunResult = "success"
	ResultFailure   RunResult = "failure"
	ResultSkipped   RunResult = "skipped"
	ResultEscalated RunResult = "escalated"
	ResultRunning   RunResult = "running"

	StatusStarted   TriggerStatus = "started"
	StatusCompleted TriggerStatus = "completed"
	StatusBlocked   TriggerStatus = "blocked"

	CronActuationEscalate = "escalate"
	CronActuationConfirm  = "confirm"

	MaxRuns = 200
)

type TrustLevel string
type Trigger string
type RunResult string
type TriggerStatus string

// SkillYAML is the on-disk patrol skill document.
type SkillYAML struct {
	ID             string   `yaml:"id"`
	Name           string   `yaml:"name"`
	Description    string   `yaml:"description"`
	Schedule       string   `yaml:"schedule"`
	PromptTemplate string   `yaml:"prompt_template"`
	MCPTools       []string `yaml:"mcp_tools"`
	TrustLevel     string   `yaml:"trust_level"`
	Scope          string   `yaml:"scope"`
	Timeout        int      `yaml:"timeout"`
	TimeoutSeconds int      `yaml:"timeout_seconds"`
	Enabled        *bool    `yaml:"enabled"`
	CronActuation  string   `yaml:"cron_actuation"`
}

// PatrolSkill is the public JSON shape for GET /patrol/skills.
type PatrolSkill struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	Schedule       string     `json:"schedule"`
	PromptTemplate string     `json:"prompt_template"`
	MCPTools       []string   `json:"mcp_tools"`
	TrustLevel     TrustLevel `json:"trust_level"`
	Scope          string     `json:"scope"`
	TimeoutSeconds int        `json:"timeout_seconds"`
	Enabled        bool       `json:"enabled"`
	LastRunAt      string     `json:"last_run_at,omitempty"`
	LastResult     RunResult  `json:"last_result,omitempty"`
	NextRunAt      string     `json:"next_run_at,omitempty"`
	CronActuation  string     `json:"cron_actuation,omitempty"`
}

// PatrolRun is one recorded execution.
type PatrolRun struct {
	ID         string    `json:"id"`
	SkillID    string    `json:"skill_id"`
	SkillName  string    `json:"skill_name"`
	Trigger    Trigger   `json:"trigger"`
	StartedAt  string    `json:"started_at"`
	FinishedAt string    `json:"finished_at,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	Result     RunResult `json:"result"`
	Evidence   string    `json:"evidence,omitempty"`
	Error      string    `json:"error,omitempty"`
}

type SkillsResponse struct {
	Skills []PatrolSkill `json:"skills"`
}

type RunsResponse struct {
	Runs  []PatrolRun `json:"runs"`
	Total int         `json:"total"`
}

type TriggerResponse struct {
	RunID  string        `json:"run_id"`
	Status TriggerStatus `json:"status"`
	Result RunResult     `json:"result,omitempty"`
	Error  string        `json:"error,omitempty"`
}

type EnableRequest struct {
	Enabled bool `json:"enabled"`
}

// WebhookTriggerRequest is the body for POST /patrol/webhook/{event}.
type WebhookTriggerRequest struct {
	Reason  string `json:"reason,omitempty"`
	SkillID string `json:"skill_id,omitempty"`
}

type persistState struct {
	Enabled   map[string]bool `json:"enabled"`
	Runs      []PatrolRun     `json:"runs"`
	UpdatedAt string          `json:"updated_at"`
}

type dispatchOutcome struct {
	Result   RunResult
	Evidence string
	Error    string
	Status   TriggerStatus
}

type gateDecision struct {
	Allow  bool
	Result RunResult
	Reason string
}

func (s PatrolSkill) timeout() time.Duration {
	sec := s.TimeoutSeconds
	if sec <= 0 {
		sec = 120
	}
	return time.Duration(sec) * time.Second
}
