package delivery

import (
	"context"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryreadings"
	"regexp"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// trackedGiteaRepos is the ref checks' fallback for a pipeline missing from
// pipelineMirrorRepos (reposForPipeline). The pages do not read it: the
// supply chain's tracked_repos are what the cluster's Pipelines clone
// (pipelineClones). It and pipelineMirrorRepos still hold the retired socket
// entry because the ref checks read them; changing what those checks ask is
// not a page change.
var trackedGiteaRepos = []string{
	"bifrost-trade-core",
	"bifrost-trade-worker",
	"bifrost-trade-socket",
	"bifrost-trade-api",
	"bifrost-trade-frontend",
	"bifrost-trade-infra",
	"bifrost-ui",
	"bifrost-platform",
}

// pipelineMirrorRepos maps a deliver pipeline to the Gitea repos it clones.
// Platform delivery checks effective clone refs; other delivery retains the
// legacy single-ref probe sets, including the retired socket entry.
var pipelineMirrorRepos = map[string][]string{
	"bifrost-ci-platform":           {"bifrost-platform"},
	"bifrost-ci-frontend":           {"bifrost-trade-frontend"},
	"bifrost-ci-infra":              {"bifrost-trade-infra"},
	"bifrost-build-ci-infra-tools":  {"bifrost-trade-infra"},
	"bifrost-deliver-platform":      {"bifrost-platform", "bifrost-ui"},
	"bifrost-deliver-platform-prod": {"bifrost-platform", "bifrost-ui"},
	"bifrost-deliver-stg": {
		"bifrost-trade-core",
		"bifrost-trade-worker",
		"bifrost-trade-socket",
		"bifrost-trade-api",
		"bifrost-trade-frontend",
		"bifrost-trade-infra",
		"bifrost-ui",
	},
	"bifrost-deliver-prod": {
		"bifrost-trade-core",
		"bifrost-trade-worker",
		"bifrost-trade-socket",
		"bifrost-trade-api",
		"bifrost-trade-frontend",
		"bifrost-trade-infra",
		"bifrost-ui",
	},
}

// pipelineRepoRefParams selects effective clone refs for platform delivery only.
var pipelineRepoRefParams = map[string]map[string]string{
	"bifrost-deliver-platform":      {"bifrost-platform": "revision", "bifrost-ui": "uiRevision", "bifrost-trade-infra": "infraRevision"},
	"bifrost-deliver-platform-prod": {"bifrost-platform": "revision", "bifrost-ui": "uiRevision"},
}

func isPlatformDelivery(name string) bool {
	return name == "bifrost-deliver-platform" || name == "bifrost-deliver-platform-prod"
}

// reposForPipeline returns the clone repos for a pipeline, falling back to all
// tracked repos when the pipeline is unknown.
func reposForPipeline(pipelineName string) []string {
	if repos, ok := pipelineMirrorRepos[pipelineName]; ok {
		return repos
	}
	return trackedGiteaRepos
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
		TrackedRepos:    []string{},
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
		out.TrackedReposError = "cluster not read: " + out.Detail
		return out
	}

	// The platform's own read-only Gitea credential (TD-432); the platform
	// reads no Secret for it.
	out.MirrorCredentialsConfigured = s.giteaReaderConfigured(ctx)

	// What the cluster's Pipelines and Tasks name, read now: the repositories
	// the Pipelines clone, and the Dockerfile ConfigMaps they and the Tasks
	// mount (OWNER_LAW #93, 2026-11-17: no hand-kept list).
	var unread []string
	dyn, dynErr := s.buildDynamicClient()
	if dynErr != nil {
		out.TrackedReposError = "Pipelines not read: " + dynErr.Error()
		unread = append(unread, "Dockerfile ConfigMaps: "+dynErr.Error())
	} else {
		cmNames := map[string]bool{}
		pipes, listErr := dyn.Resource(pipelineGVR).Namespace(out.CicdNamespace).List(ctx, metav1.ListOptions{})
		if listErr != nil {
			out.TrackedReposError = "Pipelines not read: " + listErr.Error()
			unread = append(unread, "Dockerfile ConfigMaps (Pipelines): "+listErr.Error())
		} else {
			repos := map[string]bool{}
			for _, item := range pipes.Items {
				cloned, _ := pipelineClones(item.Object)
				for _, r := range cloned {
					repos[r] = true
				}
				dockerfileConfigMapRefs(item.Object, cmNames)
			}
			out.TrackedRepos = sortedSet(repos)
		}
		tasks, taskErr := dyn.Resource(taskGVR).Namespace(out.CicdNamespace).List(ctx, metav1.ListOptions{})
		if taskErr != nil {
			unread = append(unread, "Dockerfile ConfigMaps (Tasks): "+taskErr.Error())
		} else {
			for _, item := range tasks.Items {
				dockerfileConfigMapRefs(item.Object, cmNames)
			}
		}
		for _, name := range sortedSet(cmNames) {
			cm, cmErr := clientset.CoreV1().ConfigMaps(out.CicdNamespace).Get(ctx, name, metav1.GetOptions{})
			if cmErr != nil {
				out.DockerfileCMs = append(out.DockerfileCMs, DockerfileConfigMapView{
					Name: name, Namespace: out.CicdNamespace, Present: false, Detail: cmErr.Error(),
				})
				continue
			}
			out.DockerfileCMs = append(out.DockerfileCMs, dockerfileCMFrom(cm))
		}
	}

	// Every Deployment in the STG namespace, not a hand-kept list of names.
	deps, depErr := clientset.AppsV1().Deployments(out.StgNamespace).List(ctx, metav1.ListOptions{})
	if depErr != nil {
		unread = append(unread, "STG workloads: "+depErr.Error())
	} else {
		sort.Slice(deps.Items, func(i, j int) bool { return deps.Items[i].Name < deps.Items[j].Name })
		for _, dep := range deps.Items {
			if len(dep.Spec.Template.Spec.Containers) == 0 {
				continue
			}
			out.StgWorkloads = append(out.StgWorkloads, StgWorkloadImageView{
				Deployment: dep.Name,
				Namespace:  out.StgNamespace,
				Image:      dep.Spec.Template.Spec.Containers[0].Image,
			})
		}
	}

	if last, err := readThrough(s, routeSupplyChain,
		func(c RunCache) deliverRuns { return deliverRunsOf(c.Objects(supplyChainPipeline)) },
		func() (deliverRuns, error) { return s.listDeliverRuns(ctx, out.CicdNamespace) },
		deliverRuns.keys); err == nil {
		out.LastDeliverRun, out.LastDeliverSuccess = last.run, last.success
	}

	if dynErr == nil {
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
		"%d Dockerfile CMs · %d STG workloads · %d tracked repos · mirror creds=%v",
		len(out.DockerfileCMs), len(out.StgWorkloads), len(out.TrackedRepos), out.MirrorCredentialsConfigured,
	)
	if len(unread) > 0 {
		out.Reachability = probe.ReachDegraded
		out.Detail += " · not read: " + strings.Join(unread, "; ")
	}
	return out
}

