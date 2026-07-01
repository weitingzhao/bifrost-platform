package retrospective

import "time"

// RootCause classifies why a remediation was needed.
type RootCause string

const (
	RootCauseTransient     RootCause = "transient"      // one-off restart fixed it
	RootCauseProbeDrift    RootCause = "probe_drift"     // matrix vs cluster contradiction (sensor false negative)
	RootCausePlatformDefect RootCause = "platform_defect" // code/config bug in platform
	RootCauseConfigDrift   RootCause = "config_drift"    // environment drifted from desired
	RootCauseResourceLimit RootCause = "resource_limit"  // OOM, disk, quota
	RootCauseExternal      RootCause = "external"        // upstream dependency, network
	RootCauseUnknown       RootCause = "unknown"
)

// Severity ranks how impactful a pattern is.
type Severity string

const (
	SeverityCritical Severity = "critical" // production-affecting, recurring
	SeverityHigh     Severity = "high"     // recurring, non-trivial remediation
	SeverityMedium   Severity = "medium"   // occasional, auto-resolved
	SeverityLow      Severity = "low"      // one-off or minor
)

// ComponentRef identifies what was affected.
type ComponentRef struct {
	Namespace  string `json:"namespace,omitempty"`
	Deployment string `json:"deployment,omitempty"`
	Pod        string `json:"pod,omitempty"`
	Pipeline   string `json:"pipeline,omitempty"`
	Service    string `json:"service,omitempty"`
}

// ActionTaken records what the agent did to remediate.
type ActionTaken struct {
	Tool  string `json:"tool"`
	Count int    `json:"count"`
}

// JobRef is a lightweight reference to a remediation job.
type JobRef struct {
	ID        string `json:"id"`
	Scope     string `json:"scope"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// Finding represents one observed issue from a single job.
type Finding struct {
	JobID      string        `json:"job_id"`
	Scope      string        `json:"scope"`
	Component  ComponentRef  `json:"component"`
	ErrorText  string        `json:"error_text,omitempty"`
	Actions    []ActionTaken `json:"actions"`
	Resolved   bool          `json:"resolved"`
	OccurredAt string        `json:"occurred_at"`
}

// ClassificationSignal records one piece of evidence used by the root-cause classifier.
type ClassificationSignal struct {
	Name   string    `json:"name"`
	Weight float64   `json:"weight"`
	Cause  RootCause `json:"cause"`
	Detail string    `json:"detail,omitempty"`
}

// PatternCluster groups similar findings across jobs.
type PatternCluster struct {
	ID          string        `json:"id"`
	Label       string        `json:"label"`
	Description string        `json:"description"`
	RootCause   RootCause     `json:"root_cause"`
	Confidence  float64       `json:"confidence"`  // 0.0–1.0 classification confidence
	Signals     []ClassificationSignal `json:"signals,omitempty"`
	Severity    Severity      `json:"severity"`
	Component   ComponentRef  `json:"component"`
	Occurrences int           `json:"occurrences"`
	FirstSeen   string        `json:"first_seen"`
	LastSeen    string        `json:"last_seen"`
	Jobs        []JobRef      `json:"jobs"`
	TopActions  []ActionTaken `json:"top_actions"`
	SuccessRate float64       `json:"success_rate"`
	AvgDuration float64       `json:"avg_duration_seconds"`
	Trending    string        `json:"trending"` // up | stable | down
}

// ScopeStats summarizes one scope's job outcomes.
type ScopeStats struct {
	Scope       string  `json:"scope"`
	Total       int     `json:"total"`
	Done        int     `json:"done"`
	Failed      int     `json:"failed"`
	Cancelled   int     `json:"cancelled"`
	Running     int     `json:"running"`
	SuccessRate float64 `json:"success_rate"`
	AvgDuration float64 `json:"avg_duration_seconds"`
}

// ToolUsage ranks MCP tools by invocation count.
type ToolUsage struct {
	Tool  string `json:"tool"`
	Count int    `json:"count"`
	Jobs  int    `json:"jobs"` // how many distinct jobs used this tool
}

// NamespaceActivity records how often each namespace was touched.
type NamespaceActivity struct {
	Namespace  string `json:"namespace"`
	ToolCalls  int    `json:"tool_calls"`
	Jobs       int    `json:"jobs"`
	TopActions []ActionTaken `json:"top_actions"`
}

// RootCauseDistribution counts patterns by root cause.
type RootCauseDistribution struct {
	Cause    RootCause `json:"cause"`
	Count    int       `json:"count"`
	Fraction float64  `json:"fraction"` // 0.0–1.0
}

// AnalysisReport is the top-level result of retrospective analysis.
type AnalysisReport struct {
	GeneratedAt        time.Time              `json:"generated_at"`
	TotalJobs          int                    `json:"total_jobs"`
	AnalysisWindow     string                 `json:"analysis_window"`
	Patterns           []PatternCluster       `json:"patterns"`
	RootCauseDist      []RootCauseDistribution `json:"root_cause_distribution"`
	ScopeStats         []ScopeStats           `json:"scope_stats"`
	ToolUsage          []ToolUsage            `json:"tool_usage"`
	Namespaces         []NamespaceActivity    `json:"namespaces"`
	HealthScore        float64                `json:"health_score"` // 0–100, higher is healthier
	Insights           []string               `json:"insights"`
}
