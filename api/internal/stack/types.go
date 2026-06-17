package stack

import (
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// AddonsResponse is the L0 read model for GET /api/v1/stack/addons (P2).
type AddonsResponse struct {
	ClusterID   string             `json:"cluster_id"`
	Namespace   string             `json:"namespace"`
	Reachability probe.Reachability `json:"reachability"`
	Detail      string             `json:"detail"`
	Addons      []AddonView        `json:"addons"`
	GeneratedAt time.Time          `json:"generated_at"`
}

type AddonView struct {
	ID           string             `json:"id"`
	Label        string             `json:"label"`
	Status       string             `json:"status"`
	Reachability probe.Reachability `json:"reachability"`
	Kind         string             `json:"kind,omitempty"`
	Name         string             `json:"name,omitempty"`
	Ready        string             `json:"ready,omitempty"`
	Detail       string             `json:"detail,omitempty"`
}
