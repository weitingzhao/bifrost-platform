package landingci

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Ensure creates CI for a fully pinned candidate. The existing platform run
// name format stays unchanged; operation identity is persisted by its caller.
func (c *Client) Ensure(ctx context.Context, name string, key Key) error {
	repo, err := key.validate()
	if err != nil {
		return err
	}
	params := map[string]string{"revision": key.Revision}
	for sibling, param := range repo.SiblingParams {
		params[param] = key.Siblings[sibling]
	}
	if key.Repo == "bifrost-trade-infra" {
		if name != landing.InfraCIName(key.Revision, key.Base, key.Siblings["bifrost-platform"], key.Generation) {
			return fmt.Errorf("invalid landing CI identity")
		}
		params = infraParams(key)
	}
	return c.ensure(ctx, name, key.Repo, repo.Pipeline, key.Revision, params)
}

// EnsureLandingCI preserves v1's platform spec, including uiRevision=main.
// It shares all collision and uncertain-create handling with the pinned API.
func (c *Client) EnsureLandingCI(ctx context.Context, name, revision string) error {
	return c.ensure(ctx, name, "bifrost-platform", "bifrost-ci-platform", revision, map[string]string{"revision": revision, "uiRevision": "main"})
}
func (c *Client) ensure(ctx context.Context, name, repo, pipeline, revision string, params map[string]string) error {
	r, supported := landing.LandingRepos[repo]
	if !supported || !r.CINamePattern.MatchString(name) || !fullSHA.MatchString(revision) {
		return fmt.Errorf("invalid landing CI identity")
	}
	res, err := c.resource()
	if err != nil {
		return err
	}
	labels := map[string]string{"tekton.dev/pipeline": pipeline, "bifrost.io/repo": repo, "bifrost.io/trigger": "agentrt-landing-drill", "bifrost.io/revision": revision}
	if repo == "bifrost-trade-infra" {
		labels["bifrost.io/base"], labels["bifrost.io/purpose"] = params["base"], "landing"
	}
	desired := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "PipelineRun", "spec": platformSpec(pipeline, params)}}
	desired.SetName(name)
	desired.SetNamespace(c.namespace())
	desired.SetLabels(labels)
	matches := func(obj *unstructured.Unstructured) error {
		actualRef, _, _ := unstructured.NestedMap(obj.Object, "spec", "pipelineRef")
		expectedRef, _, _ := unstructured.NestedMap(desired.Object, "spec", "pipelineRef")
		_, inline, _ := unstructured.NestedFieldNoCopy(obj.Object, "spec", "pipelineSpec")
		values, counts := namedValues(*obj, "spec", "params")
		if inline || !reflect.DeepEqual(actualRef, expectedRef) || !reflect.DeepEqual(values, params) {
			return fmt.Errorf("landing CI name collision")
		}
		for name := range params {
			if counts[name] != 1 {
				return fmt.Errorf("landing CI name collision")
			}
		}
		for key, value := range labels {
			if obj.GetLabels()[key] != value {
				return fmt.Errorf("landing CI name collision")
			}
		}
		return nil
	}
	if obj, e := res.Get(ctx, name, metav1.GetOptions{}); e == nil {
		return matches(obj)
	} else if !apierrors.IsNotFound(e) {
		return e
	}
	if obj, e := res.Create(ctx, desired, metav1.CreateOptions{}); e == nil {
		return matches(obj)
	} else {
		err = e
	}
	if existing, e := res.Get(ctx, name, metav1.GetOptions{}); e == nil {
		return matches(existing)
	}
	return err
}

// This is the existing platform CI spec, now owned by the hard landingci
// package. Generic delivery CI remains independent of landing policy.
func platformSpec(pipeline string, values map[string]string) map[string]any {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	params := make([]any, 0, len(keys))
	for _, key := range keys {
		params = append(params, map[string]any{"name": key, "value": values[key]})
	}
	return map[string]any{
		"pipelineRef": map[string]any{"name": pipeline}, "params": params,
		"taskRunTemplate": map[string]any{"podTemplate": map[string]any{
			"nodeSelector": map[string]any{"kubernetes.io/arch": "amd64"},
			"tolerations":  []any{map[string]any{"key": "node-role.kubernetes.io/control-plane", "operator": "Exists", "effect": "NoSchedule"}},
		}},
		"workspaces": []any{map[string]any{"name": "source", "volumeClaimTemplate": map[string]any{"spec": map[string]any{
			"accessModes": []any{"ReadWriteOnce"}, "storageClassName": "local-path", "resources": map[string]any{"requests": map[string]any{"storage": "2Gi"}},
		}}}},
	}
}
