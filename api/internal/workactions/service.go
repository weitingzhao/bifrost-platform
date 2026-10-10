// Package workactions runs the generic write actions from LANE-W33B.
// Namespaces, repositories, and images come from actuationpolicy, not from this package.
package workactions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
)

var pipelineRunGVR = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}

// Clients reaches the cluster. Tests substitute both functions.
type Clients struct {
	Kube    func() (kubernetes.Interface, error)
	Dynamic func() (dynamic.Interface, error)
}

// Service is the executor behind the five actions.
type Service struct {
	Policy  *actuationpolicy.Policy
	Clients Clients
	// Sync fetches the repo mirror and waits for commit before a plan run.
	// Production wires it to the delivery mirror API. Nil refuses to plan.
	Sync func(ctx context.Context, repo, commit string) error
	// Now is injectable in tests.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// Plan starts a plan-mode pipeline run and returns its name.
func (s *Service) Plan(ctx context.Context, repo, path, commit string) (string, error) {
	if err := s.Policy.RepoAllowed(repo, path); err != nil {
		return "", err
	}
	if !fullSHA(commit) {
		return "", fmt.Errorf("commit must be a 40-character hex sha")
	}
	if s.Sync == nil {
		return "", fmt.Errorf("mirror sync is not configured")
	}
	if err := s.Sync(ctx, repo, commit); err != nil {
		return "", err
	}
	name := fmt.Sprintf("plan-%s-%d", commit[:8], s.now().Unix())
	if err := s.startRun(ctx, name, "plan", repo, path, commit, false, ""); err != nil {
		return "", err
	}
	return name, nil
}

// Apply starts apply-mode for a successful plan. It does not prune.
func (s *Service) Apply(ctx context.Context, planID string) (map[string]any, error) {
	summary, err := s.Summarize(ctx, planID)
	if err != nil {
		return nil, err
	}
	if !summary.Ready || summary.Policy != "pass" {
		return nil, fmt.Errorf("plan is not a successful policy pass")
	}
	tier, err := s.Policy.ManifestTier(summary.Namespaces, summary.Daemon)
	if err != nil {
		return nil, err
	}
	if tier == "X" {
		return nil, fmt.Errorf("plan renders a daemon deployment and cannot be applied")
	}
	for _, obj := range summary.Objects {
		if err := s.Policy.KindAllowed(obj.Namespace, obj.Kind); err != nil {
			return nil, err
		}
	}
	name := applyRunName(planID, s.now())
	if err := s.startRun(ctx, name, "apply", summary.Repo, summary.Path, summary.Commit, tier == "C" || tier == "D", planID); err != nil {
		return nil, err
	}
	return map[string]any{
		"apply_run": name,
		"plan_id":   planID,
		"tier":      tier,
		"objects":   len(summary.Objects),
	}, nil
}

// Object is one rendered resource.
type Object struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Tier      string `json:"tier"`
}

// Summary is what an approval card shows for a plan.
type Summary struct {
	Ready      bool
	Policy     string
	Namespaces []string
	Daemon     bool
	Objects    []Object
	Repo       string
	Path       string
	Commit     string
	Logs       string
}

// Summarize reads a plan PipelineRun. Ready is false until it succeeds.
func (s *Service) Summarize(ctx context.Context, planID string) (Summary, error) {
	var out Summary
	dyn, err := s.Clients.Dynamic()
	if err != nil {
		return out, err
	}
	ns := s.Policy.Delivery.Namespace
	obj, err := dyn.Resource(pipelineRunGVR).Namespace(ns).Get(ctx, planID, metav1.GetOptions{})
	if err != nil {
		return out, err
	}
	var mode string
	out.Repo, out.Path, out.Commit, mode = paramsOf(obj)
	out.Logs = fmt.Sprintf("/api/v1/delivery/runs/%s/logs", planID)
	succeeded := condition(obj, "Succeeded")
	out.Policy, _ = resultOf(obj, "policy")
	raw, _ := resultOf(obj, "objects")
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &out.Objects)
	}
	seen := map[string]bool{}
	for _, o := range out.Objects {
		if !seen[o.Namespace] {
			seen[o.Namespace] = true
			out.Namespaces = append(out.Namespaces, o.Namespace)
		}
		if o.Kind == "Deployment" && o.Name == s.Policy.Apply.DaemonDeployment {
			out.Daemon = true
		}
	}
	out.Ready = succeeded && out.Policy == "pass" && planRunOK(obj, s.Policy.Delivery.Pipeline, mode)
	return out, nil
}

