package delivery

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

var (
	pipelineGVR    = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelines"}
	pipelineRunGVR = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}
)

type Service struct {
	entry          *config.ClusterEntry
	cluster        *cluster.Service
	dynamicFactory func() (dynamic.Interface, error)
	httpClient     *http.Client
}

func NewService(entry *config.ClusterEntry) *Service {
	return &Service{
		entry:   entry,
		cluster: cluster.NewService(entry),
		httpClient: &http.Client{
			Timeout: 8 * time.Second,
		},
	}
}

func (s *Service) PipelinesNamespace() string {
	if v := strings.TrimSpace(s.entry.ResolvedStackNamespace()); v != "" {
		return v
	}
	return "cicd"
}

func (s *Service) clusterID() string {
	if s.entry != nil && s.entry.ID != "" {
		return s.entry.ID
	}
	return "unknown"
}

func (s *Service) Pipelines(ctx context.Context) PipelinesResponse {
	now := time.Now().UTC()
	ns := s.PipelinesNamespace()
	base := PipelinesResponse{
		ClusterID:    s.clusterID(),
		Namespace:    ns,
		Reachability: probe.ReachFail,
		Pipelines:    []PipelineView{},
		GeneratedAt:  now,
	}

	dyn, err := s.buildDynamicClient()
	if err != nil {
		base.Detail = err.Error()
		if ce, ok := err.(*cluster.ClientError); ok {
			base.Reachability = ce.Reachability
			base.Detail = ce.Detail
		}
		return base
	}

	list, err := dyn.Resource(pipelineGVR).Namespace(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		base.Reachability = probe.ReachDegraded
		base.Detail = fmt.Sprintf("list pipelines: %v", err)
		if isCRDMissing(err) {
			base.Detail = "Tekton Pipeline CRD not registered (install Tekton Pipelines)"
		}
		return base
	}

	views := make([]PipelineView, 0, len(list.Items))
	for _, item := range list.Items {
		views = append(views, s.enrichPipelineView(ctx, item.GetName(), item.GetNamespace()))
	}
	reach := probe.ReachOK
	detail := fmt.Sprintf("%d pipeline(s) in %s", len(views), ns)
	if len(views) == 0 {
		detail = fmt.Sprintf("Tekton ready; no Pipeline resources in %s yet", ns)
	}
	return PipelinesResponse{
		ClusterID:    s.clusterID(),
		Namespace:    ns,
		Reachability: reach,
		Detail:       detail,
		Pipelines:    views,
		GeneratedAt:  now,
	}
}

func (s *Service) PipelineRuns(ctx context.Context, pipelineName string) PipelineRunsResponse {
	now := time.Now().UTC()
	ns := s.PipelinesNamespace()
	base := PipelineRunsResponse{
		ClusterID:    s.clusterID(),
		Namespace:    ns,
		Pipeline:     pipelineName,
		Reachability: probe.ReachFail,
		Runs:         []PipelineRunView{},
		GeneratedAt:  now,
	}

	dyn, err := s.buildDynamicClient()
	if err != nil {
		base.Detail = err.Error()
		if ce, ok := err.(*cluster.ClientError); ok {
			base.Reachability = ce.Reachability
			base.Detail = ce.Detail
		}
		return base
	}

	list, err := dyn.Resource(pipelineRunGVR).Namespace(ns).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("tekton.dev/pipeline=%s", pipelineName),
	})
	if err != nil {
		base.Reachability = probe.ReachDegraded
		base.Detail = fmt.Sprintf("list pipeline runs: %v", err)
		return base
	}

	// Fallback: filter by spec.pipelineRef.name when label missing (older runs).
	if len(list.Items) == 0 {
		all, listErr := dyn.Resource(pipelineRunGVR).Namespace(ns).List(ctx, metav1.ListOptions{})
		if listErr == nil {
			for _, item := range all.Items {
				refName, _, _ := unstructured.NestedString(item.Object, "spec", "pipelineRef", "name")
				if refName == pipelineName {
					list.Items = append(list.Items, item)
				}
			}
		}
	}

	views := make([]PipelineRunView, 0, len(list.Items))
	for _, item := range list.Items {
		views = append(views, pipelineRunFromUnstructured(item, pipelineName))
	}
	sort.Slice(views, func(i, j int) bool {
		return pipelineRunStartedAt(views[i]) > pipelineRunStartedAt(views[j])
	})
	return PipelineRunsResponse{
		ClusterID:    s.clusterID(),
		Namespace:    ns,
		Pipeline:     pipelineName,
		Reachability: probe.ReachOK,
		Detail:       fmt.Sprintf("%d run(s) for pipeline %s", len(views), pipelineName),
		Runs:         views,
		GeneratedAt:  now,
	}
}

