package promote

import "time"

// ReleaseCycleLane separates Trade (Satellite) vs Platform (Rocket) release cycles.
type ReleaseCycleLane string

const (
	ReleaseCycleLaneTrade    ReleaseCycleLane = "trade"
	ReleaseCycleLanePlatform ReleaseCycleLane = "platform"
)

// CycleStepKind is one of the four stages in a release cycle.
type CycleStepKind string

const (
	CycleStepStgDeploy  CycleStepKind = "stg_deploy"
	CycleStepStgGate    CycleStepKind = "stg_gate"
	CycleStepProdDeploy CycleStepKind = "prod_deploy"
	CycleStepProdGate   CycleStepKind = "prod_gate"
)

// Cycle outcomes.
const (
	CycleOutcomeInProgress = "in_progress"
	CycleOutcomeReleased   = "released"
	CycleOutcomeFailed     = "failed"
	CycleOutcomeSuperseded = "superseded"
)

// Cycle step results.
const (
	CycleStepResultSuccess = "success"
	CycleStepResultFailed  = "failed"
	CycleStepResultRunning = "running"
	CycleStepResultPass    = "pass"
	CycleStepResultFail    = "fail"
)

// CycleStepRecord is one stage within a ReleaseCycleRecord.
type CycleStepRecord struct {
	Kind        CycleStepKind `json:"kind"`
	StartedAt   *time.Time    `json:"started_at,omitempty"`
	CompletedAt *time.Time    `json:"completed_at,omitempty"`
	Result      string        `json:"result,omitempty"` // success | failed | running | pass | fail | ""
	RunName     string        `json:"run_name,omitempty"`
	Detail      string        `json:"detail,omitempty"`
	GateChecks  []GateCheck   `json:"gate_checks,omitempty"`
}

// ReleaseCycleRecord is one full STG→PROD release lifecycle.
type ReleaseCycleRecord struct {
	ID             string            `json:"id"`
	Lane           ReleaseCycleLane  `json:"lane"`
	Revision       string            `json:"revision"`
	Outcome        string            `json:"outcome"` // released | failed | in_progress | superseded
	StartedAt      time.Time         `json:"started_at"`
	CompletedAt    *time.Time        `json:"completed_at,omitempty"`
	Steps          []CycleStepRecord `json:"steps"`
	AgentSessionID string            `json:"agent_session_id,omitempty"`
	TriggeredBy    string            `json:"triggered_by,omitempty"`
}

// ReleaseCyclesResponse is the list API payload.
type ReleaseCyclesResponse struct {
	Lane    ReleaseCycleLane     `json:"lane"`
	Entries []ReleaseCycleRecord `json:"entries"`
}

// ParseReleaseCycleLane normalizes the lane query param.
func ParseReleaseCycleLane(raw string) ReleaseCycleLane {
	switch raw {
	case string(ReleaseCycleLanePlatform):
		return ReleaseCycleLanePlatform
	default:
		return ReleaseCycleLaneTrade
	}
}

// LaneForGateTier maps a gate tier to its release cycle lane.
func LaneForGateTier(tier GateTier) ReleaseCycleLane {
	if IsPlatformTier(tier) {
		return ReleaseCycleLanePlatform
	}
	return ReleaseCycleLaneTrade
}

// LaneForPipeline maps a deliver pipeline name to its release cycle lane and deploy step.
func LaneForPipeline(pipelineName string) (ReleaseCycleLane, CycleStepKind, bool) {
	switch pipelineName {
	case "bifrost-deliver-stg":
		return ReleaseCycleLaneTrade, CycleStepStgDeploy, true
	case "bifrost-deliver-prod":
		return ReleaseCycleLaneTrade, CycleStepProdDeploy, true
	case "bifrost-deliver-platform":
		return ReleaseCycleLanePlatform, CycleStepStgDeploy, true
	case "bifrost-deliver-platform-prod":
		return ReleaseCycleLanePlatform, CycleStepProdDeploy, true
	default:
		return "", "", false
	}
}

// GateStepForTier maps a gate tier to its cycle step kind.
func GateStepForTier(tier GateTier) (CycleStepKind, bool) {
	switch tier {
	case GateTierStg, GateTierPlatformStg:
		return CycleStepStgGate, true
	case GateTierProd, GateTierPlatformProd:
		return CycleStepProdGate, true
	default:
		return "", false
	}
}

func emptyCycleSteps() []CycleStepRecord {
	return []CycleStepRecord{
		{Kind: CycleStepStgDeploy},
		{Kind: CycleStepStgGate},
		{Kind: CycleStepProdDeploy},
		{Kind: CycleStepProdGate},
	}
}