// gitRepoURL is a clone URL's last path segment: the repository it names.
var gitRepoURL = regexp.MustCompile(`([A-Za-z0-9._-]+)\.git$`)

// pipelineParamRef is a value that is only a pipeline parameter: $(params.x).
var pipelineParamRef = regexp.MustCompile(`^\$\(params\.([A-Za-z0-9_-]+)\)$`)

// pipelineClones reads the Gitea repositories a Tekton Pipeline object clones:
// every task parameter (tasks and finally) whose value, after a bare
// $(params.x) is replaced by that pipeline parameter's default, ends in
// "<repo>.git". revisionRepo is the one repository cloned at the pipeline's
// own $(params.revision), empty when none or several are (a run's revision
// then names no single repository).
func pipelineClones(obj map[string]any) (repos []string, revisionRepo string) {
	spec, _ := obj["spec"].(map[string]any)
	defaults := map[string]string{}
	for _, p := range anyList(spec["params"]) {
		pm, _ := p.(map[string]any)
		name, _ := pm["name"].(string)
		if def, ok := pm["default"].(string); ok && name != "" {
			defaults[name] = def
		}
	}
	seen := map[string]bool{}
	atRevision := map[string]bool{}
	for _, key := range []string{"tasks", "finally"} {
		for _, t := range anyList(spec[key]) {
			tm, _ := t.(map[string]any)
			var cloned []string
			takesRevision := false
			for _, p := range anyList(tm["params"]) {
				pm, _ := p.(map[string]any)
				name, _ := pm["name"].(string)
				value, _ := pm["value"].(string)
				if name == "revision" && strings.TrimSpace(value) == "$(params.revision)" {
					takesRevision = true
				}
				if m := pipelineParamRef.FindStringSubmatch(strings.TrimSpace(value)); m != nil {
					value = defaults[m[1]]
				}
				if m := gitRepoURL.FindStringSubmatch(strings.TrimSpace(value)); m != nil {
					cloned = append(cloned, m[1])
				}
			}
			for _, r := range cloned {
				seen[r] = true
				if takesRevision {
					atRevision[r] = true
				}
			}
		}
	}
	repos = sortedSet(seen)
	if len(atRevision) == 1 {
		revisionRepo = sortedSet(atRevision)[0]
	}
	return repos, revisionRepo
}

