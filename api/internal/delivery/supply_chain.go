package delivery

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

var dockerfileConfigMapNames = []string{
	"bifrost-api-stg-dockerfile",
	"bifrost-frontend-stg-dockerfile",
	"bifrost-worker-stg-dockerfile",
	"bifrost-socket-stg-dockerfile",
}

var trackedGiteaRepos = []string{
	"bifrost-trade-core",
	"bifrost-trade-worker",
	"bifrost-trade-socket",
	"bifrost-trade-api",
	"bifrost-trade-frontend",
	"bifrost-trade-infra",
	"bifrost-ui",
}

var stgImageDeployments = []string{
	"nginx", "frontend",
	"api-monitor", "api-massive", "api-docs", "api-ops", "api-trading",
	"api-strategy", "api-portfolio", "api-market", "api-research",
	"daemon", "account-sync", "celery-worker",
	"ib-ingestor", "ib-account-agent", "ib-operator", "massive-ws",
}

func (s *Service) stgNamespace() string {
	if s.entry != nil {
		for _, ns := range s.entry.BifrostNamespaces {
			if ns == "bifrost-stg" {
				return ns
			}
		}
	}
	return "bifrost-stg"
}

func (s *Service) SupplyChain(ctx context.Context) SupplyChainResponse {
	now := time.Now().UTC()
	out := SupplyChainResponse{
		ClusterID:       s.clusterID(),
		CicdNamespace:   s.PipelinesNamespace(),
		StgNamespace:    s.stgNamespace(),
		Reachability:    probe.ReachFail,
		TrackedRepos:    append([]string(nil), trackedGiteaRepos...),
		DockerfileCMs:   []DockerfileConfigMapView{},
		StgWorkloads:    []StgWorkloadImageView{},
		GeneratedAt:     now,
		DefaultRevision: "main",
	}

	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		out.Detail = err.Error()
		if ce, ok := err.(*cluster.ClientError); ok {
			out.Reachability = ce.Reachability
			out.Detail = ce.Detail
		}
		return out
	}

	_, err = clientset.CoreV1().Secrets(out.CicdNamespace).Get(ctx, "gitea-bootstrap", metav1.GetOptions{})
	out.MirrorCredentialsConfigured = err == nil

	for _, name := range dockerfileConfigMapNames {
		cm, cmErr := clientset.CoreV1().ConfigMaps(out.CicdNamespace).Get(ctx, name, metav1.GetOptions{})
		if cmErr != nil {
			out.DockerfileCMs = append(out.DockerfileCMs, DockerfileConfigMapView{
				Name: name, Namespace: out.CicdNamespace, Present: false, Detail: cmErr.Error(),
			})
			continue
		}
		out.DockerfileCMs = append(out.DockerfileCMs, dockerfileCMFrom(cm))
	}

	for _, depName := range stgImageDeployments {
		dep, depErr := clientset.AppsV1().Deployments(out.StgNamespace).Get(ctx, depName, metav1.GetOptions{})
		if depErr != nil {
			continue
		}
		if len(dep.Spec.Template.Spec.Containers) == 0 {
			continue
		}
		out.StgWorkloads = append(out.StgWorkloads, StgWorkloadImageView{
			Deployment: depName,
			Namespace:  out.StgNamespace,
			Image:      dep.Spec.Template.Spec.Containers[0].Image,
		})
	}

	dyn, err := s.buildDynamicClient()
	if err == nil {
		runs, listErr := dyn.Resource(pipelineRunGVR).Namespace(out.CicdNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "tekton.dev/pipeline=bifrost-deliver-stg",
		})
		if listErr == nil {
			var views []PipelineRunView
			for _, item := range runs.Items {
				views = append(views, pipelineRunFromUnstructured(item, "bifrost-deliver-stg"))
			}
			sort.Slice(views, func(i, j int) bool {
				return pipelineRunStartedAt(views[i]) > pipelineRunStartedAt(views[j])
			})
			if len(views) > 0 {
				last := views[0]
				out.LastDeliverRun = &last
				for _, v := range views {
					if isPipelineRunSucceededView(v) {
						succ := v
						out.LastDeliverSuccess = &succ
						break
					}
				}
			}
		}

		taskRuns, trErr := dyn.Resource(taskRunGVR).Namespace(out.CicdNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "bifrost.io/supply-chain",
		})
		if trErr == nil && len(taskRuns.Items) > 0 {
			sort.Slice(taskRuns.Items, func(i, j int) bool {
				return taskRuns.Items[i].GetCreationTimestamp().After(taskRuns.Items[j].GetCreationTimestamp().Time)
			})
			latest := taskRunSummaryFrom(taskRuns.Items[0])
			out.LastSupplyChainTask = &latest
		}
	}

	out.Reachability = probe.ReachOK
	out.Detail = fmt.Sprintf(
		"%d Dockerfile CMs · %d STG workloads · mirror creds=%v",
		len(out.DockerfileCMs), len(out.StgWorkloads), out.MirrorCredentialsConfigured,
	)
	return out
}

