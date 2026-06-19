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
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

var (
	taskRunGVR = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "taskruns"}
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
		"taskRunTemplate": amd64CITaskRunTemplate(),
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