// planRunOK is the identity of a plan this service started.
// Anything else is not ready, even when a result says pass.
func planRunOK(obj *unstructured.Unstructured, pipeline, mode string) bool {
	labels := obj.GetLabels()
	if labels["bifrost.io/trigger"] != "platform-api" || labels["bifrost.io/mode"] != "plan" {
		return false
	}
	if mode != "plan" {
		return false
	}
	name, found, _ := unstructured.NestedString(obj.Object, "spec", "pipelineRef", "name")
	if !found || name != pipeline || name == "" {
		return false
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "spec", "pipelineRef", "resolver"); found {
		return false
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "spec", "pipelineRef", "params"); found {
		return false
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "spec", "pipelineRef", "bundle"); found {
		return false
	}
	return true
}

// applyRunName is unique per attempt, so a plan whose apply failed can be
// applied again under a new approval (TD-276). The plan id is also a label.
func applyRunName(planID string, now time.Time) string {
	suffix := fmt.Sprintf("-%d", now.Unix())
	base := trimName("apply-" + planID)
	if len(base)+len(suffix) > 63 {
		base = strings.Trim(base[:63-len(suffix)], "-")
	}
	return base + suffix
}

func (s *Service) startRun(ctx context.Context, name, mode, repo, path, commit string, requireMain bool, planID string) error {
	dyn, err := s.Clients.Dynamic()
	if err != nil {
		return err
	}
	require := "false"
	if requireMain {
		require = "true"
	}
	d := s.Policy.Delivery
	labels := map[string]any{
		"tekton.dev/pipeline": d.Pipeline,
		"bifrost.io/trigger":  "platform-api",
		"bifrost.io/mode":     mode,
	}
	if planID != "" {
		labels["bifrost.io/plan"] = trimName(planID)
	}
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tekton.dev/v1",
		"kind":       "PipelineRun",
		"metadata": map[string]any{
			"name":      name,
			"namespace": d.Namespace,
			"labels":    labels,
		},
		"spec": map[string]any{
			"pipelineRef": map[string]any{"name": d.Pipeline},
			"taskRunTemplate": map[string]any{
				"serviceAccountName": d.ServiceAccount,
			},
			"params": []any{
				param("mode", mode),
				param("repo", repo),
				param("path", path),
				param("commit", commit),
				param("giteaBase", d.GiteaBase),
				param("giteaOrg", d.GiteaOrg),
				param("requireOnMain", require),
			},
		},
	}}
	_, err = dyn.Resource(pipelineRunGVR).Namespace(d.Namespace).Create(ctx, obj, metav1.CreateOptions{})
	return err
}

func param(name, value string) map[string]any {
	return map[string]any{"name": name, "value": value}
}

// CreateJobFromCronJob instantiates one Job the way kubectl create job --from does.
func (s *Service) CreateJobFromCronJob(ctx context.Context, namespace, cronjob, requester string) (map[string]any, error) {
	if _, err := s.Policy.JobTier(namespace); err != nil {
		return nil, err
	}
	cs, err := s.Clients.Kube()
	if err != nil {
		return nil, err
	}
	cj, err := cs.BatchV1().CronJobs(namespace).Get(ctx, cronjob, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	name := trimName(fmt.Sprintf("%s-manual-%s", cronjob, s.now().UTC().Format("20060102150405")))
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"cronjob.kubernetes.io/instantiate": "manual",
			},
			Labels: map[string]string{
				"bifrost.io/requested-by": sanitizeLabel(requester),
			},
		},
		Spec: cj.Spec.JobTemplate.Spec,
	}
	created, err := cs.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return map[string]any{"namespace": created.Namespace, "job": created.Name}, nil
}

// DeleteFinished deletes Complete or Failed Jobs, and probe Pods this lane left behind.
func (s *Service) DeleteFinished(ctx context.Context, namespace string, names []string, selector string) (map[string]any, error) {
	if err := s.Policy.CleanupAllowed(namespace); err != nil {
		return nil, err
	}
	selector = strings.TrimSpace(selector)
	if (len(names) == 0) == (selector == "") {
		return nil, fmt.Errorf("provide exactly one of names or label_selector")
	}
	cs, err := s.Clients.Kube()
	if err != nil {
		return nil, err
	}
	var jobs []batchv1.Job
	if len(names) > 0 {
		for _, name := range names {
			job, err := cs.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return nil, err
			}
			jobs = append(jobs, *job)
		}
	} else {
		list, err := cs.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return nil, err
		}
		jobs = list.Items
	}
	deleted := []string{}
	policy := metav1.DeletePropagationBackground
	for _, job := range jobs {
		if !finished(job) {
			continue
		}
		if err := cs.BatchV1().Jobs(namespace).Delete(ctx, job.Name, metav1.DeleteOptions{PropagationPolicy: &policy}); err != nil && !apierrors.IsNotFound(err) {
			return nil, err
		}
		deleted = append(deleted, job.Name)
	}
	pods, err := cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "bifrost.io/probe=true"})
	if err != nil {
		return nil, err
	}
	probePods := []string{}
	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
			continue
		}
		if err := cs.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return nil, err
		}
		probePods = append(probePods, pod.Name)
	}
	return map[string]any{"jobs": deleted, "probe_pods": probePods}, nil
}

