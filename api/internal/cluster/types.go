package cluster

import (
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

type SummaryResponse struct {
	ClusterID       string             `json:"cluster_id"`
	Label           string             `json:"label"`
	Distribution    string             `json:"distribution"`
	APIServer       string             `json:"api_server"`
	KubeconfigPath  string             `json:"kubeconfig_path"`
	Reachability    probe.Reachability `json:"reachability"`
	Detail          string             `json:"detail"`
	ServerVersion   string             `json:"server_version,omitempty"`
	NodesReady      int                `json:"nodes_ready"`
	NodesTotal      int                `json:"nodes_total"`
	FailingPods     int                `json:"failing_pods"`
	GeneratedAt     time.Time          `json:"generated_at"`
}

type NodeView struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Roles       string `json:"roles"`
	Version     string `json:"version"`
	InternalIP  string `json:"internal_ip"`
	Reachability probe.Reachability `json:"reachability"`
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

type ClientError struct {
	Reachability probe.Reachability
	Detail       string
}

func (e *ClientError) Error() string {
	return e.Detail
}
