package gitops

import (
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// AppsResponse is the L0 read model for GET /api/v1/gitops/apps (P3).
type AppsResponse struct {
	ClusterID             string             `json:"cluster_id"`
	ArgoCDNamespace       string             `json:"argocd_namespace"`
	ApplicationsNamespace string             `json:"applications_namespace"`
	ArgoCDStatus          string             `json:"argocd_status"`
	Reachability          probe.Reachability `json:"reachability"`
	Detail                string             `json:"detail"`
	Server                *ArgoCDServerView  `json:"server,omitempty"`
	Apps                  []ApplicationView  `json:"apps"`
	GeneratedAt           time.Time          `json:"generated_at"`
}

type ArgoCDServerView struct {
	Kind         string             `json:"kind"`
	Name         string             `json:"name"`
	Ready        string             `json:"ready"`
	Status       string             `json:"status"`
	Reachability probe.Reachability `json:"reachability"`
	Detail       string             `json:"detail,omitempty"`
}

type ApplicationView struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Project     string `json:"project,omitempty"`
	SyncStatus  string `json:"sync_status"`
	HealthStatus string `json:"health_status"`
	Destination string `json:"destination,omitempty"`
	Revision    string `json:"revision,omitempty"`
}
