package delivery

import (
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

type PipelinesResponse struct {
	ClusterID     string          `json:"cluster_id"`
	Namespace     string          `json:"namespace"`
	Reachability  probe.Reachability `json:"reachability"`
	Detail        string          `json:"detail"`
	Pipelines     []PipelineView  `json:"pipelines"`
	GeneratedAt   time.Time       `json:"generated_at"`
}

type PipelineView struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Detail     string `json:"detail,omitempty"`
	BuildReady *bool  `json:"build_ready,omitempty"`
	BlockReason string `json:"block_reason,omitempty"`
}

type PipelinePreflightResponse struct {
	ClusterID    string             `json:"cluster_id"`
	Pipeline     string             `json:"pipeline"`
	BuildReady   bool               `json:"build_ready"`
	Reason       string             `json:"reason,omitempty"`
	Reachability probe.Reachability `json:"reachability"`
	GeneratedAt  time.Time          `json:"generated_at"`
}

type PipelineRunsResponse struct {
	ClusterID    string           `json:"cluster_id"`
	Namespace    string           `json:"namespace"`
	Pipeline     string           `json:"pipeline"`
	Reachability probe.Reachability  `json:"reachability"`
	Detail       string           `json:"detail"`
	Runs         []PipelineRunView `json:"runs"`
	GeneratedAt  time.Time        `json:"generated_at"`
}

type PipelineRunView struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Pipeline     string `json:"pipeline"`
	Status       string `json:"status"`
	Reason       string `json:"reason,omitempty"`
	StartTime    string `json:"start_time,omitempty"`
	CompletionTime string `json:"completion_time,omitempty"`
}

type RunLogsResponse struct {
	ClusterID   string    `json:"cluster_id"`
	Namespace   string    `json:"namespace"`
	RunName     string    `json:"run_name"`
	Logs        string    `json:"logs"`
	GeneratedAt time.Time `json:"generated_at"`
}

type StgSmokeTargetView struct {
	ID           string             `json:"id"`
	URL          string             `json:"url"`
	Reachability probe.Reachability `json:"reachability"`
	Detail       string             `json:"detail"`
}

type StgSmokeResponse struct {
	ClusterID    string               `json:"cluster_id"`
	Reachability probe.Reachability   `json:"reachability"`
	Detail       string               `json:"detail"`
	Targets      []StgSmokeTargetView `json:"targets"`
	GeneratedAt  time.Time            `json:"generated_at"`
}

type DockerfileConfigMapView struct {
	Name            string   `json:"name"`
	Namespace       string   `json:"namespace"`
	Present         bool     `json:"present"`
	ResourceVersion string   `json:"resource_version,omitempty"`
	UpdatedAt       string   `json:"updated_at,omitempty"`
	FileKeys        []string `json:"file_keys,omitempty"`
	ApproxBytes     int      `json:"approx_bytes,omitempty"`
	Detail          string   `json:"detail,omitempty"`
}

type StgWorkloadImageView struct {
	Deployment string `json:"deployment"`
	Namespace  string `json:"namespace"`
	Image      string `json:"image"`
}

type SupplyChainTaskRunView struct {
	Name           string `json:"name"`
	Namespace      string `json:"namespace"`
	Task           string `json:"task"`
	Actuation      string `json:"actuation,omitempty"`
	Status         string `json:"status"`
	Reason         string `json:"reason,omitempty"`
	StartTime      string `json:"start_time,omitempty"`
	CompletionTime string `json:"completion_time,omitempty"`
}

type SupplyChainResponse struct {
	ClusterID                   string                    `json:"cluster_id"`
	CicdNamespace               string                    `json:"cicd_namespace"`
	StgNamespace                string                    `json:"stg_namespace"`
	Reachability                probe.Reachability        `json:"reachability"`
	Detail                      string                    `json:"detail"`
	MirrorCredentialsConfigured bool                      `json:"mirror_credentials_configured"`
	DefaultRevision             string                    `json:"default_revision"`
	TrackedRepos                []string                  `json:"tracked_repos"`
	DockerfileCMs               []DockerfileConfigMapView `json:"dockerfile_configmaps"`
	StgWorkloads                []StgWorkloadImageView    `json:"stg_workloads"`
	LastDeliverRun              *PipelineRunView          `json:"last_deliver_run,omitempty"`
	LastDeliverSuccess          *PipelineRunView          `json:"last_deliver_success,omitempty"`
	LastSupplyChainTask         *SupplyChainTaskRunView    `json:"last_supply_chain_task,omitempty"`
	GeneratedAt                 time.Time                 `json:"generated_at"`
}

type StartPipelineRunRequest struct {
	Revision string `json:"revision"`
}

type RefreshDockerfileRequest struct {
	Revision string `json:"revision"`
}

type PipelinePhaseView struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type PipelineTaskRunView struct {
	PipelineTask string `json:"pipeline_task"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	Reason       string `json:"reason,omitempty"`
}

type PipelineRunStepsResponse struct {
	ClusterID    string                `json:"cluster_id"`
	Namespace    string                `json:"namespace"`
	RunName      string                `json:"run_name"`
	Pipeline     string                `json:"pipeline"`
	Reachability probe.Reachability    `json:"reachability"`
	Detail       string                `json:"detail"`
	Phases       []PipelinePhaseView   `json:"phases"`
	Tasks        []PipelineTaskRunView `json:"tasks,omitempty"`
	GeneratedAt  time.Time             `json:"generated_at"`
}
