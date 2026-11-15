package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/postrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile/k8sstate"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// After a PROD release of the platform, the platform reads what now runs in
// its own namespace and records one verdict per run (internal/postrelease has
// the judgement and why; this file is its hands). Observe only: the verdict
// is an audit record (GET /api/v1/audit, action post_release_check) and a log
// line. Nothing is stopped, rolled back or told to anyone (design
// ci-speed/auto-prod-design.md 3.1, step A; ruling 2026-11-14).
//
// POST_RELEASE_CHECK is off (the default: nothing is read) or observe.
// POST_RELEASE_SETTLE_SECONDS is how long after a run completed its verdict
// is taken (default 180). POST_RELEASE_PIPELINE names another pipeline to
// follow, for trying the check where the PROD pipeline does not deliver (the
// STG platform sees the same runs and would judge its own namespace by them).
const (
	postReleaseEnv         = "POST_RELEASE_CHECK"
	postReleaseSettleEnv   = "POST_RELEASE_SETTLE_SECONDS"
	postReleasePipelineEnv = "POST_RELEASE_PIPELINE"
	postReleasePipeline    = "bifrost-deliver-platform-prod"
	postReleasePrincipal   = "post-release-check"
	postReleaseAction      = "post_release_check"
	postReleaseSettle      = 180 * time.Second
	// A run that settled longer ago than this is history: what runs now may
	// no longer be what it delivered, and a process that starts does not
	// judge the past.
	postReleaseMaxAge = 30 * time.Minute
	postReleasePass   = 60 * time.Second
	// The platform-api Service of base-platform/manifest.yaml (infra).
	postReleaseHealthService = "platform-api"
	postReleaseHealthPort    = 8780
)

// postReleaseDeployments are the workloads the pipeline delivers (infra
// k8s/base-platform/manifest.yaml).
var postReleaseDeployments = []string{"platform-api", "platform-workers", "platform-console"}

func postReleaseOn() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(postReleaseEnv)), "observe")
}

// postReleaseSettleOf reads POST_RELEASE_SETTLE_SECONDS; what is not a
// number of seconds from 0 to an hour leaves the default.
func postReleaseSettleOf(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return postReleaseSettle
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 || n > 3600 {
		slog.Error("post release check: "+postReleaseSettleEnv+" is not a number of seconds from 0 to 3600; the default holds", "value", value)
		return postReleaseSettle
	}
	return time.Duration(n) * time.Second
}

// postRun is one run of the pipeline as the cluster keeps it.
type postRun struct {
	postrelease.Run
	Status string // running | succeeded | failed
	// Params are the run's parameters, for the policy trial.
	Params map[string]string
}

// postRuns reads the pipeline's runs, newest first.
func postRuns(items []unstructured.Unstructured, pipeline string) []postRun {
	var picked []unstructured.Unstructured
	for _, item := range items {
		if ref, _, _ := unstructured.NestedString(item.Object, "spec", "pipelineRef", "name"); ref == pipeline {
			picked = append(picked, item)
		}
	}
	sort.SliceStable(picked, func(i, j int) bool {
		a, b := picked[i].GetCreationTimestamp().Time, picked[j].GetCreationTimestamp().Time
		if !a.Equal(b) {
			return a.After(b)
		}
		return picked[i].GetName() > picked[j].GetName()
	})
	out := make([]postRun, 0, len(picked))
	for _, item := range picked {
		r := postRun{Status: "running", Params: map[string]string{}}
		r.Name = item.GetName()
		params, _, _ := unstructured.NestedSlice(item.Object, "spec", "params")
		for _, value := range params {
			p, _ := value.(map[string]any)
			name, _ := p["name"].(string)
			if v, ok := p["value"].(string); ok && name != "" {
				r.Params[name] = v
			}
		}
		r.Commit = r.Params["revision"]
		// A run the cluster has no verdict on yet is running.
		if f, ok := pipelineRunFact(item); ok {
			r.Status = strings.ToLower(f.Result)
			r.Started, r.Completed = f.StartedAt, f.CompletedAt
		}
		if r.Started.IsZero() {
			r.Started = item.GetCreationTimestamp().Time
		}
		out = append(out, r)
	}
	return out
}

