package gitops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

var applicationGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "applications",
}

type Service struct {
	entry           *config.ClusterEntry
	cluster         *cluster.Service
	dynamicFactory  func() (dynamic.Interface, error)
}

func NewService(entry *config.ClusterEntry) *Service {
	return &Service{
		entry:   entry,
		cluster: cluster.NewService(entry),
	}
}

func (s *Service) Apps(ctx context.Context) AppsResponse {
	now := time.Now().UTC()
	base := s.baseMeta(now)

	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		return s.failApps(base, err, now)
	}

	argoNS := s.entry.ResolvedArgoCDNamespace()
	appsNS := s.entry.ResolvedApplicationsNamespace()
	serverMatch := s.entry.ResolvedArgoCDServerMatch()

	if _, nsErr := clientset.CoreV1().Namespaces().Get(ctx, argoNS, metav1.GetOptions{}); nsErr != nil {
		return AppsResponse{
			ClusterID:             base.ClusterID,
			ArgoCDNamespace:       argoNS,
			ApplicationsNamespace: appsNS,
			ArgoCDStatus:          "not_installed",
			Reachability:          probe.ReachDegraded,
			Detail:                fmt.Sprintf("namespace %s not found (Argo CD not installed)", argoNS),
			Apps:                  []ApplicationView{},
			GeneratedAt:           now,
		}
	}

	serverView := findArgoCDServer(ctx, clientset, argoNS, serverMatch)
	if serverView == nil {
		return AppsResponse{
			ClusterID:             base.ClusterID,
			ArgoCDNamespace:       argoNS,
			ApplicationsNamespace: appsNS,
			ArgoCDStatus:          "not_installed",
			Reachability:          probe.ReachDegraded,
			Detail:                fmt.Sprintf("%s not detected in namespace %s", serverMatch, argoNS),
			Apps:                  []ApplicationView{},
			GeneratedAt:           now,
		}
	}

	apps, appsDetail, appsReach := s.listApplications(ctx, appsNS)
	argoStatus, reach, detail := aggregateArgoStatus(serverView, appsReach, appsDetail, len(apps))

	return AppsResponse{
		ClusterID:             base.ClusterID,
		ArgoCDNamespace:       argoNS,
		ApplicationsNamespace: appsNS,
		ArgoCDStatus:          argoStatus,
		Reachability:          reach,
		Detail:                detail,
		Server:                serverView,
		Apps:                  apps,
		GeneratedAt:           now,
	}
}

type baseMeta struct {
	ClusterID string
}

func (s *Service) baseMeta(now time.Time) baseMeta {
	id := "unknown"
	if s.entry != nil && s.entry.ID != "" {
		id = s.entry.ID
	}
	_ = now
	return baseMeta{ClusterID: id}
}

func (s *Service) failApps(base baseMeta, err error, now time.Time) AppsResponse {
	reach := probe.ReachFail
	detail := err.Error()
	if ce, ok := err.(*cluster.ClientError); ok {
		reach = ce.Reachability
		detail = ce.Detail
	}
	argoNS := "cicd"
	appsNS := "cicd"
	if s.entry != nil {
		argoNS = s.entry.ResolvedArgoCDNamespace()
		appsNS = s.entry.ResolvedApplicationsNamespace()
	}
	return AppsResponse{
		ClusterID:             base.ClusterID,
		ArgoCDNamespace:       argoNS,
		ApplicationsNamespace: appsNS,
		ArgoCDStatus:          "unavailable",
		Reachability:          reach,
		Detail:                detail,
		Apps:                  []ApplicationView{},
		GeneratedAt:           now,
	}
}

func findArgoCDServer(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace, match string,
) *ArgoCDServerView {
	match = strings.ToLower(match)
	deployments, _ := clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	for _, d := range deployments.Items {
		if strings.Contains(strings.ToLower(d.Name), match) {
			return deploymentToServerView(d)
		}
	}
	statefulSets, _ := clientset.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	for _, ss := range statefulSets.Items {
		if strings.Contains(strings.ToLower(ss.Name), match) {
			return statefulSetToServerView(ss)
		}
	}
	return nil
}

func deploymentToServerView(d appsv1.Deployment) *ArgoCDServerView {
	replicas := int32(1)
	if d.Spec.Replicas != nil {
		replicas = *d.Spec.Replicas
	}
	ready := fmt.Sprintf("%d/%d", d.Status.ReadyReplicas, replicas)
	status := "Ready"
	reach := probe.ReachOK
	if d.Status.ReadyReplicas < replicas {
		status = "Progressing"
		reach = probe.ReachDegraded
	}
	if replicas > 0 && d.Status.ReadyReplicas == 0 {
		status = "Unavailable"
		reach = probe.ReachFail
	}
	return &ArgoCDServerView{
		Kind:         "Deployment",
		Name:         d.Name,
		Ready:        ready,
		Status:       status,
		Reachability: reach,
	}
}

func statefulSetToServerView(ss appsv1.StatefulSet) *ArgoCDServerView {
	replicas := int32(1)
	if ss.Spec.Replicas != nil {
		replicas = *ss.Spec.Replicas
	}
	ready := fmt.Sprintf("%d/%d", ss.Status.ReadyReplicas, replicas)
	status := "Ready"
	reach := probe.ReachOK
	if ss.Status.ReadyReplicas < replicas {
		status = "Progressing"
		reach = probe.ReachDegraded
	}
	if replicas > 0 && ss.Status.ReadyReplicas == 0 {
		status = "Unavailable"
		reach = probe.ReachFail
	}
	return &ArgoCDServerView{
		Kind:         "StatefulSet",
		Name:         ss.Name,
		Ready:        ready,
		Status:       status,
		Reachability: reach,
	}
}

