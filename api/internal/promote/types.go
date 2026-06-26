package promote

import (
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

type GateCheck struct {
	ID           string             `json:"id"`
	Label        string             `json:"label"`
	Required     bool               `json:"required"`
	Reachability probe.Reachability `json:"reachability"`
	Detail       string             `json:"detail"`
}

type ReleaseGateRecord struct {
	Tier        GateTier    `json:"tier,omitempty"`
	At          time.Time   `json:"at"`
	Result      string      `json:"result"`
	Revision    string      `json:"revision,omitempty"`
	LogPath     string      `json:"log_path"`
	Checks      []GateCheck `json:"checks"`
	TriggeredBy string      `json:"triggered_by,omitempty"`
	Summary     string      `json:"summary,omitempty"`
}

type TierBSignoffRecord struct {
	At       time.Time `json:"at"`
	SignedBy string    `json:"signed_by"`
	Notes    string    `json:"notes,omitempty"`
}

type TierBItemView struct {
	ID           string             `json:"id"`
	Label        string             `json:"label"`
	Kind         string             `json:"kind"` // auto | manual
	Required     bool               `json:"required"`
	Reachability probe.Reachability `json:"reachability"`
	Detail       string             `json:"detail"`
}

type TierBStatusResponse struct {
	ClusterID    string             `json:"cluster_id,omitempty"`
	Items        []TierBItemView    `json:"items"`
	SignedOff    bool               `json:"signed_off"`
	SignoffAt    *time.Time         `json:"signoff_at,omitempty"`
	SignedBy     string             `json:"signed_by,omitempty"`
	Notes        string             `json:"notes,omitempty"`
	Ready        bool               `json:"ready"`
	Reachability probe.Reachability `json:"reachability"`
	Detail       string             `json:"detail"`
	GeneratedAt  time.Time          `json:"generated_at"`
}

type TierBSignoffResponse struct {
	OK          bool                `json:"ok"`
	Action      string              `json:"action"`
	Target      string              `json:"target"`
	Changed     bool                `json:"changed"`
	Message     string              `json:"message"`
	Status      TierBStatusResponse `json:"status"`
	GeneratedAt time.Time           `json:"generated_at"`
}

type ReleaseGateResponse struct {
	Tier         GateTier           `json:"tier"`
	ClusterID   string             `json:"cluster_id,omitempty"`
	Result      string             `json:"result"`
	Revision    string             `json:"revision,omitempty"`
	At          time.Time          `json:"at"`
	LogPath     string             `json:"log_path"`
	Checks      []GateCheck        `json:"checks"`
	Ready       bool               `json:"ready"`
	Blockers    []string           `json:"blockers,omitempty"`
	GeneratedAt time.Time          `json:"generated_at"`
	Reachability probe.Reachability `json:"reachability"`
	Detail      string             `json:"detail"`
}

type RunGateResponse struct {
	OK          bool      `json:"ok"`
	Action      string    `json:"action"`
	Target      string    `json:"target"`
	Changed     bool      `json:"changed"`
	Message     string    `json:"message"`
	Gate        ReleaseGateResponse `json:"gate"`
	GeneratedAt time.Time `json:"generated_at"`
}

type ReleaseStageState struct {
	Revision string     `json:"revision,omitempty"`
	Status   string     `json:"status"`
	At       *time.Time `json:"at,omitempty"`
	Detail   string     `json:"detail,omitempty"`
}

type ReleaseAction struct {
	Action      string `json:"action"`
	Label       string `json:"label"`
	Description string `json:"description"`
	MCPTool     string `json:"mcp_tool,omitempty"`
	Params      map[string]string `json:"params,omitempty"`
}

type ReleaseStateResponse struct {
	StgDeploy        ReleaseStageState `json:"stg_deploy"`
	StgGate          ReleaseStageState `json:"stg_gate"`
	ProdDeploy       ReleaseStageState `json:"prod_deploy"`
	ProdGate         ReleaseStageState `json:"prod_gate"`
	Consistent       bool              `json:"consistent"`
	Warnings         []string          `json:"warnings,omitempty"`
	NextAction       *ReleaseAction    `json:"next_action,omitempty"`
	AvailableActions []ReleaseAction   `json:"available_actions"`
	GeneratedAt      time.Time         `json:"generated_at"`
}
