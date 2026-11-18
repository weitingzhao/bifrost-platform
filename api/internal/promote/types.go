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