func (s *Service) listApplications(ctx context.Context, namespace string) ([]ApplicationView, string, probe.Reachability) {
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return []ApplicationView{}, fmt.Sprintf("applications API unavailable: %v", err), probe.ReachDegraded
	}

	list, err := dyn.Resource(applicationGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		if isCRDMissing(err) {
			return []ApplicationView{}, "Argo CD Application CRD not registered (install Argo CD)", probe.ReachDegraded
		}
		return []ApplicationView{}, fmt.Sprintf("list applications: %v", err), probe.ReachDegraded
	}

	views := make([]ApplicationView, 0, len(list.Items))
	for _, item := range list.Items {
		views = append(views, applicationFromUnstructured(item))
	}
	if len(views) == 0 {
		return views, "Argo CD installed; no Application resources yet", probe.ReachOK
	}
	return views, fmt.Sprintf("%d application(s)", len(views)), probe.ReachOK
}

func (s *Service) buildDynamicClient() (dynamic.Interface, error) {
	if s.dynamicFactory != nil {
		return s.dynamicFactory()
	}
	cfg, _, err := s.cluster.RestConfig()
	if err != nil {
		return nil, err
	}
	return dynamic.NewForConfig(cfg)
}

func isCRDMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "the server could not find the requested resource") ||
		strings.Contains(msg, "no matches for kind") ||
		strings.Contains(msg, "could not find the requested resource")
}

func applicationFromUnstructured(obj unstructured.Unstructured) ApplicationView {
	view := ApplicationView{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
	}
	if project, ok, _ := unstructured.NestedString(obj.Object, "spec", "project"); ok {
		view.Project = project
	}
	if sync, ok, _ := unstructured.NestedString(obj.Object, "status", "sync", "status"); ok {
		view.SyncStatus = sync
	} else {
		view.SyncStatus = "Unknown"
	}
	if health, ok, _ := unstructured.NestedString(obj.Object, "status", "health", "status"); ok {
		view.HealthStatus = health
	} else {
		view.HealthStatus = "Unknown"
	}
	if rev, ok, _ := unstructured.NestedString(obj.Object, "status", "sync", "revision"); ok {
		view.Revision = rev
	}
	destNS, _, _ := unstructured.NestedString(obj.Object, "spec", "destination", "namespace")
	destName, _, _ := unstructured.NestedString(obj.Object, "spec", "destination", "name")
	switch {
	case destNS != "" && destName != "":
		view.Destination = destNS + "/" + destName
	case destNS != "":
		view.Destination = destNS
	case destName != "":
		view.Destination = destName
	}
	return view
}

func aggregateArgoStatus(
	server *ArgoCDServerView,
	appsReach probe.Reachability,
	appsDetail string,
	appCount int,
) (argoStatus string, reach probe.Reachability, detail string) {
	if server.Reachability == probe.ReachFail {
		return "degraded", probe.ReachDegraded, fmt.Sprintf("%s %s not ready (%s); %s", server.Kind, server.Name, server.Ready, appsDetail)
	}
	if server.Reachability == probe.ReachDegraded {
		return "degraded", probe.ReachDegraded, fmt.Sprintf("%s %s progressing (%s); %s", server.Kind, server.Name, server.Ready, appsDetail)
	}
	if appsReach == probe.ReachFail {
		return "degraded", probe.ReachDegraded, appsDetail
	}
	if appsReach == probe.ReachDegraded {
		return "installed", probe.ReachDegraded, fmt.Sprintf("%s ready; %s", server.Name, appsDetail)
	}
	if appCount == 0 {
		return "installed", probe.ReachOK, fmt.Sprintf("%s ready; %s", server.Name, appsDetail)
	}
	return "installed", probe.ReachOK, fmt.Sprintf("%s ready; %s", server.Name, appsDetail)
}

func (s *Service) SyncApplication(ctx context.Context, name string) (cluster.ActuationResponse, error) {
	now := time.Now().UTC()
	ns := s.entry.ResolvedApplicationsNamespace()
	target := fmt.Sprintf("Application/%s/%s", ns, name)
	resp := cluster.ActuationResponse{
		OK:           false,
		Action:       "gitops.sync",
		Target:       target,
		Changed:      false,
		Message:      "",
		GeneratedAt:  now,
	}

	dyn, err := s.buildDynamicClient()
	if err != nil {
		resp.Message = err.Error()
		return resp, err
	}

	if _, err := dyn.Resource(applicationGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{}); err != nil {
		resp.Message = fmt.Sprintf("application %s not found in %s: %v", name, ns, err)
		return resp, fmt.Errorf("%s", resp.Message)
	}

	patch := map[string]any{
		"operation": map[string]any{
			"initiatedBy": map[string]any{
				"username": "platform-api",
			},
			"sync": map[string]any{
				"revision": "HEAD",
			},
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		resp.Message = err.Error()
		return resp, err
	}

	_, err = dyn.Resource(applicationGVR).Namespace(ns).Patch(
		ctx,
		name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	if err != nil {
		resp.Message = fmt.Sprintf("sync patch failed: %v", err)
		return resp, err
	}

	resp.OK = true
	resp.Changed = true
	resp.Message = fmt.Sprintf("Sync requested for Application %s in %s", name, ns)
	return resp, nil
}
