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
