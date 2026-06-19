package vision

import (
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

type GateCheck struct {
	ID           string               `json:"id"`
	Label        string               `json:"label"`
	Required     bool                 `json:"required"`
	Reachability probe.Reachability   `json:"reachability"`
	Detail       string               `json:"detail,omitempty"`
}

type V1GateResponse struct {
	Milestone    string               `json:"milestone"`
	Result       string               `json:"result"`
	Ready        bool                 `json:"ready"`
	Blockers     []string             `json:"blockers,omitempty"`
	Checks       []GateCheck          `json:"checks"`
	At           *time.Time           `json:"at,omitempty"`
	SignedAt     *time.Time           `json:"signed_at,omitempty"`
	SignedBy     string               `json:"signed_by,omitempty"`
	Reachability probe.Reachability   `json:"reachability"`
	Detail       string               `json:"detail,omitempty"`
	GeneratedAt  time.Time            `json:"generated_at"`
}

type V1SignoffRecord struct {
	At       time.Time `json:"at"`
	SignedBy string    `json:"signed_by"`
	Notes    string    `json:"notes,omitempty"`
	GateAt   time.Time `json:"gate_at"`
	Result   string    `json:"result"`
}

type RunGateResponse struct {
	OK          bool           `json:"ok"`
	Action      string         `json:"action"`
	Target      string         `json:"target"`
	Changed     bool           `json:"changed"`
	Message     string         `json:"message"`
	Gate        V1GateResponse `json:"gate"`
	GeneratedAt time.Time      `json:"generated_at"`
}

type SignoffResponse struct {
	OK          bool           `json:"ok"`
	Action      string         `json:"action"`
	Target      string         `json:"target"`
	Changed     bool           `json:"changed"`
	Message     string         `json:"message"`
	Gate        V1GateResponse `json:"gate"`
	GeneratedAt time.Time      `json:"generated_at"`
}
