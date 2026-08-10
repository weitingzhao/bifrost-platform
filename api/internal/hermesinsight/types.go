package hermesinsight

const (
	TypeFirstTask   = "first-task"
	SourceFirstTask = "hermes-first-task"

	VerdictOK      = "ok"
	VerdictPending = "pending"
	VerdictBlocked = "blocked"
	VerdictWarn    = "warn"

	MaxInsights      = 200
	DefaultListLimit = 50
)

// HermesInsight is one Analysis Desk V1 row (GET /api/v1/hermes/insights).
type HermesInsight struct {
	ID         string `json:"id"`
	Time       string `json:"time"`
	Symbol     string `json:"symbol"`
	Type       string `json:"type"`
	Verdict    string `json:"verdict"`
	DurationMS int64  `json:"duration_ms"`
	Source     string `json:"source"`
	Summary    string `json:"summary,omitempty"`
}

// ListResponse is GET /api/v1/hermes/insights.
type ListResponse struct {
	Items []HermesInsight `json:"items"`
	Total int             `json:"total"`
}

// RunFirstTaskResponse is POST /api/v1/hermes/run-first-task.
// Handler-ran failures stay HTTP 200 with ok=false; 5xx is unexpected only.
type RunFirstTaskResponse struct {
	OK      bool          `json:"ok"`
	Insight HermesInsight `json:"insight"`
	Error   string        `json:"error"`
}

type persistState struct {
	Insights  []HermesInsight `json:"insights"`
	UpdatedAt string          `json:"updated_at"`
}