// postReleaseDue picks the run to judge now, or says why there is none.
// Only the newest run of the pipeline is ever judged, and only when it
// succeeded: with a later run under way or failed, what runs is no longer
// that release's alone, and a verdict on it would be charged to the wrong
// run. runs are newest first; judged says whether a run already has its
// verdict.
func postReleaseDue(runs []postRun, now time.Time, settle, maxAge time.Duration, judged func(run string) bool) (*postRun, string) {
	if len(runs) == 0 {
		return nil, "the pipeline has no runs"
	}
	r := runs[0]
	switch {
	case r.Status != "succeeded":
		return nil, "the newest run, " + r.Name + ", is " + r.Status
	case r.Completed.IsZero():
		return nil, r.Name + " has no completion time"
	case now.Sub(r.Completed) < settle:
		return nil, fmt.Sprintf("%s settles until %s", r.Name, r.Completed.Add(settle).UTC().Format(time.RFC3339))
	case now.Sub(r.Completed) > settle+maxAge:
		return nil, r.Name + " completed too long ago to judge by what runs now"
	case judged(r.Name):
		return nil, r.Name + " has its verdict"
	}
	return &r, ""
}

// postWorld is postrelease.World on this process's cluster client, the
// platform-api Service of its namespace and its release policy route.
type postWorld struct {
	s         *Server
	namespace string
	healthURL string
	pipeline  string
	params    map[string]string
}

// Pods lists the pods each Deployment's own selector selects. Read-only: the
// identity's observer role reads Deployments and pods cluster-wide (infra
// k8s/platform-rbac/00-clusterroles.yaml, bifrost-platform-observer).
func (w *postWorld) Pods(ctx context.Context, deployments []string) ([]postrelease.Pod, error) {
	if w.s.cluster == nil {
		return nil, fmt.Errorf("%w: no cluster client", postrelease.ErrNoReading)
	}
	core, _, err := w.s.cluster.Service().KubernetesClient()
	if err != nil {
		return nil, err
	}
	var out []postrelease.Pod
	for _, name := range deployments {
		d, err := core.AppsV1().Deployments(w.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("deployment %s: %w", name, err)
		}
		selector, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
		if err != nil || selector.Empty() {
			return nil, fmt.Errorf("deployment %s: no usable selector", name)
		}
		pods, err := core.CoreV1().Pods(w.namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			return nil, fmt.Errorf("pods of %s: %w", name, err)
		}
		for _, p := range pods.Items {
			if pod, ok := postPodOf(name, p); ok {
				out = append(out, pod)
			}
		}
	}
	return out, nil
}

// postPodOf reads one pod. A pod being deleted is not one of the
// Deployment's any more and is left out. Init containers are not read: they
// exit by design.
func postPodOf(deployment string, p corev1.Pod) (postrelease.Pod, bool) {
	if p.DeletionTimestamp != nil {
		return postrelease.Pod{}, false
	}
	out := postrelease.Pod{Deployment: deployment, Name: p.Name, Phase: string(p.Status.Phase)}
	for _, cs := range p.Status.ContainerStatuses {
		c := postrelease.Container{Name: cs.Name, Ready: cs.Ready, Restarts: int(cs.RestartCount)}
		for _, t := range []*corev1.ContainerStateTerminated{cs.LastTerminationState.Terminated, cs.State.Terminated} {
			if t != nil && t.FinishedAt.After(c.Terminated) {
				c.Terminated = t.FinishedAt.Time
				c.Exit = strings.TrimSpace(fmt.Sprintf("exit %d %s", t.ExitCode, t.Reason))
			}
		}
		out.Containers = append(out.Containers, c)
	}
	return out, true
}