// LastDeliverStgSuccess returns the most recent succeeded bifrost-deliver-stg PipelineRun, if any.
func (s *Service) LastDeliverStgSuccess(ctx context.Context) *PipelineRunView {
	return s.lastDeliverSuccess(ctx, "bifrost-deliver-stg")
}

// LastDeliverProdSuccess returns the most recent succeeded bifrost-deliver-prod PipelineRun, if any.
func (s *Service) LastDeliverProdSuccess(ctx context.Context) *PipelineRunView {
	return s.lastDeliverSuccess(ctx, "bifrost-deliver-prod")
}

// LastDeliverProdRun returns the most recent bifrost-deliver-prod PipelineRun regardless of status.
func (s *Service) LastDeliverProdRun(ctx context.Context) *PipelineRunView {
	return s.lastDeliverRun(ctx, "bifrost-deliver-prod")
}

func (s *Service) lastDeliverRun(ctx context.Context, pipelineName string) *PipelineRunView {
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return nil
	}
	ns := s.PipelinesNamespace()
	runs, listErr := dyn.Resource(pipelineRunGVR).Namespace(ns).List(ctx, metav1.ListOptions{
		LabelSelector: "tekton.dev/pipeline=" + pipelineName,
	})
	if listErr != nil || len(runs.Items) == 0 {
		return nil
	}
	var views []PipelineRunView
	for _, item := range runs.Items {
		views = append(views, pipelineRunFromUnstructured(item, pipelineName))
	}
	sort.Slice(views, func(i, j int) bool {
		return pipelineRunStartedAt(views[i]) > pipelineRunStartedAt(views[j])
	})
	if len(views) == 0 {
		return nil
	}
	last := views[0]
	return &last
}

func (s *Service) lastDeliverSuccess(ctx context.Context, pipelineName string) *PipelineRunView {
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return nil
	}
	ns := s.PipelinesNamespace()
	runs, listErr := dyn.Resource(pipelineRunGVR).Namespace(ns).List(ctx, metav1.ListOptions{
		LabelSelector: "tekton.dev/pipeline=" + pipelineName,
	})
	if listErr != nil || len(runs.Items) == 0 {
		return nil
	}
	var views []PipelineRunView
	for _, item := range runs.Items {
		views = append(views, pipelineRunFromUnstructured(item, pipelineName))
	}
	sort.Slice(views, func(i, j int) bool {
		return pipelineRunStartedAt(views[i]) > pipelineRunStartedAt(views[j])
	})
	for _, v := range views {
		if isPipelineRunSucceededView(v) {
			succ := v
			return &succ
		}
	}
	return nil
}

