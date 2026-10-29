package delivery

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation"
)

// NamedDeliverInput is frozen before started; IntentSeq is the committed
// event sequence returned by the store, never the pre-append placeholder 0.
type NamedDeliverInput struct {
	Batch, Try, IntentSeq int64
	P, U, Infra           string
}
type NamedDeliverIdentity struct {
	Name, Who, Fingerprint      string
	Params, Labels, Annotations map[string]string
}

func (in NamedDeliverInput) Identity() (NamedDeliverIdentity, error) {
	id := NamedDeliverIdentity{}
	if in.Batch < 1 || in.Try < 1 || in.IntentSeq < 1 || !callerParamSHA.MatchString(in.P) || !callerParamSHA.MatchString(in.U) || !callerParamSHA.MatchString(in.Infra) {
		return id, fmt.Errorf("invalid named STG release input")
	}
	n, attempt := strconv.FormatInt(in.Batch, 10), strconv.FormatInt(in.Try, 10)
	id.Name, id.Who = fmt.Sprintf("stgrel-%s-%s-%s", n, attempt, in.P[:8]), fmt.Sprintf("stgrel-bifrost-platform-%s-%s", n, attempt)
	id.Params = map[string]string{"revision": in.P, "uiRevision": in.U, "infraRevision": in.Infra}
	id.Labels = map[string]string{"bifrost.io/stg-release": "true", "bifrost.io/stg-release-n": n, "bifrost.io/stg-release-try": attempt}
	id.Annotations = map[string]string{"bifrost.io/stg-release-batch": "stg:bifrost-platform:" + n, "bifrost.io/stg-release-intent": strconv.FormatInt(in.IntentSeq, 10)}
	if len(id.Name) > 40 || len(validation.IsDNS1123Subdomain(id.Name)) != 0 || !windowWhoPattern.MatchString(id.Who) {
		return id, fmt.Errorf("invalid named STG release name")
	}
	for key, value := range id.Labels {
		if len(validation.IsQualifiedName(key)) != 0 || len(validation.IsValidLabelValue(value)) != 0 {
			return id, fmt.Errorf("invalid STG release label")
		}
	}
	for key := range id.Annotations {
		if len(validation.IsQualifiedName(key)) != 0 {
			return id, fmt.Errorf("invalid STG release annotation")
		}
	}
	var err error
	id.Fingerprint, err = stgRunFingerprint(id.Params, id.Labels, id.Annotations)
	return id, err
}
func stringObject(values map[string]string) map[string]any {
	out := map[string]any{}
	for k, v := range values {
		out[k] = v
	}
	return out
}
func stgRunFingerprint(params, labels, annotations map[string]string) (string, error) {
	return canon.Sum(map[string]any{"namespace": STGReleaseNamespace, "pipeline": STGReleasePipeline,
		"params_canonical": stringObject(params), "labels": stringObject(labels), "annotations": stringObject(annotations)})
}

func namedStrings(obj *unstructured.Unstructured, fields ...string) (map[string]string, error) {
	items, _, err := unstructured.NestedSlice(obj.Object, fields...)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed %v", ErrSTGEvidence, fields)
	}
	out := map[string]string{}
	for _, item := range items {
		m, ok := item.(map[string]any)
		name, _ := m["name"].(string)
		value, valid := m["value"].(string)
		_, duplicate := out[name]
		if !ok || name == "" || !valid || duplicate {
			return nil, fmt.Errorf("%w: duplicate or malformed %v", ErrSTGEvidence, fields)
		}
		out[name] = value
	}
	return out, nil
}

// MatchNamedDeliverRun tolerates controller-owned metadata, but hashes only
// the exact managed keys. Every actual param participates in the fingerprint.
// Execution settings must also match the immutable B5 v1 contract.
func MatchNamedDeliverRun(obj *unstructured.Unstructured, in NamedDeliverInput, requireHold bool) error {
	id, err := in.Identity()
	if err != nil {
		return err
	}
	if obj == nil || obj.GetName() != id.Name || obj.GetNamespace() != STGReleaseNamespace || obj.GetUID() == "" {
		return ErrSTGCollision
	}
	ref, _, _ := unstructured.NestedMap(obj.Object, "spec", "pipelineRef")
	_, inline, _ := unstructured.NestedFieldNoCopy(obj.Object, "spec", "pipelineSpec")
	if inline || !reflect.DeepEqual(ref, map[string]any{"name": STGReleasePipeline}) {
		return ErrSTGCollision
	}
	if !matchesSTGExecutionV1(obj) {
		return ErrSTGCollision
	}
	params, err := namedStrings(obj, "spec", "params")
	if err != nil {
		return ErrSTGCollision
	}
	labels, annotations := map[string]string{}, map[string]string{}
	for k := range id.Labels {
		labels[k] = obj.GetLabels()[k]
	}
	for k := range id.Annotations {
		annotations[k] = obj.GetAnnotations()[k]
	}
	hash, err := stgRunFingerprint(params, labels, annotations)
	if err != nil || hash != id.Fingerprint || requireHold && !slices.Contains(obj.GetFinalizers(), STGReleaseFinalizer) {
		return ErrSTGCollision
	}
	return nil
}
