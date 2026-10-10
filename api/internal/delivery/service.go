package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

var (
	pipelineGVR    = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelines"}
	pipelineRunGVR = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}
	taskRunGVR     = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "taskruns"}
)

// sanitizeLabelValue converts a Git ref (e.g. "feature/release-ui-polish")
// into a valid K8s label value by replacing illegal characters with '-'.
// K8s labels: [a-zA-Z0-9._-], max 63 chars, must start/end alphanumeric.
func sanitizeLabelValue(v string) string {
	var b strings.Builder
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	s := b.String()
	s = strings.Trim(s, "-._")
	if len(s) > 63 {
		s = s[:63]
		s = strings.TrimRight(s, "-._")
	}
	return s
}

type Service struct {
	entry          *config.ClusterEntry
	cluster        *cluster.Service
	dynamicFactory func() (dynamic.Interface, error)
	httpClient     *http.Client
	policy         *actuationpolicy.Policy
	// giteaBase and mirrorCreds are test hooks. Production leaves them empty.
	giteaBase   string
	mirrorCreds func(ctx context.Context) (string, string, error)
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

func (s *Service) StartPipelineRun(ctx context.Context, pipelineName, revision, tag, who string, extra map[string]string) (cluster.ActuationResponse, PipelineRunView, error) {
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

	rev := strings.TrimSpace(revision)
	if rev == "" {
		rev = "main"
	}
	if err := validateRevision(rev); err != nil {
		resp.Message = err.Error()
		return resp, empty, fmt.Errorf("%s", resp.Message)
	}
	if requiresFullSHA(pipelineName) && !isFullGitSHA(rev) {
		resp.Message = fmt.Sprintf("revision %q must be a 40-character lowercase git SHA", rev)
		return resp, empty, fmt.Errorf("%s", resp.Message)
	}
	if msg := s.releaseWindowMessage(ctx, pipelineName, who); msg != "" {
		resp.Message = msg
		return resp, empty, fmt.Errorf("%s", msg)
	}

	// Multi-repo guard: refuse to start if the revision is genuinely missing in
	// any repo the pipeline clones — otherwise it fails halfway at a clone-*
	// task. Only block when probes had full visibility (reachability ok); never
	// block on network/probe uncertainty.
	if pf := s.RefPreflight(ctx, pipelineName, rev); pf.Reachability == probe.ReachOK && len(pf.Missing) > 0 {
		resp.Message = fmt.Sprintf(
			"revision %q is missing in: %s — create the same branch/tag in those repos (or deploy a ref present in all)",
			rev, strings.Join(pf.Missing, ", "))
		return resp, empty, fmt.Errorf("%s", resp.Message)
	}

	dyn, err := s.buildDynamicClient()
	if err != nil {
		resp.Message = err.Error()
		return resp, empty, err
	}

	pipe, getErr := dyn.Resource(pipelineGVR).Namespace(ns).Get(ctx, pipelineName, metav1.GetOptions{})
	if getErr != nil {
		resp.Message = fmt.Sprintf("pipeline %s not found in %s: %v", pipelineName, ns, getErr)
		return resp, empty, fmt.Errorf("%s", resp.Message)
	}

	if isKanikoPipeline(pipelineName) {
		if pf := s.PipelinePreflight(ctx, pipelineName); !pf.BuildReady {
			resp.Message = pf.Reason
			return resp, empty, fmt.Errorf("%s", pf.Reason)
		}
	}

	runName := fmt.Sprintf("%s-%d", pipelineName, now.Unix())
	if att, ok := actions.CreateAttemptFrom(ctx); ok {
		runName = actions.ObjectName(pipelineName, att)
	}
	spec := map[string]any{
		"pipelineRef": map[string]any{
			"name": pipelineName,
		},
	}
	var legacy []map[string]any
	if pipelineTakesRevision(pipelineName) {
		legacy = pipelineRunParams(pipelineName, rev, tag)
	}
	if len(legacy) > 0 || len(extra) > 0 {
		merged, mergeErr := mergePipelineParams(declaredPipelineParams(pipe), legacy, extra)
		if mergeErr != nil {
			resp.Message = mergeErr.Error()
			return resp, empty, fmt.Errorf("%s", resp.Message)
		}
		if len(merged) > 0 {
			spec["params"] = merged
			for _, p := range merged {
				if p["name"] == "revision" {
					if v, ok := p["value"].(string); ok && v != "" {
						rev = v
					}
				}
			}
		}
	}
	if ws := pipelineRunWorkspaces(pipelineName); len(ws) > 0 {
		spec["workspaces"] = ws
	}
	if isKanikoPipeline(pipelineName) {
		spec["taskRunTemplate"] = amd64CITaskRunTemplate()
	}
	if err := applyDeclaredRunExtras(spec, pipe.GetAnnotations()); err != nil {
		resp.Message = err.Error()
		return resp, empty, fmt.Errorf("%s", resp.Message)
	}
	if pipelineName == "bifrost-deliver-stg" {
		spec["taskRunSpecs"] = []map[string]any{
			{"pipelineTaskName": "prepare", "serviceAccountName": "tekton-deliver"},
			{"pipelineTaskName": "rollout", "serviceAccountName": "tekton-deliver"},
			{"pipelineTaskName": "gitops-sync", "serviceAccountName": "tekton-deliver"},
		}
	}
	if pipelineName == "bifrost-deliver-prod" {
		spec["taskRunSpecs"] = []map[string]any{
			{"pipelineTaskName": "prepare", "serviceAccountName": "tekton-deliver"},
			{"pipelineTaskName": "rollout", "serviceAccountName": "tekton-deliver"},
			{"pipelineTaskName": "gitops-sync", "serviceAccountName": "tekton-deliver"},
		}
	}
	if pipelineName == "bifrost-deliver-platform" || pipelineName == "bifrost-deliver-platform-prod" {
		spec["taskRunSpecs"] = []map[string]any{
			{"pipelineTaskName": "rollout", "serviceAccountName": "tekton-deliver"},
			{"pipelineTaskName": "gitops-sync", "serviceAccountName": "tekton-deliver"},
		}
	}
	// Research (second payload). verify-research also needs the SA: it asserts the
	// running Deployment image matches the tag just built, which the default SA
	// cannot read in the research namespace.
	if pipelineName == "bifrost-deliver-research" {
		spec["taskRunSpecs"] = []map[string]any{
			{"pipelineTaskName": "rollout-research", "serviceAccountName": "tekton-deliver"},
			{"pipelineTaskName": "verify-research", "serviceAccountName": "tekton-deliver"},
			{"pipelineTaskName": "gitops-sync", "serviceAccountName": "tekton-deliver"},
		}
	}
	meta := map[string]any{
		"name":      runName,
		"namespace": ns,
		"labels": map[string]any{
			"tekton.dev/pipeline": pipelineName,
			"bifrost.io/trigger":  "platform-api",
			"bifrost.io/revision": sanitizeLabelValue(rev),
		},
	}
	if ann := actions.IdentityAnnotations(ctx); len(ann) > 0 {
		raw := make(map[string]any, len(ann))
		for k, v := range ann {
			raw[k] = v
		}
		meta["annotations"] = raw
	}
	obj, objErr := unstructuredFrom(map[string]any{
		"apiVersion": "tekton.dev/v1",
		"kind":       "PipelineRun",
		"metadata":   meta,
		"spec":       spec,
	})
	if objErr != nil {
		resp.Message = objErr.Error()
		return resp, empty, objErr
	}

	res := dyn.Resource(pipelineRunGVR).Namespace(ns)
	if existing, gerr := res.Get(ctx, runName, metav1.GetOptions{}); gerr == nil {
		if aerr := actions.Adopt(ctx, ns, runName, existing.GetAnnotations()); aerr != nil {
			resp.Message = aerr.Error()
			return resp, empty, aerr
		}
		view := pipelineRunFromUnstructured(*existing, pipelineName)
		resp.OK = true
		resp.Target = fmt.Sprintf("PipelineRun/%s/%s", ns, runName)
		resp.Message = fmt.Sprintf("PipelineRun %s adopted for pipeline %s", runName, pipelineName)
		return resp, view, nil
	} else if !apierrors.IsNotFound(gerr) {
		if uncertainCreate(gerr) {
			uerr := actions.Uncertain("create timed out; outcome needs checking")
			resp.Message = uerr.Error()
			return resp, empty, uerr
		}
		resp.Message = gerr.Error()
		return resp, empty, gerr
	}

	created, err := res.Create(ctx, obj, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, gerr := res.Get(ctx, runName, metav1.GetOptions{})
		if gerr != nil {
			uerr := actions.Uncertain(fmt.Sprintf("conflicting object %s/%s; outcome needs checking", ns, runName))
			resp.Message = uerr.Error()
			return resp, empty, uerr
		}
		if aerr := actions.Adopt(ctx, ns, runName, existing.GetAnnotations()); aerr != nil {
			resp.Message = aerr.Error()
			return resp, empty, aerr
		}
		view := pipelineRunFromUnstructured(*existing, pipelineName)
		resp.OK = true
		resp.Target = fmt.Sprintf("PipelineRun/%s/%s", ns, runName)
		resp.Message = fmt.Sprintf("PipelineRun %s adopted for pipeline %s", runName, pipelineName)
		return resp, view, nil
	}
	if uncertainCreate(err) {
		uerr := actions.Uncertain("create timed out; outcome needs checking")
		resp.Message = uerr.Error()
		return resp, empty, uerr
	}
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
	sort.Slice(pods.Items, func(i, j int) bool {
		return pods.Items[i].Name < pods.Items[j].Name
	})

	var b strings.Builder
	var lastLogAt *time.Time
	for _, pod := range pods.Items {
		for _, c := range pod.Spec.Containers {
			logOpts := &corev1.PodLogOptions{
				Container:  c.Name,
				TailLines:  int64Ptr(5000),
				Timestamps: true,
			}
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
			cleaned, containerLast := stripK8sLogTimestamps(data)
			lastLogAt = maxTimePtr(lastLogAt, containerLast)
			b.WriteString(cleaned)
			if cleaned != "" && !strings.HasSuffix(cleaned, "\n") {
				b.WriteByte('\n')
			}
		}
	}
	out.Logs = b.String()
	out.LastLogAt = lastLogAt
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

// The in-cluster registry service, as Tekton sees it from a build pod. Both
// research image lines live here; k8s manifests reference the same registry by
// its NodePort address instead.
const researchRegistryRepo = "registry.cicd.svc.cluster.local:5000/bifrost-research"

const marketDataRegistryRepo = "registry.cicd.svc.cluster.local:5000/bifrost-market-data"

// pipelineRunParams are the params a run of a revision-taking pipeline is started with.
// A pipeline that clones bifrost-ui takes the ui ref separately as uiRevision; a run
// started here clones it at the same ref as everything else, which RefPreflight has
// checked exists in every repo the pipeline clones.
func pipelineRunParams(pipelineName, rev, tag string) []map[string]any {
	params := []map[string]any{
		{"name": "revision", "value": rev},
	}
	switch pipelineName {
	case "bifrost-deliver-research":
		// research builds an explicitly tagged image; the tag must match what
		// k8s/api/deployment.yaml will point at once the image lands.
		if t := strings.TrimSpace(tag); t != "" {
			params = append(params, map[string]any{"name": "tag", "value": t})
		}
	case "bifrost-deliver-prod":
		// Compatibility for callers that still pass one revision: each clone
		// param receives that same ref. A caller params map overrides per repo.
		// mergePipelineParams drops any of these the live Pipeline does not declare.
		for _, name := range []string{"coreRevision", "workerRevision", "apiRevision", "frontendRevision", "infraRevision"} {
			params = append(params, map[string]any{"name": name, "value": rev})
		}
	case "bifrost-build-market-data":
		// Like the Dagster line, this pipeline names a full image rather
		// than a tag; the repository is the plugin's own.
		if t := strings.TrimSpace(tag); t != "" {
			params = append(params, map[string]any{"name": "image", "value": marketDataRegistryRepo + ":" + t})
		}
	case "bifrost-build-research-dagster":
		// This pipeline names a full image rather than a tag, and Dagster's
		// image shares a repository with the runtime one — only the suffix
		// tells them apart. Asking for "0.94.1" here would build a Dagster
		// image over the image research-api runs, so the suffix is enforced
		// rather than trusted.
		if img := researchDagsterImage(tag); img != "" {
			params = append(params, map[string]any{"name": "image", "value": img})
		}
	}
	if slices.Contains(pipelineMirrorRepos[pipelineName], "bifrost-ui") {
		params = append(params, map[string]any{"name": "uiRevision", "value": rev})
	}
	return params
}

// Pipelines whose first parameter is the Gitea revision to build.
func pipelineTakesRevision(name string) bool {
	switch name {
	case "bifrost-deliver-stg", "bifrost-deliver-prod",
		"bifrost-deliver-platform", "bifrost-deliver-platform-prod",
		"bifrost-deliver-research", "bifrost-build-research-dagster",
		"bifrost-build-market-data", "bifrost-build-flex-query",
		"bifrost-build-ib-gateway":
		return true
	}
	return false
}

// researchDagsterImage turns a caller's tag into the image the Dagster build
// pipeline expects. The `-dagster` suffix is appended when missing: the two
// research image lines live in one repository and are told apart by that suffix
// alone, so a bare semver would overwrite the runtime image with a Dagster
// build. An empty tag yields an empty image and the pipeline keeps its default.
func researchDagsterImage(tag string) string {
	t := strings.TrimSpace(tag)
	if t == "" {
		return ""
	}
	if !strings.HasSuffix(t, "-dagster") {
		t += "-dagster"
	}
	return researchRegistryRepo + ":" + t
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
	case "bifrost-deliver-stg", "bifrost-deliver-prod":
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
	case "bifrost-deliver-platform", "bifrost-deliver-platform-prod":
		return []map[string]any{
			map[string]any{
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
			},
		}
	case "bifrost-build-stg":
		return []map[string]any{
			{"name": "api-source", "emptyDir": map[string]any{}},
			{"name": "frontend-source", "emptyDir": map[string]any{}},
		}
	case "bifrost-deliver-research", "bifrost-build-frontend-stg", "bifrost-build-market-data":
		return []map[string]any{buildContextPVC}
	case "bifrost-build-ib-gateway", "bifrost-build-flex-query":
		// These pipelines name the workspace "source", matching flex-query's pipeline.
		source := map[string]any{}
		for k, v := range buildContextPVC {
			source[k] = v
		}
		source["name"] = "source"
		return []map[string]any{source}
	case "bifrost-build-research-dagster":
		// The Dagster image carries the full dbt project, so its context needs
		// more room than the runtime image — 8Gi is what the hand-run template
		// in k8s/cicd/tekton has always used.
		return []map[string]any{
			{
				"name": "build-context",
				"volumeClaimTemplate": map[string]any{
					"spec": map[string]any{
						"accessModes":      []any{"ReadWriteOnce"},
						"storageClassName": "local-path",
						"resources": map[string]any{
							"requests": map[string]any{"storage": "8Gi"},
						},
					},
				},
			},
		}
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
				url: gw + stgAPIGatewayPath(domain),
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

	out.Reachability, out.Detail = aggregateStgSmoke(out.Targets)
	return out
}

// stgAPIGatewayPath is the gateway path a release smoke asks for one domain. One catalog with
// the connectivity matrix (probe.TradeGatewayRoutes), so the smoke and the matrix cannot drift
// apart again (TD-55). Each probe goes through the process's own prefix (/api/monitor/ops/health,
// /api/account/health). A name that is not a route ID (including the alias names TD-55 B2 retired:
// trading, strategy, portfolio) is asked for as /api/<name>/health, which the gateway no longer
// routes; clusters.yaml names none (it uses the defaults).
func stgAPIGatewayPath(domain string) string {
	if r, ok := probe.TradeRouteFor(domain); ok {
		return r.GatewayPath()
	}
	return "/api/" + domain + "/health"
}

// smokeHTTPStatus maps an HTTP status code to smoke probe reachability + detail.
func smokeHTTPStatus(statusCode int) (probe.Reachability, string) {
	detail := fmt.Sprintf("HTTP %d", statusCode)
	switch {
	case statusCode == 200:
		return probe.ReachOK, detail
	case statusCode == 503:
		return probe.ReachDegraded, detail
	case statusCode >= 400:
		return probe.ReachFail, detail
	default:
		return probe.ReachUnknown, detail
	}
}

// aggregateStgSmoke derives overall STG smoke reachability from probe targets.
// API domains (id prefix "stg-api-") drive the primary rollup; frontend-only
// failures fall through to the first-target / partial heuristics.
func aggregateStgSmoke(targets []StgSmokeTargetView) (probe.Reachability, string) {
	apiOK := 0
	apiTotal := 0
	for _, t := range targets {
		if strings.HasPrefix(t.ID, "stg-api-") {
			apiTotal++
			if t.Reachability == probe.ReachOK || t.Reachability == probe.ReachDegraded {
				apiOK++
			}
		}
	}
	switch {
	case apiTotal > 0 && apiOK == apiTotal:
		return probe.ReachOK, fmt.Sprintf("stg %d/%d API domains reachable", apiOK, apiTotal)
	case apiOK > 0:
		return probe.ReachDegraded, fmt.Sprintf("stg %d/%d API domains reachable", apiOK, apiTotal)
	case len(targets) > 0 && targets[0].Reachability == probe.ReachFail:
		return probe.ReachFail, "stg smoke unreachable"
	default:
		return probe.ReachDegraded, "stg smoke partial"
	}
}

// aggregateFailCountSmoke rolls up targets by counting ReachFail (prod/dev smoke).
func aggregateFailCountSmoke(targets []StgSmokeTargetView, label string) (probe.Reachability, string) {
	fail := 0
	for _, t := range targets {
		if t.Reachability == probe.ReachFail {
			fail++
		}
	}
	if fail == 0 {
		return probe.ReachOK, fmt.Sprintf("%d %s smoke target(s) OK", len(targets), label)
	}
	return probe.ReachFail, fmt.Sprintf("%d failing %s smoke target(s)", fail, label)
}

func (s *Service) probeStgHTTP(ctx context.Context, id, url string) StgSmokeTargetView {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return StgSmokeTargetView{
			ID: id, URL: url, Reachability: probe.ReachFail,
			Detail: "request error: " + err.Error(),
		}
	}
	if s.entry != nil {
		s.entry.ApplyStgGatewayHost(req)
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
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	reach, detail := smokeHTTPStatus(resp.StatusCode)
	return StgSmokeTargetView{ID: id, URL: url, Reachability: reach, Detail: detail}
}

func (s *Service) probeDevHTTP(ctx context.Context, id, url string) StgSmokeTargetView {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return StgSmokeTargetView{
			ID: id, URL: url, Reachability: probe.ReachFail,
			Detail: "request error: " + err.Error(),
		}
	}
	if s.entry != nil {
		s.entry.ApplyDevGatewayHost(req)
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
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	reach, detail := smokeHTTPStatus(resp.StatusCode)
	return StgSmokeTargetView{ID: id, URL: url, Reachability: reach, Detail: detail}
}

func (s *Service) probeProdHTTP(ctx context.Context, id, url string) StgSmokeTargetView {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return StgSmokeTargetView{
			ID: id, URL: url, Reachability: probe.ReachFail,
			Detail: "request error: " + err.Error(),
		}
	}
	if s.entry != nil {
		s.entry.ApplyProdGatewayHost(req)
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
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	reach, detail := smokeHTTPStatus(resp.StatusCode)
	return StgSmokeTargetView{ID: id, URL: url, Reachability: reach, Detail: detail}
}

// ProdSmoke HTTP probes for bifrost-prod via Traefik @ VIP :80 (Host trader.bifrost.lan).
func (s *Service) ProdSmoke(ctx context.Context) StgSmokeResponse {
	now := time.Now().UTC()
	out := StgSmokeResponse{
		ClusterID:    s.clusterID(),
		Reachability: probe.ReachUnknown,
		Detail:       "prod smoke probes not configured",
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

	if fe := s.entry.ResolvedProdFrontendURL(); fe != "" {
		probes = append(probes, struct {
			id  string
			url string
		}{id: "prod-frontend", url: fe})
	}

	gw := strings.TrimRight(s.entry.ResolvedProdGatewayURL(), "/")
	if gw != "" {
		for _, domain := range s.entry.ResolvedStgAPIDomains() {
			probes = append(probes, struct {
				id  string
				url string
			}{
				id:  "prod-api-" + domain,
				url: gw + stgAPIGatewayPath(domain),
			})
		}
	} else if u := s.entry.ResolvedProdAPIMonitorURL(); u != "" {
		probes = append(probes, struct {
			id  string
			url string
		}{id: "prod-api-monitor", url: u})
	}

	for _, p := range probes {
		if p.url == "" {
			continue
		}
		out.Targets = append(out.Targets, s.probeProdHTTP(ctx, p.id, p.url))
	}
	if len(out.Targets) == 0 {
		out.Detail = "configure prod_smoke URLs in clusters.yaml or PLATFORM_PROD_* env"
		return out
	}

	out.Reachability, out.Detail = aggregateFailCountSmoke(out.Targets, "prod")
	return out
}

// DevSmoke HTTP probes for bifrost-dev via Traefik @ VIP :80 (Host dev.trader.bifrost.lan).
func (s *Service) DevSmoke(ctx context.Context) StgSmokeResponse {
	now := time.Now().UTC()
	out := StgSmokeResponse{
		ClusterID:    s.clusterID(),
		Reachability: probe.ReachUnknown,
		Detail:       "dev smoke probes not configured",
		Targets:      []StgSmokeTargetView{},
		GeneratedAt:  now,
	}
	if s.entry == nil {
		out.Detail = "no cluster configured"
		return out
	}

	gw := strings.TrimRight(s.entry.ResolvedDevGatewayURL(), "/")
	if gw == "" {
		out.Detail = "configure dev_smoke.gateway_url in clusters.yaml or PLATFORM_DEV_GATEWAY_URL"
		return out
	}
	for _, domain := range s.entry.ResolvedStgAPIDomains() {
		out.Targets = append(out.Targets, s.probeDevHTTP(ctx, "dev-api-"+domain, gw+stgAPIGatewayPath(domain)))
	}
	if len(out.Targets) == 0 {
		return out
	}
	fail := 0
	for _, t := range out.Targets {
		if t.Reachability == probe.ReachFail {
			fail++
		}
	}
	if fail == 0 {
		out.Reachability = probe.ReachOK
		out.Detail = fmt.Sprintf("%d dev API domain(s) OK @ %s", len(out.Targets), gw)
	} else {
		out.Reachability = probe.ReachFail
		out.Detail = fmt.Sprintf("%d failing dev API probe(s)", fail)
	}
	return out
}

func (s *Service) ClusterClient() (kubernetes.Interface, string, error) {
	return s.cluster.KubernetesClient()
}

// ProdDeliverArtifactsReady is true when the latest deliver-prod PipelineRun completed build tasks
// (rollout may have failed transiently while the cluster later recovered).
func (s *Service) ProdDeliverArtifactsReady(ctx context.Context) (bool, string) {
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return false, err.Error()
	}
	ns := s.PipelinesNamespace()
	runs, listErr := dyn.Resource(pipelineRunGVR).Namespace(ns).List(ctx, metav1.ListOptions{
		LabelSelector: "tekton.dev/pipeline=bifrost-deliver-prod",
	})
	if listErr != nil || len(runs.Items) == 0 {
		return false, "no deliver-prod PipelineRun"
	}
	sort.Slice(runs.Items, func(i, j int) bool {
		return runs.Items[i].GetCreationTimestamp().After(runs.Items[j].GetCreationTimestamp().Time)
	})
	prName := runs.Items[0].GetName()
	buildTasks := []string{"build-all-apis", "build-frontend", "build-worker-socket"}
	for _, task := range buildTasks {
		trName := prName + "-" + task
		tr, trErr := dyn.Resource(taskRunGVR).Namespace(ns).Get(ctx, trName, metav1.GetOptions{})
		if trErr != nil {
			return false, trName + " missing"
		}
		view := taskRunSummaryFrom(*tr)
		if !isTaskRunSucceededView(view) {
			return false, trName + " not succeeded"
		}
	}
	return true, prName + " build tasks succeeded"
}

func isTaskRunSucceededView(v SupplyChainTaskRunView) bool {
	st := strings.ToLower(v.Status)
	re := strings.ToLower(v.Reason)
	return st == "true" || st == "succeeded" || re == "succeeded" || re == "completed"
}

// unstructuredFrom JSON-round-trips obj so nested slices are []any.
// Unstructured.DeepCopy panics on []map[string]any, which the create path
// builds for params and taskRunSpecs.
func unstructuredFrom(obj map[string]any) (*unstructured.Unstructured, error) {
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	out := &unstructured.Unstructured{}
	if err := json.Unmarshal(raw, &out.Object); err != nil {
		return nil, err
	}
	return out, nil
}

// uncertainCreate is a timeout, server timeout, or cancelled context on the
// call that creates a PipelineRun. It does not prove the object is absent.
func uncertainCreate(err error) bool {
	if err == nil {
		return false
	}
	return apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
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
	if params, found, _ := unstructured.NestedSlice(obj.Object, "spec", "params"); found {
		for _, p := range params {
			if pm, ok := p.(map[string]any); ok {
				if name, _ := pm["name"].(string); name == "revision" {
					if val, _ := pm["value"].(string); val != "" {
						view.Revision = val
					}
				}
			}
		}
	}
	if view.Revision == "" {
		if labels := obj.GetLabels(); labels != nil {
			if rev, ok := labels["bifrost.io/revision"]; ok && rev != "" {
				view.Revision = rev
			}
		}
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

// SetKubernetesUnavailableForTest makes cluster reads fail closed so a unit
// test cannot reach a real apiserver.
func (s *Service) SetKubernetesUnavailableForTest() {
	cs := cluster.NewService(s.entry)
	cs.SetClientFactoryForTest(func() (kubernetes.Interface, string, error) {
		return nil, "", fmt.Errorf("no cluster in unit test")
	})
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

const (
	giteaInClusterBase = "http://gitea.cicd.svc.cluster.local:3000"
	giteaOrg           = "bifrost"
	giteaSecretName    = "gitea-bootstrap"
)

// giteaBaseURL returns the Gitea API base. In-cluster it resolves the internal
// service DNS; for local dev set GITEA_BASE (e.g. http://localhost:3000 paired
// with `kubectl -n cicd port-forward svc/gitea 3000:3000`).
func giteaBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("GITEA_BASE")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return giteaInClusterBase
}

func validateRevision(rev string) error {
	if len(rev) > 256 {
		return fmt.Errorf("revision too long (max 256 characters)")
	}
	if strings.ContainsAny(rev, "\n\r\t") {
		return fmt.Errorf("revision cannot contain whitespace")
	}
	if strings.HasPrefix(rev, "#") || strings.Contains(rev, "## ") {
		return fmt.Errorf("invalid revision — looks like pasted text, not a git ref")
	}
	return nil
}

func intersectRefSets(sets []map[string]bool) []string {
	if len(sets) == 0 {
		return nil
	}
	out := make([]string, 0)
	for name := range sets[0] {
		ok := true
		for i := 1; i < len(sets); i++ {
			if !sets[i][name] {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Service) Revisions(ctx context.Context, repos []string) RevisionsResponse {
	now := time.Now().UTC()
	ns := s.PipelinesNamespace()
	out := RevisionsResponse{
		ClusterID:    s.clusterID(),
		Repos:        repos,
		DefaultRef:   "main",
		Tags:         []GiteaTagView{},
		Branches:     []GiteaBranchView{},
		CommonRefs:   []string{},
		Reachability: probe.ReachFail,
		GeneratedAt:  now,
	}

	if len(repos) == 0 {
		repos = trackedGiteaRepos
		out.Repos = repos
	}

	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		out.Detail = "cluster client: " + err.Error()
		return out
	}

	user, pass := s.giteaCredentials(ctx, clientset, ns)

	seenTag := map[string]bool{}
	var allTags []GiteaTagView
	seenBranch := map[string]bool{}
	var allBranches []GiteaBranchView
	var errors []string
	branchSets := make([]map[string]bool, 0, len(repos))
	tagSets := make([]map[string]bool, 0, len(repos))

	for _, repo := range repos {
		tags, fetchErr := s.fetchGiteaTags(ctx, repo, user, pass)
		if fetchErr != nil {
			errors = append(errors, repo+" tags: "+fetchErr.Error())
		} else {
			tagSet := map[string]bool{}
			for _, t := range tags {
				tagSet[t.Name] = true
				key := t.Name + "/" + t.Repo
				if !seenTag[key] {
					seenTag[key] = true
					allTags = append(allTags, t)
				}
			}
			tagSets = append(tagSets, tagSet)
		}

		branches, fetchErr := s.fetchGiteaBranches(ctx, repo, user, pass)
		if fetchErr != nil {
			errors = append(errors, repo+" branches: "+fetchErr.Error())
		} else {
			branchSet := map[string]bool{}
			for _, b := range branches {
				branchSet[b.Name] = true
				key := b.Name + "/" + b.Repo
				if !seenBranch[key] {
					seenBranch[key] = true
					allBranches = append(allBranches, b)
				}
			}
			branchSets = append(branchSets, branchSet)
		}
	}

	out.Tags = allTags
	out.Branches = allBranches

	common := map[string]bool{}
	for _, name := range intersectRefSets(branchSets) {
		common[name] = true
	}
	for _, name := range intersectRefSets(tagSets) {
		common[name] = true
	}
	commonList := make([]string, 0, len(common))
	for name := range common {
		commonList = append(commonList, name)
	}
	sort.Strings(commonList)
	out.CommonRefs = commonList

	if len(errors) > 0 {
		out.Reachability = probe.ReachDegraded
		out.Detail = strings.Join(errors, "; ")
	} else {
		out.Reachability = probe.ReachOK
		out.Detail = fmt.Sprintf("%d branch(es), %d tag(s), %d common ref(s) across %d repo(s)",
			len(allBranches), len(allTags), len(commonList), len(repos))
	}
	return out
}

// giteaCredentials resolves basic-auth credentials for the Gitea REST API.
// The gitea-bootstrap secret stores an admin token (no username/password), so we
// fall back to using the token as the basic-auth username, which Gitea accepts.
// Without this, REST calls are unauthenticated and Gitea returns 404 for private
// repos — which previously left branch/tag listings and ref preflight empty.
func (s *Service) giteaCredentials(ctx context.Context, clientset kubernetes.Interface, ns string) (string, string) {
	secret, err := clientset.CoreV1().Secrets(ns).Get(ctx, giteaSecretName, metav1.GetOptions{})
	if err != nil {
		return "", ""
	}
	if user := string(secret.Data["username"]); user != "" {
		return user, string(secret.Data["password"])
	}
	if token := strings.TrimSpace(string(secret.Data["gitea_admin_token"])); token != "" {
		return token, "x-oauth-basic"
	}
	return "", ""
}

// GiteaAccess is what a read-only consumer of the Gitea REST API needs.
type GiteaAccess struct {
	Base string // e.g. http://gitea.cicd.svc.cluster.local:3000 (no /api/v1)
	Org  string
	User string
	Pass string
}

// GiteaAccess resolves the Gitea base URL, org and basic-auth credentials the
// delivery service itself uses, so other read-only views share one auth path.
func (s *Service) GiteaAccess(ctx context.Context) (GiteaAccess, error) {
	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		return GiteaAccess{}, fmt.Errorf("cluster client: %w", err)
	}
	user, pass := s.giteaCredentials(ctx, clientset, s.PipelinesNamespace())
	return GiteaAccess{Base: giteaBaseURL(), Org: giteaOrg, User: user, Pass: pass}, nil
}

func (s *Service) fetchGiteaTags(ctx context.Context, repo, user, pass string) ([]GiteaTagView, error) {
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/tags?limit=50", giteaBaseURL(), giteaOrg, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if user != "" && pass != "" {
		req.SetBasicAuth(user, pass)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, err
	}

	var raw []struct {
		Name   string `json:"name"`
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("json: %w", err)
	}

	tags := make([]GiteaTagView, 0, len(raw))
	for _, t := range raw {
		tags = append(tags, GiteaTagView{
			Name:   t.Name,
			Repo:   repo,
			Commit: t.Commit.SHA,
		})
	}
	return tags, nil
}

func (s *Service) fetchGiteaBranches(ctx context.Context, repo, user, pass string) ([]GiteaBranchView, error) {
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/branches?limit=50", giteaBaseURL(), giteaOrg, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if user != "" && pass != "" {
		req.SetBasicAuth(user, pass)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, err
	}

	var raw []struct {
		Name   string `json:"name"`
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("json: %w", err)
	}

	branches := make([]GiteaBranchView, 0, len(raw))
	for _, b := range raw {
		branches = append(branches, GiteaBranchView{
			Name:   b.Name,
			Repo:   repo,
			Commit: b.Commit.ID,
		})
	}
	return branches, nil
}

// Compare returns changed file paths between two refs via Gitea compare API (read-only).
func (s *Service) Compare(ctx context.Context, repo, from, to string) CompareResponse {
	now := time.Now().UTC()
	out := CompareResponse{
		ClusterID:    s.clusterID(),
		Repo:         repo,
		From:         from,
		To:           to,
		Files:        []string{},
		Reachability: probe.ReachFail,
		GeneratedAt:  now,
	}
	repo = strings.TrimSpace(repo)
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if repo == "" || from == "" || to == "" {
		out.Detail = "repo, from, and to query params required"
		return out
	}
	if from == to {
		out.Reachability = probe.ReachOK
		out.Detail = "identical refs — no files"
		return out
	}

	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		out.Detail = "cluster client: " + err.Error()
		return out
	}
	ns := s.PipelinesNamespace()
	user, pass := s.giteaCredentials(ctx, clientset, ns)

	files, fetchErr := s.fetchGiteaCompareFiles(ctx, repo, from, to, user, pass)
	if fetchErr != nil {
		out.Detail = fetchErr.Error()
		return out
	}
	out.Files = files
	out.Reachability = probe.ReachOK
	out.Detail = fmt.Sprintf("%d files", len(files))
	return out
}

func (s *Service) fetchGiteaCompareFiles(ctx context.Context, repo, from, to, user, pass string) ([]string, error) {
	// Gitea: GET /api/v1/repos/{owner}/{repo}/compare/{base}...{head}
	url := fmt.Sprintf(
		"%s/api/v1/repos/%s/%s/compare/%s...%s",
		giteaBaseURL(),
		giteaOrg,
		repo,
		from,
		to,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if user != "" && pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}
	var raw struct {
		Files []struct {
			Filename string `json:"filename"`
		} `json:"files"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("json: %w", err)
	}
	files := make([]string, 0, len(raw.Files))
	for _, f := range raw.Files {
		if name := strings.TrimSpace(f.Filename); name != "" {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	return files, nil
}
