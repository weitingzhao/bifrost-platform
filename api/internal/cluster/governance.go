package cluster

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	storagev1 "k8s.io/api/storage/v1"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

type CapabilityCoverageView struct {
	ID           string             `json:"id"`
	Label        string             `json:"label"`
	Category     string             `json:"category"`
	Scope        string             `json:"scope"`
	LabelHint    string             `json:"label_hint,omitempty"`
	RequiredFor  string             `json:"required_for,omitempty"`
	NodesReady   int                `json:"nodes_ready"`
	NodesTotal   int                `json:"nodes_total"`
	NodeNames    []string           `json:"node_names"`
	Reachability probe.Reachability `json:"reachability"`
	GapReason    string             `json:"gap_reason,omitempty"`
}

type ClusterCapabilityView struct {
	ID           string             `json:"id"`
	Label        string             `json:"label"`
	Category     string             `json:"category"`
	Status       string             `json:"status"`
	Reachability probe.Reachability `json:"reachability"`
	Detail       string             `json:"detail"`
}

type GovernanceResponse struct {
	ClusterID    string                   `json:"cluster_id"`
	Reachability probe.Reachability       `json:"reachability"`
	Detail       string                   `json:"detail"`
	Catalog      []CapabilityCatalogEntry `json:"catalog"`
	NodeCoverage []CapabilityCoverageView `json:"node_coverage"`
	ClusterCaps  []ClusterCapabilityView  `json:"cluster_capabilities"`
	GeneratedAt  time.Time                `json:"generated_at"`
}

func (s *Service) Governance(ctx context.Context) GovernanceResponse {
	now := time.Now().UTC()
	base := s.baseMeta(now)

	nodesResp := s.Nodes(ctx)
	catalog := NodeCapabilityCatalog()
	nodeCoverage := buildNodeCoverage(nodesResp.Nodes, catalog)
	clusterCaps, clusterReach, clusterDetail := s.probeClusterCapabilities(ctx)

	reach := nodesResp.Reachability
	detail := "governance snapshot ok"
	if nodesResp.Reachability != probe.ReachOK {
		detail = nodesResp.Detail
	}
	if clusterReach == probe.ReachFail && reach == probe.ReachOK {
		reach = probe.ReachDegraded
		detail = clusterDetail
	} else if clusterReach == probe.ReachFail {
		reach = probe.ReachFail
		detail = clusterDetail
	}

	return GovernanceResponse{
		ClusterID:    base.ClusterID,
		Reachability: reach,
		Detail:       detail,
		Catalog:      catalog,
		NodeCoverage: nodeCoverage,
		ClusterCaps:  clusterCaps,
		GeneratedAt:  now,
	}
}

func buildNodeCoverage(nodes []NodeView, catalog []CapabilityCatalogEntry) []CapabilityCoverageView {
	nodeCapsByID := make(map[string][]NodeView)
	for _, n := range nodes {
		for _, c := range n.Capabilities {
			nodeCapsByID[c.ID] = append(nodeCapsByID[c.ID], n)
		}
	}

	out := make([]CapabilityCoverageView, 0)
	for _, entry := range catalog {
		if entry.Scope != "node" {
			continue
		}
		matched := nodeCapsByID[entry.ID]
		cv := CapabilityCoverageView{
			ID:          entry.ID,
			Label:       entry.Label,
			Category:    entry.Category,
			Scope:       entry.Scope,
			LabelHint:   entry.LabelHint,
			RequiredFor: entry.RequiredFor,
			NodeNames:   []string{},
		}
		for _, n := range matched {
			cv.NodesTotal++
			cv.NodeNames = append(cv.NodeNames, n.Name)
			if n.Reachability == probe.ReachOK && n.Status == "Ready" {
				cv.NodesReady++
			}
		}
		sort.Strings(cv.NodeNames)
		cv.Reachability, cv.GapReason = coverageReach(entry.ID, cv.NodesReady, cv.NodesTotal)
		out = append(out, cv)
	}
	return out
}

func coverageReach(id string, ready, total int) (probe.Reachability, string) {
	switch {
	case ready > 0:
		return probe.ReachOK, ""
	case total > 0:
		return probe.ReachDegraded, fmt.Sprintf("%s: %d node(s) labeled but none Ready", id, total)
	default:
		if id == "nfs-client" {
			return probe.ReachFail, "No node labeled storage.nfs/client=true — NFS PVCs will fail to mount"
		}
		return probe.ReachDegraded, "No nodes declare this capability yet"
	}
}

