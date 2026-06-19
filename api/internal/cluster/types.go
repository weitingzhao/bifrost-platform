package cluster

import (
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

type SummaryResponse struct {
	ClusterID        string             `json:"cluster_id"`
	Label            string             `json:"label"`
	Distribution     string             `json:"distribution"`
	APIServer        string             `json:"api_server"`
	KubeconfigPath    string             `json:"kubeconfig_path"`
	APIReachability   probe.Reachability `json:"api_reachability"`
	Reachability      probe.Reachability `json:"reachability"`
	Detail           string             `json:"detail"`
	ServerVersion    string             `json:"server_version,omitempty"`
	NodesReady        int `json:"nodes_ready"`
	NodesTotal        int `json:"nodes_total"`
	ElasticStandby    int `json:"elastic_standby,omitempty"`
	ElasticDegraded   int `json:"elastic_degraded,omitempty"`
	NodesRegistered      int `json:"nodes_registered,omitempty"`
	NodesRegisteredReady int `json:"nodes_registered_ready,omitempty"`
	FailingPods       int                `json:"failing_pods"`
	FailingPodDetails []FailingPodView  `json:"failing_pod_details,omitempty"`
	RunningPods       int               `json:"running_pods"`
	PendingPods       int               `json:"pending_pods"`
	CPUAllocatable   string             `json:"cpu_allocatable,omitempty"`
	MemoryAllocatable string            `json:"memory_allocatable,omitempty"`
	GeneratedAt      time.Time          `json:"generated_at"`
}

type NodeView struct {
	Name               string             `json:"name"`
	Status             string             `json:"status"`
	Roles              string             `json:"roles"`
	Architecture       string             `json:"architecture,omitempty"`
	OSImage            string             `json:"os_image,omitempty"`
	WorkloadLabel      string             `json:"workload_label,omitempty"`
	Version            string             `json:"version"`
	InternalIP         string             `json:"internal_ip"`
	Reachability       probe.Reachability `json:"reachability"`
	CPUAllocatable     string             `json:"cpu_allocatable,omitempty"`
	MemoryAllocatable  string             `json:"memory_allocatable,omitempty"`
	StorageAllocatable string             `json:"storage_allocatable,omitempty"`
	ComputeManaged     bool               `json:"compute_managed,omitempty"`
	ElasticMode        string             `json:"elastic_mode,omitempty"`
	Unschedulable      bool               `json:"unschedulable,omitempty"`
	CPUUsagePercent    *float64           `json:"cpu_usage_percent,omitempty"`
	MemoryUsagePercent *float64           `json:"memory_usage_percent,omitempty"`
	CPUReachability    probe.Reachability `json:"cpu_reachability,omitempty"`
	MemoryReachability probe.Reachability `json:"memory_reachability,omitempty"`
}

type NodesResponse struct {
	ClusterID    string             `json:"cluster_id"`
	Reachability probe.Reachability `json:"reachability"`
	Detail       string             `json:"detail"`
	Nodes        []NodeView         `json:"nodes"`
	GeneratedAt  time.Time          `json:"generated_at"`
}

type NamespaceView struct {
	Name         string `json:"name"`
	Status       string `json:"status"`
	PodCount     int    `json:"pod_count"`
	RunningPods  int    `json:"running_pods"`
	FailingPods  int    `json:"failing_pods"`
}

type NamespacesResponse struct {
	ClusterID    string             `json:"cluster_id"`
	Reachability probe.Reachability `json:"reachability"`
	Detail       string             `json:"detail"`
	Filter       string             `json:"filter"`
	Namespaces   []NamespaceView    `json:"namespaces"`
	GeneratedAt  time.Time          `json:"generated_at"`
}

type WorkloadView struct {
	Namespace    string             `json:"namespace"`
	Kind         string             `json:"kind"`
	Name         string             `json:"name"`
	Ready        string             `json:"ready"`
	Status       string             `json:"status"`
	Restarts     int32              `json:"restarts"`
	Age          string             `json:"age"`
	Reachability probe.Reachability `json:"reachability"`
}

type WorkloadsResponse struct {
	ClusterID    string             `json:"cluster_id"`
	Namespace    string             `json:"namespace"`
	Reachability probe.Reachability `json:"reachability"`
	Detail       string             `json:"detail"`
	Workloads    []WorkloadView     `json:"workloads"`
	GeneratedAt  time.Time          `json:"generated_at"`
}

type EventView struct {
	Namespace string    `json:"namespace"`
	Type      string    `json:"type"`
	Reason    string    `json:"reason"`
	Object    string    `json:"object"`
	Message   string    `json:"message"`
	Count     int32     `json:"count"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

type EventsResponse struct {
	ClusterID    string             `json:"cluster_id"`
	Namespace    string             `json:"namespace"`
	Reachability probe.Reachability `json:"reachability"`
	Detail       string             `json:"detail"`
	Events       []EventView        `json:"events"`
	GeneratedAt  time.Time          `json:"generated_at"`
}

type SyncResponse struct {
	OK      bool   `json:"ok"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

type PodMetricView struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	CPU       string `json:"cpu"`
	Memory    string `json:"memory"`
}

type MetricsResponse struct {
	ClusterID              string             `json:"cluster_id"`
	Reachability           probe.Reachability `json:"reachability"`
	Detail                 string             `json:"detail"`
	MetricsServerAvailable bool               `json:"metrics_server_available"`
	MetricsServerDetail    string             `json:"metrics_server_detail,omitempty"`
	CPUUsagePercent        *float64           `json:"cpu_usage_percent,omitempty"`
	MemoryUsagePercent     *float64           `json:"memory_usage_percent,omitempty"`
	CPUReachability        probe.Reachability `json:"cpu_reachability,omitempty"`
	MemoryReachability     probe.Reachability `json:"memory_reachability,omitempty"`
	TopPods                []PodMetricView    `json:"top_pods"`
	GeneratedAt            time.Time          `json:"generated_at"`
}

type ObservabilityComponentView struct {
	ID           string             `json:"id"`
	Label        string             `json:"label"`
	Kind         string             `json:"kind"`
	Name         string             `json:"name"`
	Ready        string             `json:"ready"`
	Status       string             `json:"status"`
	Reachability probe.Reachability `json:"reachability"`
	Detail       string             `json:"detail"`
}

type ObservabilityResponse struct {
	ClusterID     string                       `json:"cluster_id"`
	Namespace     string                       `json:"namespace"`
	LayerBStatus  string                       `json:"layer_b_status"`
	Reachability  probe.Reachability           `json:"reachability"`
	Detail        string                       `json:"detail"`
	Components    []ObservabilityComponentView `json:"components"`
	GrafanaURL    string                       `json:"grafana_url,omitempty"`
	PrometheusURL string                       `json:"prometheus_url,omitempty"`
	DocsURL       string                       `json:"docs_url,omitempty"`
	GeneratedAt   time.Time                    `json:"generated_at"`
}

type FailingPodView struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Phase     string `json:"phase"`
	Reason    string `json:"reason"`
	Node      string `json:"node,omitempty"`
	Age       string `json:"age,omitempty"`
}

type ClientError struct {
	Reachability probe.Reachability
	Detail       string
}

func (e *ClientError) Error() string {
	return e.Detail
}