// Probe starts a one-shot Job. envFromDeploy copies that Deployment's first container env.
func (s *Service) Probe(ctx context.Context, namespace, image string, command, args []string, envFromDeploy string, timeout int, requester string) (map[string]any, error) {
	envFrom := strings.TrimSpace(envFromDeploy) != ""
	if _, err := s.Policy.ProbeTier(namespace, image, envFrom); err != nil {
		return nil, err
	}
	seconds, err := s.Policy.Timeout(timeout)
	if err != nil {
		return nil, err
	}
	if len(command) == 0 {
		return nil, fmt.Errorf("command is required")
	}
	cs, err := s.Clients.Kube()
	if err != nil {
		return nil, err
	}
	var env []corev1.EnvVar
	var envFromSrc []corev1.EnvFromSource
	if envFrom {
		dep, err := cs.AppsV1().Deployments(namespace).Get(ctx, envFromDeploy, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if len(dep.Spec.Template.Spec.Containers) == 0 {
			return nil, fmt.Errorf("deployment has no containers")
		}
		src := dep.Spec.Template.Spec.Containers[0]
		env = append([]corev1.EnvVar{}, src.Env...)
		envFromSrc = append([]corev1.EnvFromSource{}, src.EnvFrom...)
	}
	name := trimName("probe-" + s.now().UTC().Format("20060102150405"))
	deadline := int64(seconds)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"bifrost.io/probe":        "true",
				"bifrost.io/requested-by": sanitizeLabel(requester),
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To(int32(0)),
			TTLSecondsAfterFinished: ptr.To(int32(3600)),
			ActiveDeadlineSeconds:   &deadline,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"bifrost.io/probe": "true"}},
				Spec: corev1.PodSpec{
					RestartPolicy:                corev1.RestartPolicyNever,
					AutomountServiceAccountToken: ptr.To(false),
					ServiceAccountName:           "default",
					// Same race wait-pg covers: a new pod IP is not in the
					// NetworkPolicy for a few seconds, and the first dial is refused.
					InitContainers: []corev1.Container{{
						Name:    "wait-net",
						Image:   image,
						Command: []string{"sh", "-c", "sleep 8"},
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("64Mi"),
							},
						},
					}},
					Containers: []corev1.Container{{
						Name:    "probe",
						Image:   image,
						Command: command,
						Args:    args,
						Env:     env,
						EnvFrom: envFromSrc,
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("500m"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
						},
					}},
				},
			},
		},
	}
	created, err := cs.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return map[string]any{"namespace": created.Namespace, "job": created.Name, "logs": fmt.Sprintf("/api/v1/cluster/workloads/pods/%s/%s/logs", namespace, created.Name)}, nil
}

func finished(job batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func fullSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}

func trimName(name string) string {
	name = sanitizeLabel(name)
	if len(name) > 63 {
		name = strings.Trim(name[:63], "-")
	}
	if name == "" {
		return "job"
	}
	return name
}

func sanitizeLabel(v string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(v) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), "-._")
	if s == "" {
		return "unknown"
	}
	if len(s) > 63 {
		s = strings.Trim(s[:63], "-._")
	}
	return s
}

func paramsOf(obj *unstructured.Unstructured) (repo, path, commit, mode string) {
	raw, _, _ := unstructured.NestedSlice(obj.Object, "spec", "params")
	for _, item := range raw {
		m, _ := item.(map[string]any)
		name, _ := m["name"].(string)
		value, _ := m["value"].(string)
		switch name {
		case "repo":
			repo = value
		case "path":
			path = value
		case "commit":
			commit = value
		case "mode":
			mode = value
		}
	}
	return repo, path, commit, mode
}

func resultOf(obj *unstructured.Unstructured, name string) (string, bool) {
	raw, _, _ := unstructured.NestedSlice(obj.Object, "status", "results")
	for _, item := range raw {
		m, _ := item.(map[string]any)
		if n, _ := m["name"].(string); n == name {
			v, _ := m["value"].(string)
			return v, true
		}
	}
	return "", false
}

func condition(obj *unstructured.Unstructured, want string) bool {
	raw, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, item := range raw {
		m, _ := item.(map[string]any)
		t, _ := m["type"].(string)
		st, _ := m["status"].(string)
		if t == want && st == "True" {
			return true
		}
	}
	return false
}

// CommandSHA256 is the hash owner-run.sh compares.
func CommandSHA256(command string) string {
	sum := sha256.Sum256([]byte(command))
	return hex.EncodeToString(sum[:])
}
