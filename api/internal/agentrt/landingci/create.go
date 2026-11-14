package landingci

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// create creates the PipelineRun name once: an existing run of that name is
// accepted only with this spec and these labels, else it collides.
func (c *Client) create(ctx context.Context, name, pipeline string, params, labels map[string]string) error {
	res, err := c.resource()
	if err != nil {
		return err
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
		want := withoutPreRepoURL(params, counts)
		if pipeline == "bifrost-ci-platform" {
			want = withoutPreBase(want, counts)
		}
		if inline || !reflect.DeepEqual(actualRef, expectedRef) || !reflect.DeepEqual(values, want) {
			return fmt.Errorf("landing CI name collision")
		}
		for name := range want {
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

// A run this operation created before platform CI carried repo-url is still
// its run: it is accepted here (landing reads never needed repo-url) and stays
// Unknown(ci_identity) to B5's ReadRelease. Any other difference collides.
func withoutPreRepoURL(params map[string]string, counts map[string]int) map[string]string {
	return withoutAbsent(params, counts, "repo-url")
}

// Likewise a platform run made before BaseParam was on (no base param at
// all) is still this operation's run; one that names another base collides.
func withoutPreBase(params map[string]string, counts map[string]int) map[string]string {
	return withoutAbsent(params, counts, BaseParamName)
}

// withoutAbsent drops param from params when the existing run has none.
func withoutAbsent(params map[string]string, counts map[string]int, param string) map[string]string {
	if _, ok := params[param]; !ok || counts[param] != 0 {
		return params
	}
	out := make(map[string]string, len(params)-1)
	for name, value := range params {
		if name != param {
			out[name] = value
		}
	}
	return out
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
