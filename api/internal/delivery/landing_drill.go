package delivery

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// LandingCI reads all exact-revision platform CI runs. Unlike CIIndex, it
// never prefers an old success over a newer failure, unknown or running run.
func (h *Handler) LandingCI(ctx context.Context, revision string, after time.Time) (landing.CIRun, error) {
	return h.svc.landingCI(ctx, revision, after)
}
func (s *Service) landingCI(ctx context.Context, revision string, after time.Time) (landing.CIRun, error) {
	if !isFullGitSHA(revision) {
		return landing.CIRun{}, fmt.Errorf("invalid landing revision")
	}
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return landing.CIRun{}, err
	}
	var runs []landing.CIRun
	continuation := ""
	for {
		list, err := dyn.Resource(pipelineRunGVR).Namespace(s.PipelinesNamespace()).List(ctx, metav1.ListOptions{Limit: 500, Continue: continuation})
		if err != nil {
			return landing.CIRun{}, err
		}
		for _, item := range list.Items {
			pipeline, _, _ := unstructured.NestedString(item.Object, "spec", "pipelineRef", "name")
			if pipeline != "bifrost-ci-platform" {
				continue
			}
			if landingRevision(item) != revision {
				continue
			}
			status := "Unknown"
			conds, _, _ := unstructured.NestedSlice(item.Object, "status", "conditions")
			for _, cond := range conds {
				m, _ := cond.(map[string]any)
				if m["type"] != "Succeeded" {
					continue
				}
				switch m["status"] {
				case "True":
					status = "Succeeded"
				case "False":
					status = "Failed"
				case "Unknown":
					if m["reason"] == "Running" || m["reason"] == "Started" || m["reason"] == "Pending" {
						status = "Running"
					}
				}
			}
			runs = append(runs, landing.CIRun{Run: item.GetName(), R: revision, Status: status, CreatedAt: item.GetCreationTimestamp().Time})
		}
		continuation = list.GetContinue()
		if continuation == "" {
			break
		}
	}
	return landing.LatestCI(runs, revision, after), nil
}
func landingRevision(item unstructured.Unstructured) string {
	params, _, _ := unstructured.NestedSlice(item.Object, "spec", "params")
	for _, p := range params {
		m, _ := p.(map[string]any)
		if m["name"] == "revision" {
			v, _ := m["value"].(string)
			return v
		}
	}
	return ""
}

var landingCIName = regexp.MustCompile(`^landing-ci-[0-9a-f]{24}-[12]$`)

// EnsureLandingCI only creates a named CI run, never delivery. GET before
// create and GET after AlreadyExists/ambiguous create make retries idempotent.
// A name collision with another revision is an error, never adoption.
func (h *Handler) EnsureLandingCI(ctx context.Context, name, revision string) error {
	return h.svc.ensureLandingCI(ctx, name, revision)
}
func (s *Service) ensureLandingCI(ctx context.Context, name, revision string) error {
	if !landingCIName.MatchString(name) || !isFullGitSHA(revision) {
		return fmt.Errorf("invalid landing CI identity")
	}
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return err
	}
	res := dyn.Resource(pipelineRunGVR).Namespace(s.PipelinesNamespace())
	matches := func(obj *unstructured.Unstructured) error {
		pipeline, _, _ := unstructured.NestedString(obj.Object, "spec", "pipelineRef", "name")
		if pipeline != "bifrost-ci-platform" || landingRevision(*obj) != revision || obj.GetLabels()["bifrost.io/trigger"] != "agentrt-landing-drill" {
			return fmt.Errorf("landing CI name collision")
		}
		return nil
	}
	if obj, e := res.Get(ctx, name, metav1.GetOptions{}); e == nil {
		return matches(obj)
	} else if !apierrors.IsNotFound(e) {
		return e
	}
	obj, err := unstructuredFrom(map[string]any{"apiVersion": "tekton.dev/v1", "kind": "PipelineRun", "metadata": map[string]any{"name": name, "namespace": s.PipelinesNamespace(), "labels": map[string]any{"tekton.dev/pipeline": "bifrost-ci-platform", "bifrost.io/repo": "bifrost-platform", "bifrost.io/trigger": "agentrt-landing-drill", "bifrost.io/revision": revision}}, "spec": ciRunSpec("bifrost-ci-platform", revision, "bifrost-platform")})
	if err != nil {
		return err
	}
	if _, err = res.Create(ctx, obj, metav1.CreateOptions{}); err == nil {
		return nil
	}
	if existing, getErr := res.Get(ctx, name, metav1.GetOptions{}); getErr == nil {
		return matches(existing)
	}
	return err
}

// LandingWindowBlocked is intentionally read-only, including expired windows.
// Missing is free; malformed/unreadable is blocked. S2 never owns a window.
func (h *Handler) LandingWindowBlocked(ctx context.Context) (bool, error) {
	return h.svc.landingWindowBlocked(ctx, time.Now())
}
func (s *Service) landingWindowBlocked(ctx context.Context, now time.Time) (bool, error) {
	data, found, err := s.PolicyFacts().ConfigMap(ctx, releaseWindowConfigMap)
	if err != nil {
		return true, err
	}
	if !found {
		return false, nil
	}
	rec, valid := parseWindowCM(&corev1.ConfigMap{Data: data})
	if !valid {
		return true, fmt.Errorf("unreadable release window")
	}
	expires, err := time.Parse(time.RFC3339, rec.ExpiresAt)
	if err != nil {
		return true, fmt.Errorf("unreadable release window expiry")
	}
	return now.Before(expires), nil
}