func (s *Service) StartPipelineRun(ctx context.Context, pipelineName string) (cluster.ActuationResponse, PipelineRunView, error) {
	now := time.Now().UTC()
	ns := s.PipelinesNamespace()
	target := fmt.Sprintf("PipelineRun/%s/%s", ns, pipelineName)
	resp := cluster.ActuationResponse{
		OK:          false,
		Action:      "delivery.pipeline.run",
		Target:      target,
		Changed:     false,
		GeneratedAt: now,
	}
	var empty PipelineRunView

	dyn, err := s.buildDynamicClient()
	if err != nil {
		resp.Message = err.Error()
		return resp, empty, err
	}

	if _, err := dyn.Resource(pipelineGVR).Namespace(ns).Get(ctx, pipelineName, metav1.GetOptions{}); err != nil {
		resp.Message = fmt.Sprintf("pipeline %s not found in %s: %v", pipelineName, ns, err)
		return resp, empty, fmt.Errorf("%s", resp.Message)
	}

	if isKanikoPipeline(pipelineName) {
		if pf := s.PipelinePreflight(ctx, pipelineName); !pf.BuildReady {
			resp.Message = pf.Reason
			return resp, empty, fmt.Errorf("%s", pf.Reason)
		}
	}

	runName := fmt.Sprintf("%s-%d", pipelineName, now.Unix())
	spec := map[string]any{
		"pipelineRef": map[string]any{
			"name": pipelineName,
		},
	}
	if ws := pipelineRunWorkspaces(pipelineName); len(ws) > 0 {
		spec["workspaces"] = ws
	}
	if isKanikoPipeline(pipelineName) {
		spec["taskRunTemplate"] = amd64CITaskRunTemplate()
	}
	if pipelineName == "bifrost-deliver-stg" {
		spec["taskRunSpecs"] = []map[string]any{
			{"pipelineTaskName": "rollout", "serviceAccountName": "tekton-deliver"},
			{"pipelineTaskName": "gitops-sync", "serviceAccountName": "tekton-deliver"},
		}
	}
	obj := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "tekton.dev/v1",
			"kind":       "PipelineRun",
			"metadata": map[string]any{
				"name":      runName,
				"namespace": ns,
				"labels": map[string]any{
					"tekton.dev/pipeline": pipelineName,
					"bifrost.io/trigger":  "platform-api",
				},
			},
			"spec": spec,
		},
	}

	created, err := dyn.Resource(pipelineRunGVR).Namespace(ns).Create(ctx, &obj, metav1.CreateOptions{})
	if err != nil {
		resp.Message = fmt.Sprintf("create PipelineRun: %v", err)
		return resp, empty, err
	}

	view := pipelineRunFromUnstructured(*created, pipelineName)
	resp.OK = true
	resp.Changed = true
	resp.Target = fmt.Sprintf("PipelineRun/%s/%s", ns, runName)
	resp.Message = fmt.Sprintf("PipelineRun %s created for pipeline %s", runName, pipelineName)
	return resp, view, nil
}

func (s *Service) DeletePipelineRun(ctx context.Context, namespace, runName string) (cluster.ActuationResponse, error) {
	now := time.Now().UTC()
	ns := namespace
	if ns == "" {
		ns = s.PipelinesNamespace()
	}
	target := fmt.Sprintf("PipelineRun/%s/%s", ns, runName)
	resp := cluster.ActuationResponse{
		OK:          false,
		Action:      "delivery.pipeline.delete",
		Target:      target,
		Changed:     false,
		GeneratedAt: now,
	}

	dyn, err := s.buildDynamicClient()
	if err != nil {
		resp.Message = err.Error()
		return resp, err
	}

	err = dyn.Resource(pipelineRunGVR).Namespace(ns).Delete(ctx, runName, metav1.DeleteOptions{})
	if err != nil {
		resp.Message = fmt.Sprintf("delete PipelineRun: %v", err)
		return resp, err
	}

	resp.OK = true
	resp.Changed = true
	resp.Message = fmt.Sprintf("PipelineRun %s deleted from %s", runName, ns)
	return resp, nil
}