// Health is one GET of the platform-api Service's /health.
func (w *postWorld) Health(ctx context.Context) (int, []byte, error) {
	if w.healthURL == "" {
		return 0, nil, fmt.Errorf("%w: this process knows no namespace of its own", postrelease.ErrNoReading)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.healthURL, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", postrelease.ErrNoReading, err)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, body, nil
}

// postTrialParams are the revisions of the run a caller could have asked
// for: revision itself goes beside them, and what the platform set (source,
// registry, image) is not a caller's to send.
func postTrialParams(params map[string]string) map[string]string {
	out := map[string]string{}
	for name, value := range params {
		if name != "revision" && strings.HasSuffix(name, "Revision") && commitID.MatchString(value) {
			out[name] = value
		}
	}
	return out
}

// PolicyTrial asks the release policy about the run's commit through the
// handler of GET /delivery/pipelines/{name}/policy-check itself, in this
// process: the same parameters' path and the same decision calls as
// check_release_policy, which start nothing and record nothing.
func (w *postWorld) PolicyTrial(ctx context.Context, run postrelease.Run) (postrelease.Trial, error) {
	if w.s.releasePolicy == nil {
		return postrelease.Trial{}, fmt.Errorf("%w: this process has no release policy", postrelease.ErrNoReading)
	}
	q := url.Values{"who": {postReleasePrincipal}}
	if run.Commit != "" {
		q.Set("revision", run.Commit)
	}
	if extra := postTrialParams(w.params); len(extra) > 0 {
		raw, err := json.Marshal(extra)
		if err != nil {
			return postrelease.Trial{}, err
		}
		q.Set("params", string(raw))
	}
	route := chi.NewRouteContext()
	route.URLParams.Add("name", w.pipeline)
	req, err := http.NewRequestWithContext(context.WithValue(ctx, chi.RouteCtxKey, route), http.MethodGet,
		"/api/v1/delivery/pipelines/"+url.PathEscape(w.pipeline)+"/policy-check?"+q.Encode(), nil)
	if err != nil {
		return postrelease.Trial{}, err
	}
	answer := &postAnswer{header: http.Header{}}
	w.s.handleReleasePolicyCheck(answer, req)
	if answer.status != http.StatusOK {
		var said struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(answer.body.Bytes(), &said)
		return postrelease.Trial{}, fmt.Errorf("HTTP %d: %s", answer.status, said.Error)
	}
	var check policyCheck
	if err := json.Unmarshal(answer.body.Bytes(), &check); err != nil {
		return postrelease.Trial{}, fmt.Errorf("the answer does not parse: %w", err)
	}
	return postrelease.Trial{PolicyID: check.PolicyID, Covered: check.Covered, Reasons: check.Reasons}, nil
}

// postAnswer takes a handler's answer in memory.
type postAnswer struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (a *postAnswer) Header() http.Header { return a.header }
func (a *postAnswer) WriteHeader(status int) {
	if a.status == 0 {
		a.status = status
	}
}
func (a *postAnswer) Write(p []byte) (int, error) {
	if a.status == 0 {
		a.status = http.StatusOK
	}
	return a.body.Write(p)
}

// postReleaseSource is the loop's pass, or nil when the switch is off.
func (s *Server) postReleaseSource() func(context.Context) error {
	if s.delivery == nil || !postReleaseOn() {
		return nil
	}
	pipeline := strings.TrimSpace(os.Getenv(postReleasePipelineEnv))
	if pipeline == "" {
		pipeline = postReleasePipeline
	}
	settle := postReleaseSettleOf(os.Getenv(postReleaseSettleEnv))
	namespace := k8sstate.Namespace()
	if namespace == "" {
		slog.Error("post release check: this process knows no namespace of its own; nothing is judged")
		return nil
	}
	healthURL := fmt.Sprintf("http://%s.%s.svc:%d/health", postReleaseHealthService, namespace, postReleaseHealthPort)
	slog.Info("post release check: observing", "pipeline", pipeline, "namespace", namespace, "settle", settle.String(), "health", healthURL)
	var mu sync.Mutex
	done := map[string]bool{} // the runs this process has recorded a verdict for
	lastWhy := ""
	return func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, postReleasePass)
		defer cancel()
		// Only this pipeline's runs are read (delivery.PipelineRunsOf has why).
		items, err := s.delivery.PipelineRunsOf(ctx, pipeline)
		if err != nil {
			return err
		}
		// The audit log outlives the process (a state ConfigMap in the
		// cluster): a run recorded before a restart is not judged again.
		judged := func(run string) bool {
			mu.Lock()
			defer mu.Unlock()
			return done[run] || s.audit.Has(postReleaseAction, "run/"+run)
		}
		run, why := postReleaseDue(postRuns(items, pipeline), time.Now(), settle, postReleaseMaxAge, judged)
		if run == nil {
			mu.Lock()
			changed := lastWhy != why
			lastWhy = why
			mu.Unlock()
			if changed {
				slog.Info("post release check: nothing to judge", "why", why)
			}
			return nil
		}
		w := &postWorld{s: s, namespace: namespace, healthURL: healthURL, pipeline: pipeline, params: run.Params}
		v := postrelease.Judge(ctx, w, run.Run, postReleaseDeployments)
		mu.Lock()
		done[run.Name] = true
		lastWhy = ""
		mu.Unlock()
		s.recordPostRelease(v)
		return nil
	}
}

// recordPostRelease writes the verdict where the loop's release decisions go
// (recordHeadDecision): the platform's audit log, and the process log.
func (s *Server) recordPostRelease(v postrelease.Verdict) {
	detail, err := json.Marshal(v)
	if err != nil {
		detail = []byte(v.Verdict)
	}
	slog.Info("post release check", "run", v.Run.Name, "commit", v.Run.Commit, "verdict", v.Verdict,
		"failed", strings.Join(v.Failed, ","), "unknown", strings.Join(v.Unknown, ","), "record", string(detail))
	s.audit.RecordDirect(postReleasePrincipal, actuation.RoleOperator, postReleaseAction, "run/"+v.Run.Name, v.Verdict, string(detail))
}
