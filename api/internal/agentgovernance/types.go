package agentgovernance

import "time"

const (
	PromotionThreshold = 3
	DemotionFailStreak = 1
)

type PerformanceWindow struct {
	Window           string  `json:"window"`
	TotalExecutions  int     `json:"total_executions"`
	SuccessCount     int     `json:"success_count"`
	FailureCount     int     `json:"failure_count"`
	EscalationCount  int     `json:"escalation_count"`
	SuccessRate      float64 `json:"success_rate"`
	MeanDurationMs   float64 `json:"mean_duration_ms"`
	InterventionRate float64 `json:"intervention_rate"`
}

type PerformanceResponse struct {
	GeneratedAt time.Time           `json:"generated_at"`
	Windows     []PerformanceWindow `json:"windows"`
	MttrSeconds *float64            `json:"mttr_seconds,omitempty"`
	DataSource  string              `json:"data_source"`
	JobCount    int                 `json:"job_count"`
}

type TrustMatrixEntry struct {
	SkillID               string `json:"skill_id"`
	SkillLabel            string `json:"skill_label"`
	CurrentLevel          string `json:"current_level"`
	ConsecutiveSuccesses  int    `json:"consecutive_successes"`
	PromotionEligible     bool   `json:"promotion_eligible"`
	DemotionTriggered     bool   `json:"demotion_triggered"`
	LastOverrideAt        string `json:"last_override_at,omitempty"`
	LastOverrideBy        string `json:"last_override_by,omitempty"`
	SuggestedLevel        string `json:"suggested_level,omitempty"`
	SuggestedLevelReason  string `json:"suggested_level_reason,omitempty"`
}

type TrustMatrixResponse struct {
	GeneratedAt time.Time          `json:"generated_at"`
	Entries       []TrustMatrixEntry `json:"entries"`
	DataSource    string             `json:"data_source"`
}

type TrustOverridesResponse struct {
	GeneratedAt time.Time                  `json:"generated_at"`
	Overrides   map[string]TrustOverride   `json:"overrides"`
}

type TrustOverrideRequest struct {
	Level     string `json:"level,omitempty"`
	Action    string `json:"action,omitempty"`
	Reason    string `json:"reason,omitempty"`
	AppliedBy string `json:"applied_by,omitempty"`
}

type CapabilityMapEntry struct {
	TaskScope        string   `json:"task_scope"`
	TaskLabel        string   `json:"task_label"`
	Autonomy         string   `json:"autonomy"`
	McpTools         []string `json:"mcp_tools"`
	MissionSignals   []string `json:"mission_signals"`
	HasGap           bool     `json:"has_gap"`
	GapDetail        string   `json:"gap_detail,omitempty"`
}

type CapabilityMapResponse struct {
	GeneratedAt time.Time            `json:"generated_at"`
	Entries       []CapabilityMapEntry `json:"entries"`
	GapCount      int                  `json:"gap_count"`
	McpToolCount  int                  `json:"mcp_tool_count"`
}

type BriefingDigest struct {
	PeriodHours      int    `json:"period_hours"`
	JobsCompleted    int    `json:"jobs_completed"`
	JobsFailed       int    `json:"jobs_failed"`
	Escalations      int    `json:"escalations"`
	PromotionPending int    `json:"promotion_pending"`
	Demotions        int    `json:"demotions"`
	Summary          string `json:"summary"`
}

type SnapshotResponse struct {
	GeneratedAt    time.Time            `json:"generated_at"`
	HermesAvailable  bool                 `json:"hermes_available"`
	DataSources      []string             `json:"data_sources"`
	Performance      PerformanceResponse  `json:"performance"`
	TrustMatrix      TrustMatrixResponse  `json:"trust_matrix"`
	CapabilityMap    CapabilityMapResponse `json:"capability_map"`
	Briefing         BriefingDigest       `json:"briefing"`
	ProgramComplete  bool                 `json:"program_complete"`
	Note             string               `json:"note,omitempty"`
}