// ProdRedisInCluster reports redis deployment readiness in bifrost-prod (K3s in-cluster; no host NodePort).
func (s *Service) ProdRedisInCluster(ctx context.Context) (probe.Reachability, string) {
	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		return probe.ReachFail, "cluster client: " + err.Error()
	}
	ns := "bifrost-prod"
	dep, depErr := clientset.AppsV1().Deployments(ns).Get(ctx, "redis", metav1.GetOptions{})
	if depErr != nil {
		return probe.ReachFail, fmt.Sprintf("deployment/redis in %s: %v", ns, depErr)
	}
	ready := int32(0)
	if dep.Status.ReadyReplicas > 0 {
		ready = dep.Status.ReadyReplicas
	}
	desired := dep.Status.Replicas
	if desired == 0 {
		desired = *dep.Spec.Replicas
	}
	if ready > 0 && ready >= desired {
		return probe.ReachOK, fmt.Sprintf("bifrost-prod/redis %d/%d ready (in-cluster)", ready, desired)
	}
	return probe.ReachDegraded, fmt.Sprintf("bifrost-prod/redis %d/%d ready", ready, desired)
}

// DevRedisInCluster reports redis deployment readiness in bifrost-dev.
func (s *Service) DevRedisInCluster(ctx context.Context) (probe.Reachability, string) {
	return s.deploymentReadyInNS(ctx, "bifrost-dev", "redis")
}

// DevPostgresInCluster reports postgres deployment readiness in bifrost-dev.
func (s *Service) DevPostgresInCluster(ctx context.Context) (probe.Reachability, string) {
	return s.deploymentReadyInNS(ctx, "bifrost-dev", "postgres")
}

func (s *Service) deploymentReadyInNS(ctx context.Context, ns, name string) (probe.Reachability, string) {
	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		return probe.ReachFail, "cluster client: " + err.Error()
	}
	dep, depErr := clientset.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if depErr != nil {
		return probe.ReachFail, fmt.Sprintf("deployment/%s in %s: %v", name, ns, depErr)
	}
	ready := int32(0)
	if dep.Status.ReadyReplicas > 0 {
		ready = dep.Status.ReadyReplicas
	}
	desired := dep.Status.Replicas
	if desired == 0 && dep.Spec.Replicas != nil {
		desired = *dep.Spec.Replicas
	}
	if ready > 0 && ready >= desired {
		return probe.ReachOK, fmt.Sprintf("%s/%s %d/%d ready (in-cluster)", ns, name, ready, desired)
	}
	return probe.ReachDegraded, fmt.Sprintf("%s/%s %d/%d ready", ns, name, ready, desired)
}

func dockerfileCMFrom(cm *corev1.ConfigMap) DockerfileConfigMapView {
	keys := make([]string, 0, len(cm.Data))
	for k := range cm.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	size := 0
	for _, k := range keys {
		size += len(cm.Data[k])
	}
	updated := cm.CreationTimestamp.UTC().Format(time.RFC3339)
	if cm.ManagedFields != nil && len(cm.ManagedFields) > 0 {
		// resourceVersion changes on update; use it as a change indicator
		_ = cm.ResourceVersion
	}
	return DockerfileConfigMapView{
		Name:            cm.Name,
		Namespace:       cm.Namespace,
		Present:         true,
		ResourceVersion: cm.ResourceVersion,
		UpdatedAt:       updated,
		FileKeys:        keys,
		ApproxBytes:     size,
	}
}

func isPipelineRunSucceededView(v PipelineRunView) bool {
	st := strings.ToLower(v.Status)
	re := strings.ToLower(v.Reason)
	return st == "true" || st == "succeeded" || re == "succeeded" || re == "completed"
}