func (s *Service) RunLogs(ctx context.Context, namespace, runName string) (RunLogsResponse, error) {
	now := time.Now().UTC()
	ns := namespace
	if ns == "" {
		ns = s.PipelinesNamespace()
	}
	out := RunLogsResponse{
		ClusterID:   s.clusterID(),
		Namespace:   ns,
		RunName:     runName,
		GeneratedAt: now,
	}

	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		return out, err
	}

	pods, err := clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("tekton.dev/pipelineRun=%s", runName),
	})
	if err != nil {
		return out, err
	}
	if len(pods.Items) == 0 {
		out.Logs = "(no pods yet — PipelineRun may still be starting)"
		return out, nil
	}

	var b strings.Builder
	for _, pod := range pods.Items {
		for _, c := range pod.Spec.Containers {
			logOpts := &corev1.PodLogOptions{Container: c.Name, TailLines: int64Ptr(200)}
			req := clientset.CoreV1().Pods(ns).GetLogs(pod.Name, logOpts)
			stream, logErr := req.Stream(ctx)
			if logErr != nil {
				fmt.Fprintf(&b, "=== %s/%s (unavailable: %v)\n", pod.Name, c.Name, logErr)
				continue
			}
			data, readErr := io.ReadAll(stream)
			_ = stream.Close()
			fmt.Fprintf(&b, "=== %s/%s\n", pod.Name, c.Name)
			if readErr != nil {
				fmt.Fprintf(&b, "(read error: %v)\n", readErr)
				continue
			}
			b.Write(data)
			if len(data) > 0 && data[len(data)-1] != '\n' {
				b.WriteByte('\n')
			}
		}
	}
	out.Logs = b.String()
	if out.Logs == "" {
		out.Logs = "(pods found but no log lines yet)"
	}
	return out, nil
}

func int64Ptr(v int64) *int64 { return &v }

// amd64CITaskRunTemplate pins Kaniko/RUN steps to amd64 nodes (see install-phase-b-stg.sh).
// ARM workers cannot execute RUN layers from --custom-platform=linux/amd64 images (exec format error).
func amd64CITaskRunTemplate() map[string]any {
	return map[string]any{
		"podTemplate": map[string]any{
			"nodeSelector": map[string]any{
				"kubernetes.io/arch": "amd64",
			},
			"tolerations": []map[string]any{
				{
					"key":      "node-role.kubernetes.io/control-plane",
					"operator": "Exists",
					"effect":   "NoSchedule",
				},
			},
		},
	}
}

func pipelineRunWorkspaces(pipelineName string) []map[string]any {
	buildContextPVC := map[string]any{
		"name": "build-context",
		"volumeClaimTemplate": map[string]any{
			"spec": map[string]any{
				"accessModes":      []any{"ReadWriteOnce"},
				"storageClassName": "local-path",
				"resources": map[string]any{
					"requests": map[string]any{"storage": "5Gi"},
				},
			},
		},
	}
	switch pipelineName {
	case "bifrost-deliver-stg":
		return []map[string]any{
			map[string]any{
				"name": "build-context",
				"volumeClaimTemplate": map[string]any{
					"spec": map[string]any{
						"accessModes":      []any{"ReadWriteOnce"},
						"storageClassName": "local-path",
						"resources": map[string]any{
							"requests": map[string]any{"storage": "10Gi"},
						},
					},
				},
			},
		}
	case "bifrost-build-stg":
		return []map[string]any{
			{"name": "api-source", "emptyDir": map[string]any{}},
			{"name": "frontend-source", "emptyDir": map[string]any{}},
		}
	case "bifrost-build-frontend-stg":
		return []map[string]any{buildContextPVC}
	default:
		return nil
	}
}