func (s *Service) probeClusterCapabilities(ctx context.Context) ([]ClusterCapabilityView, probe.Reachability, string) {
	clientset, _, err := s.buildClient()
	if err != nil {
		return missingClusterCapabilities(err.Error()), probe.ReachFail, err.Error()
	}

	scList, scErr := clientset.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	scByName := map[string]storagev1.StorageClass{}
	if scErr == nil {
		for _, sc := range scList.Items {
			scByName[sc.Name] = sc
		}
	}

	deployments, depErr := clientset.AppsV1().Deployments("kube-system").List(ctx, metav1.ListOptions{})
	var deployItems []appsv1.Deployment
	if depErr == nil {
		deployItems = deployments.Items
	}

	metricsOK := s.metricsServerReady(ctx)

	views := make([]ClusterCapabilityView, 0, len(clusterCapabilityCatalog))
	failCount := 0
	for _, spec := range clusterCapabilityCatalog {
		view := ClusterCapabilityView{
			ID:       spec.ID,
			Label:    spec.Label,
			Category: spec.Category,
		}
		switch spec.ID {
		case "storage-class-nfs-hot":
			view = storageClassView(view, scByName, "nfs-hot", scErr)
		case "storage-class-nfs-cold":
			view = storageClassView(view, scByName, "nfs-cold", scErr)
		case "storage-class-local-path":
			view = storageClassView(view, scByName, "local-path", scErr)
		case "nfs-provisioner-hot":
			view = nfsProvisionerView(view, deployItems, "nfs-provisioner-hot", depErr)
		case "nfs-provisioner-cold":
			view = nfsProvisionerView(view, deployItems, "nfs-provisioner-cold", depErr)
		case "metrics-server":
			view = metricsServerCapabilityView(view, metricsOK)
		default:
			view.Status = "unknown"
			view.Reachability = probe.ReachDegraded
			view.Detail = "probe not implemented"
		}
		if view.Reachability == probe.ReachFail {
			failCount++
		}
		views = append(views, view)
	}

	reach := probe.ReachOK
	detail := "cluster capabilities probed"
	if scErr != nil || depErr != nil {
		reach = probe.ReachDegraded
		detail = "partial cluster probe"
	}
	if failCount > 0 {
		reach = probe.ReachDegraded
		if failCount >= 3 {
			reach = probe.ReachFail
		}
		detail = fmt.Sprintf("%d cluster capability gap(s)", failCount)
	}
	return views, reach, detail
}

func storageClassView(view ClusterCapabilityView, scByName map[string]storagev1.StorageClass, name string, listErr error) ClusterCapabilityView {
	if listErr != nil {
		view.Status = "unknown"
		view.Reachability = probe.ReachDegraded
		view.Detail = fmt.Sprintf("list StorageClasses: %v", listErr)
		return view
	}
	sc, ok := scByName[name]
	if !ok {
		view.Status = "missing"
		view.Reachability = probe.ReachFail
		view.Detail = fmt.Sprintf("StorageClass %q not found", name)
		return view
	}
	prov := sc.Provisioner
	reclaim := "Delete"
	if sc.ReclaimPolicy != nil {
		reclaim = string(*sc.ReclaimPolicy)
	}
	view.Status = "ready"
	view.Reachability = probe.ReachOK
	view.Detail = fmt.Sprintf("provisioner=%s reclaim=%s", prov, reclaim)
	return view
}

func nfsProvisionerView(view ClusterCapabilityView, deployments []appsv1.Deployment, releasePrefix string, listErr error) ClusterCapabilityView {
	if listErr != nil {
		view.Status = "unknown"
		view.Reachability = probe.ReachDegraded
		view.Detail = fmt.Sprintf("list deployments: %v", listErr)
		return view
	}
	for _, d := range deployments {
		if !strings.HasPrefix(d.Name, releasePrefix) {
			continue
		}
		ready := d.Status.ReadyReplicas
		desired := d.Status.Replicas
		if desired == 0 {
			desired = *d.Spec.Replicas
		}
		if ready >= 1 && ready >= desired {
			view.Status = "ready"
			view.Reachability = probe.ReachOK
			view.Detail = fmt.Sprintf("deployment/%s ready=%d/%d", d.Name, ready, desired)
			return view
		}
		view.Status = "degraded"
		view.Reachability = probe.ReachDegraded
		view.Detail = fmt.Sprintf("deployment/%s ready=%d/%d", d.Name, ready, desired)
		return view
	}
	view.Status = "missing"
	view.Reachability = probe.ReachFail
	view.Detail = fmt.Sprintf("no kube-system deployment matching %q", releasePrefix)
	return view
}

func metricsServerCapabilityView(view ClusterCapabilityView, ok bool) ClusterCapabilityView {
	if ok {
		view.Status = "ready"
		view.Reachability = probe.ReachOK
		view.Detail = "metrics API responding"
		return view
	}
	view.Status = "missing"
	view.Reachability = probe.ReachDegraded
	view.Detail = "metrics API unavailable — install metrics-server addon"
	return view
}

func (s *Service) metricsServerReady(ctx context.Context) bool {
	metricsClient, err := s.buildMetricsClient()
	if err != nil {
		return false
	}
	_, err = metricsClient.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{Limit: 1})
	return err == nil
}

func missingClusterCapabilities(reason string) []ClusterCapabilityView {
	out := make([]ClusterCapabilityView, len(clusterCapabilityCatalog))
	for i, spec := range clusterCapabilityCatalog {
		out[i] = ClusterCapabilityView{
			ID:           spec.ID,
			Label:        spec.Label,
			Category:     spec.Category,
			Status:       "unknown",
			Reachability: probe.ReachFail,
			Detail:       reason,
		}
	}
	return out
}