func taskRunSummaryFrom(obj unstructured.Unstructured) SupplyChainTaskRunView {
	view := SupplyChainTaskRunView{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
		Task:      "",
		Status:    "Unknown",
	}
	if ref, ok, _ := unstructured.NestedString(obj.Object, "spec", "taskRef", "name"); ok {
		view.Task = ref
	}
	if act, ok := obj.GetLabels()["bifrost.io/supply-chain"]; ok {
		view.Actuation = act
	}
	cond, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if found && len(cond) > 0 {
		if m, ok := cond[0].(map[string]any); ok {
			if st, ok := m["status"].(string); ok {
				view.Status = st
			}
			if reason, ok := m["reason"].(string); ok {
				view.Reason = reason
			}
		}
	}
	if t, ok, _ := unstructured.NestedString(obj.Object, "status", "startTime"); ok {
		view.StartTime = t
	}
	if t, ok, _ := unstructured.NestedString(obj.Object, "status", "completionTime"); ok {
		view.CompletionTime = t
	}
	return view
}

func (s *Service) TriggerMirrorSync(ctx context.Context) (cluster.ActuationResponse, SupplyChainTaskRunView, error) {
	return s.createSupplyChainTaskRun(ctx, "bifrost-gitea-mirror-sync", "mirror-sync", nil)
}

func (s *Service) RefreshDockerfileConfigMaps(ctx context.Context, revision string) (cluster.ActuationResponse, SupplyChainTaskRunView, error) {
	rev := strings.TrimSpace(revision)
	if rev == "" {
		rev = "main"
	}
	return s.createSupplyChainTaskRun(ctx, "bifrost-refresh-dockerfile-cms", "dockerfile-configmaps", map[string]string{
		"revision": rev,
	})
}

func (s *Service) createSupplyChainTaskRun(
	ctx context.Context,
	taskName, actuation string,
	params map[string]string,
) (cluster.ActuationResponse, SupplyChainTaskRunView, error) {
	now := time.Now().UTC()
	ns := s.PipelinesNamespace()
	runName := fmt.Sprintf("%s-%d", taskName, now.Unix())
	target := fmt.Sprintf("TaskRun/%s/%s", ns, runName)
	resp := cluster.ActuationResponse{
		OK:          false,
		Action:      "delivery.supply-chain." + actuation,
		Target:      target,
		Changed:     false,
		GeneratedAt: now,
	}
	var empty SupplyChainTaskRunView

	dyn, err := s.buildDynamicClient()
	if err != nil {
		resp.Message = err.Error()
		return resp, empty, err
	}

	specParams := []map[string]any{}
	for k, v := range params {
		specParams = append(specParams, map[string]any{"name": k, "value": v})
	}
	spec := map[string]any{
		"taskRef": map[string]any{"name": taskName},
	}
	for k, v := range amd64CITaskRunTemplate() {
		spec[k] = v
	}
	if len(specParams) > 0 {
		spec["params"] = specParams
	}
	if taskName == "bifrost-refresh-dockerfile-cms" {
		spec["serviceAccountName"] = "tekton-deliver"
		spec["workspaces"] = []map[string]any{
			{
				"name": "source",
				"volumeClaimTemplate": map[string]any{
					"spec": map[string]any{
						"accessModes":      []any{"ReadWriteOnce"},
						"storageClassName": "local-path",
						"resources": map[string]any{
							"requests": map[string]any{"storage": "2Gi"},
						},
					},
				},
			},
		}
	}

	obj := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "tekton.dev/v1",
			"kind":       "TaskRun",
			"metadata": map[string]any{
				"name":      runName,
				"namespace": ns,
				"labels": map[string]any{
					"bifrost.io/supply-chain": actuation,
					"bifrost.io/trigger":      "platform-api",
				},
			},
			"spec": spec,
		},
	}

	created, err := dyn.Resource(taskRunGVR).Namespace(ns).Create(ctx, &obj, metav1.CreateOptions{})
	if err != nil {
		resp.Message = fmt.Sprintf("create TaskRun: %v", err)
		return resp, empty, err
	}

	view := taskRunSummaryFrom(*created)
	resp.OK = true
	resp.Changed = true
	resp.Message = fmt.Sprintf("TaskRun %s started (%s)", runName, taskName)
	return resp, view, nil
}