func (s *Service) StgSmoke(ctx context.Context) StgSmokeResponse {
	now := time.Now().UTC()
	out := StgSmokeResponse{
		ClusterID:    s.clusterID(),
		Reachability: probe.ReachUnknown,
		Detail:       "stg smoke probes not configured",
		Targets:      []StgSmokeTargetView{},
		GeneratedAt:  now,
	}
	if s.entry == nil {
		out.Detail = "no cluster configured"
		return out
	}

	probes := []struct {
		id  string
		url string
	}{}

	if fe := s.entry.ResolvedStgFrontendURL(); fe != "" {
		probes = append(probes, struct {
			id  string
			url string
		}{id: "stg-frontend", url: fe})
	}

	gw := strings.TrimRight(s.entry.ResolvedStgGatewayURL(), "/")
	if gw != "" {
		for _, domain := range s.entry.ResolvedStgAPIDomains() {
			probes = append(probes, struct {
				id  string
				url string
			}{
				id:  "stg-api-" + domain,
				url: gw + "/api/" + domain + stgAPIProbePath(domain),
			})
		}
	} else {
		if u := s.entry.ResolvedStgAPIMonitorURL(); u != "" {
			probes = append(probes, struct {
				id  string
				url string
			}{id: "stg-api-monitor", url: u})
		}
	}

	for _, p := range probes {
		if p.url == "" {
			continue
		}
		out.Targets = append(out.Targets, s.probeStgHTTP(ctx, p.id, p.url))
	}
	if len(out.Targets) == 0 {
		out.Detail = "configure stg_smoke URLs in clusters.yaml or PLATFORM_STG_* env"
		return out
	}

	apiOK := 0
	apiTotal := 0
	for _, t := range out.Targets {
		if strings.HasPrefix(t.ID, "stg-api-") {
			apiTotal++
			if t.Reachability == probe.ReachOK || t.Reachability == probe.ReachDegraded {
				apiOK++
			}
		}
	}
	switch {
	case apiTotal > 0 && apiOK == apiTotal:
		out.Reachability = probe.ReachOK
		out.Detail = fmt.Sprintf("stg %d/%d API domains reachable", apiOK, apiTotal)
	case apiOK > 0:
		out.Reachability = probe.ReachDegraded
		out.Detail = fmt.Sprintf("stg %d/%d API domains reachable", apiOK, apiTotal)
	case len(out.Targets) > 0 && out.Targets[0].Reachability == probe.ReachFail:
		out.Reachability = probe.ReachFail
		out.Detail = "stg smoke unreachable"
	default:
		out.Reachability = probe.ReachDegraded
		out.Detail = "stg smoke partial"
	}
	return out
}

// stgAPIProbePath — monitor exposes rich GET /status; other domains use /health only.
func stgAPIProbePath(domain string) string {
	if domain == "monitor" {
		return "/status"
	}
	return "/health"
}

func (s *Service) probeStgHTTP(ctx context.Context, id, url string) StgSmokeTargetView {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return StgSmokeTargetView{
			ID: id, URL: url, Reachability: probe.ReachFail,
			Detail: "request error: " + err.Error(),
		}
	}
	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return StgSmokeTargetView{
			ID: id, URL: url, Reachability: probe.ReachFail,
			Detail: err.Error(),
		}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	reach := probe.ReachOK
	detail := fmt.Sprintf("HTTP %d", resp.StatusCode)
	switch {
	case resp.StatusCode == 200:
		reach = probe.ReachOK
	case resp.StatusCode == 503:
		reach = probe.ReachDegraded
	case resp.StatusCode >= 400:
		reach = probe.ReachFail
	default:
		reach = probe.ReachUnknown
	}
	return StgSmokeTargetView{ID: id, URL: url, Reachability: reach, Detail: detail}
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

func pipelineRunFromUnstructured(obj unstructured.Unstructured, pipelineName string) PipelineRunView {
	view := PipelineRunView{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
		Pipeline:  pipelineName,
		Status:    "Unknown",
	}
	if ref, ok, _ := unstructured.NestedString(obj.Object, "spec", "pipelineRef", "name"); ok && ref != "" {
		view.Pipeline = ref
	}
	cond, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if found && len(cond) > 0 {
		if m, ok := cond[0].(map[string]any); ok {
			if st, ok := m["status"].(string); ok && st != "" {
				view.Status = st
			}
			if reason, ok := m["reason"].(string); ok {
				view.Reason = reason
			}
			if typ, ok := m["type"].(string); ok && view.Status == "Unknown" {
				view.Status = typ
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

func pipelineRunStartedAt(view PipelineRunView) int64 {
	if view.StartTime != "" {
		if t, err := time.Parse(time.RFC3339, view.StartTime); err == nil {
			return t.UnixMilli()
		}
	}
	prefix := view.Pipeline + "-"
	if strings.HasPrefix(view.Name, prefix) {
		if sec, err := strconv.ParseInt(strings.TrimPrefix(view.Name, prefix), 10, 64); err == nil {
			return sec * 1000
		}
	}
	return 0
}

// SetDynamicFactoryForTest injects a fake dynamic client in unit tests.
func (s *Service) SetDynamicFactoryForTest(factory func() (dynamic.Interface, error)) {
	s.dynamicFactory = factory
}

// SetClusterForTest replaces the cluster service (for log tests).
func (s *Service) SetClusterForTest(cs *cluster.Service) {
	s.cluster = cs
}

// SetClientsetForTest wires kubernetes client for log tests.
func (s *Service) SetClientsetForTest(clientset kubernetes.Interface) {
	cs := cluster.NewService(s.entry)
	cs.SetClientFactoryForTest(func() (kubernetes.Interface, string, error) {
		return clientset, "fake", nil
	})
	s.cluster = cs
}
