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

type ApplicationConditionView struct {
	Type               string `json:"type"`
	Message            string `json:"message"`
	LastTransitionTime string `json:"last_transition_time,omitempty"`
}

type ApplicationView struct {
	Name                 string                     `json:"name"`
	Namespace            string                     `json:"namespace"`
	Project              string                     `json:"project,omitempty"`
	SyncStatus           string                     `json:"sync_status"`
	HealthStatus         string                     `json:"health_status"`
	Destination          string                     `json:"destination,omitempty"`
	DestinationNamespace string                     `json:"destination_namespace,omitempty"`
	Revision             string                     `json:"revision,omitempty"`
	SourceRepo           string                     `json:"source_repo,omitempty"`
	SourcePath           string                     `json:"source_path,omitempty"`
	SourceTargetRevision string                     `json:"source_target_revision,omitempty"`
	AutomatedSync        bool                       `json:"automated_sync"`
	SelfHeal             bool                       `json:"self_heal"`
	Prune                bool                       `json:"prune"`
	HistoryCount         int                        `json:"history_count"`
	Conditions           []ApplicationConditionView `json:"conditions,omitempty"`
	PrimaryCondition     string                     `json:"primary_condition,omitempty"`
	OperationPhase       string                     `json:"operation_phase,omitempty"`
	OperationMessage     string                     `json:"operation_message,omitempty"`
}

// RollbackRequest is the optional body for POST /api/v1/gitops/apps/{name}/rollback (P3 admin).
type RollbackRequest struct {
	Revision string `json:"revision,omitempty"`
}
