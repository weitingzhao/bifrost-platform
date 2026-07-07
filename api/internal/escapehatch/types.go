package escapehatch

import "time"

type RouteStatus string

const (
	RouteOK          RouteStatus = "ok"
	RouteDegraded    RouteStatus = "degraded"
	RouteFail        RouteStatus = "fail"
	RouteUnknown     RouteStatus = "unknown"
	RouteDocumented  RouteStatus = "documented"
)

type RouteProbe struct {
	ID        string      `json:"id"`
	Label     string      `json:"label"`
	URL       string      `json:"url,omitempty"`
	Status    RouteStatus `json:"status"`
	Detail    string      `json:"detail"`
	LatencyMs int64       `json:"latency_ms,omitempty"`
}

type EscapeRoute struct {
	ID          string       `json:"id"`
	Label       string       `json:"label"`
	Layer       string       `json:"layer"`
	Summary     string       `json:"summary"`
	Command     string       `json:"command,omitempty"`
	Status      RouteStatus  `json:"status"`
	Detail      string       `json:"detail"`
	Probes      []RouteProbe `json:"probes,omitempty"`
	RunbookRefs []string     `json:"runbook_refs,omitempty"`
}

type QuarterlyDrill struct {
	IntervalDays int        `json:"interval_days"`
	LastDrillAt  *time.Time `json:"last_drill_at,omitempty"`
	LastDrillBy  string     `json:"last_drill_by,omitempty"`
	Notes        string     `json:"notes,omitempty"`
	NextDueAt    *time.Time `json:"next_due_at,omitempty"`
	Overdue      bool       `json:"overdue"`
	DaysSince    *int       `json:"days_since_last_drill,omitempty"`
}

type Response struct {
	GeneratedAt    time.Time      `json:"generated_at"`
	RunbookVersion string         `json:"runbook_version"`
	Overall        RouteStatus    `json:"overall"`
	Routes         []EscapeRoute  `json:"routes"`
	Quarterly      QuarterlyDrill `json:"quarterly"`
	AgentGuidance  string         `json:"agent_guidance,omitempty"`
}

type DrillRecord struct {
	Version   string    `json:"version"`
	At        time.Time `json:"at"`
	By        string    `json:"by"`
	Notes     string    `json:"notes,omitempty"`
	RouteIDs  []string  `json:"route_ids,omitempty"`
}

type RecordDrillRequest struct {
	Notes    string   `json:"notes,omitempty"`
	RouteIDs []string `json:"route_ids,omitempty"`
	By       string   `json:"by,omitempty"`
}