// dockerfileConfigMapRefs adds every ConfigMap volume an object mounts whose
// name ends in "-dockerfile".
func dockerfileConfigMapRefs(v any, into map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		if cm, ok := x["configMap"].(map[string]any); ok {
			if name, _ := cm["name"].(string); strings.HasSuffix(name, "-dockerfile") {
				into[name] = true
			}
		}
		for _, child := range x {
			dockerfileConfigMapRefs(child, into)
		}
	case []any:
		for _, child := range x {
			dockerfileConfigMapRefs(child, into)
		}
	}
}

func anyList(v any) []any {
	l, _ := v.([]any)
	return l
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// pipelineRepos lists the repositories every Pipeline in the pipelines
// namespace clones: the default set a revisions read covers.
func (s *Service) pipelineRepos(ctx context.Context) ([]string, error) {
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return nil, err
	}
	pipes, err := dyn.Resource(pipelineGVR).Namespace(s.PipelinesNamespace()).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	repos := map[string]bool{}
	for _, item := range pipes.Items {
		cloned, _ := pipelineClones(item.Object)
		for _, r := range cloned {
			repos[r] = true
		}
	}
	return sortedSet(repos), nil
}

// supplyChainPipeline is the pipeline whose runs the supply chain shows.
const supplyChainPipeline = "bifrost-deliver-stg"

// deliverRuns is the supply chain's newest deliver-stg run and newest
// succeeded one.
type deliverRuns struct{ run, success *PipelineRunView }

func (d deliverRuns) keys() []string {
	var out []string
	for i, v := range []*PipelineRunView{d.run, d.success} {
		which := []string{"last:", "success:"}[i]
		if v != nil {
			out = append(out, which+viewKey(*v))
		} else {
			out = append(out, which+"\x00none")
		}
	}
	return out
}

func deliverRunsOf(items []unstructured.Unstructured) deliverRuns {
	var d deliverRuns
	views := pipelineRunViews(items, supplyChainPipeline)
	if len(views) == 0 {
		return d
	}
	last := views[0]
	d.run = &last
	for _, v := range views {
		if isPipelineRunSucceededView(v) {
			succ := v
			d.success = &succ
			break
		}
	}
	return d
}

func (s *Service) listDeliverRuns(ctx context.Context, ns string) (deliverRuns, error) {
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return deliverRuns{}, err
	}
	runs, err := dyn.Resource(pipelineRunGVR).Namespace(ns).List(ctx, metav1.ListOptions{
		LabelSelector: "tekton.dev/pipeline=" + supplyChainPipeline,
	})
	if err != nil {
		return deliverRuns{}, err
	}
	return deliverRunsOf(runs.Items), nil
}

func dockerfileCMFrom(cm *corev1.ConfigMap) DockerfileConfigMapView {
	return deliveryreadings.DockerfileCMFrom(cm)
}

func isPipelineRunSucceededView(v PipelineRunView) bool {
	st := strings.ToLower(v.Status)
	re := strings.ToLower(v.Reason)
	return st == "true" || st == "succeeded" || re == "succeeded" || re == "completed"
}

func taskRunSummaryFrom(obj unstructured.Unstructured) SupplyChainTaskRunView {
	return deliveryreadings.TaskRunSummaryFrom(obj)
}

func (s *Service) TriggerMirrorSync(ctx context.Context) (cluster.ActuationResponse, SupplyChainTaskRunView, error) {
	return s.createSupplyChainTaskRun(ctx, "bifrost-gitea-mirror-sync", "mirror-sync", nil)
}

// refreshRevision is the shape of a branch or tag name the Dockerfile refresh
// accepts. Task bifrost-refresh-dockerfile-cms runs as tekton-deliver, which
// may write ConfigMaps in cicd, and pastes the revision into its shell script
// (git clone --branch "$(params.revision)"): a quote, $(…), ; or a leading -
// would be a command or an option there (TD-432 review 542 A3). Only letters,
// digits and . _ / - pass, starting with a letter or digit.
var refreshRevision = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)

// validRefreshRevision refuses a revision the refresh task could not take
// safely as a branch or tag name.
func validRefreshRevision(rev string) error {
	if !refreshRevision.MatchString(rev) || strings.Contains(rev, "..") || strings.HasSuffix(rev, "/") || strings.HasSuffix(rev, ".lock") {
		return fmt.Errorf("revision %q is not a branch or tag name (letters, digits and . _ / -)", rev)
	}
	return nil
}

func (s *Service) RefreshDockerfileConfigMaps(ctx context.Context, revision string) (cluster.ActuationResponse, SupplyChainTaskRunView, error) {
	rev := strings.TrimSpace(revision)
	if rev == "" {
		rev = "main"
	}
	if err := validRefreshRevision(rev); err != nil {
		return cluster.ActuationResponse{OK: false, Action: "delivery.supply-chain.dockerfile-configmaps",
			Message: err.Error(), GeneratedAt: time.Now().UTC()}, SupplyChainTaskRunView{}, err
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
