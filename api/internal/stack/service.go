package stack

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

type Service struct {
	entry   *config.ClusterEntry
	cluster *cluster.Service
}

func NewService(entry *config.ClusterEntry) *Service {
	return &Service{
		entry:   entry,
		cluster: cluster.NewService(entry),
	}
}

func (s *Service) Addons(ctx context.Context) AddonsResponse {
	now := time.Now().UTC()
	clusterID := "unknown"
	if s.entry != nil && s.entry.ID != "" {
		clusterID = s.entry.ID
	}
	ns := "cicd"
	if s.entry != nil {
		ns = s.entry.ResolvedStackNamespace()
	}

	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		reach := probe.ReachFail
		detail := err.Error()
		if ce, ok := err.(*cluster.ClientError); ok {
			reach = ce.Reachability
			detail = ce.Detail
		}
		return AddonsResponse{
			ClusterID:    clusterID,
			Namespace:    ns,
			Reachability: reach,
			Detail:       detail,
			Addons:       s.plannedAddons(now),
			GeneratedAt:  now,
		}
	}

	if _, nsErr := clientset.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{}); nsErr != nil {
		return AddonsResponse{
			ClusterID:    clusterID,
			Namespace:    ns,
			Reachability: probe.ReachDegraded,
			Detail:       fmt.Sprintf("namespace %s not found", ns),
			Addons:       s.probeAddons(ctx, clientset, ns),
			GeneratedAt:  now,
		}
	}

	addons := s.probeAddons(ctx, clientset, ns)
	reach, detail := aggregateAddonsReachability(addons)
	return AddonsResponse{
		ClusterID:    clusterID,
		Namespace:    ns,
		Reachability: reach,
		Detail:       detail,
		Addons:       addons,
		GeneratedAt:  now,
	}
}

func (s *Service) plannedAddons(now time.Time) []AddonView {
	_ = now
	specs := []config.StackAddonSpec{
		{ID: "gitea", Label: "Gitea", Match: "gitea"},
		{ID: "tekton", Label: "Tekton", Match: "tekton"},
		{ID: "registry", Label: "Registry", Match: "registry"},
	}
	if s.entry != nil {
		specs = s.entry.ResolvedStackAddons()
	}
	views := make([]AddonView, 0, len(specs))
	for _, spec := range specs {
		views = append(views, AddonView{
			ID:           spec.ID,
			Label:        spec.Label,
			Status:       "not_installed",
			Reachability: probe.ReachDegraded,
			Detail:       "cluster unreachable",
		})
	}
	return views
}

func (s *Service) probeAddons(ctx context.Context, clientset kubernetes.Interface, namespace string) []AddonView {
	specs := []config.StackAddonSpec{
		{ID: "gitea", Label: "Gitea", Match: "gitea"},
		{ID: "tekton", Label: "Tekton", Match: "tekton", ProbeNamespace: "tekton-pipelines"},
		{ID: "registry", Label: "Registry", Match: "registry"},
	}
	if s.entry != nil {
		specs = s.entry.ResolvedStackAddons()
	}

	views := make([]AddonView, 0, len(specs))
	for _, spec := range specs {
		ns := namespace
		if strings.TrimSpace(spec.ProbeNamespace) != "" {
			ns = strings.TrimSpace(spec.ProbeNamespace)
		}
		deployments, _ := clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
		statefulSets, _ := clientset.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})
		views = append(views, probeAddon(spec, deployments.Items, statefulSets.Items))
	}
	return views
}

func probeAddon(spec config.StackAddonSpec, deployments []appsv1.Deployment, statefulSets []appsv1.StatefulSet) AddonView {
	match := strings.ToLower(strings.TrimSpace(spec.Match))
	if match == "" {
		match = strings.ToLower(spec.ID)
	}
	for _, d := range deployments {
		if strings.Contains(strings.ToLower(d.Name), match) {
			return workloadToAddon(spec, "Deployment", d.Name, d.Status.ReadyReplicas, d.Spec.Replicas)
		}
	}
	for _, ss := range statefulSets {
		if strings.Contains(strings.ToLower(ss.Name), match) {
			return workloadToAddon(spec, "StatefulSet", ss.Name, ss.Status.ReadyReplicas, ss.Spec.Replicas)
		}
	}
	return AddonView{
		ID:           spec.ID,
		Label:        spec.Label,
		Status:       "not_installed",
		Reachability: probe.ReachDegraded,
		Detail:       "no matching Deployment or StatefulSet",
	}
}

func workloadToAddon(spec config.StackAddonSpec, kind, name string, ready int32, replicas *int32) AddonView {
	want := int32(1)
	if replicas != nil {
		want = *replicas
	}
	readyStr := fmt.Sprintf("%d/%d", ready, want)
	status := "installed"
	reach := probe.ReachOK
	detail := fmt.Sprintf("%s/%s ready", kind, name)
	if ready < want {
		status = "degraded"
		reach = probe.ReachDegraded
		detail = fmt.Sprintf("%s/%s progressing (%s)", kind, name, readyStr)
	}
	if want > 0 && ready == 0 {
		status = "degraded"
		reach = probe.ReachFail
		detail = fmt.Sprintf("%s/%s unavailable", kind, name)
	}
	return AddonView{
		ID:           spec.ID,
		Label:        spec.Label,
		Status:       status,
		Reachability: reach,
		Kind:         kind,
		Name:         name,
		Ready:        readyStr,
		Detail:       detail,
	}
}

func aggregateAddonsReachability(addons []AddonView) (probe.Reachability, string) {
	if len(addons) == 0 {
		return probe.ReachDegraded, "no stack addons configured"
	}
	installed := 0
	for _, a := range addons {
		if a.Status == "installed" {
			installed++
		}
		if a.Reachability == probe.ReachFail {
			return probe.ReachDegraded, fmt.Sprintf("%d/%d stack add-ons ready; %s failed", installed, len(addons), a.Label)
		}
	}
	if installed == 0 {
		return probe.ReachDegraded, "CI stack add-ons not installed yet (Gitea/Tekton/Registry planned P3)"
	}
	if installed < len(addons) {
		return probe.ReachDegraded, fmt.Sprintf("%d/%d stack add-ons ready", installed, len(addons))
	}
	return probe.ReachOK, fmt.Sprintf("%d/%d stack add-ons ready", installed, len(addons))
}
