package delivery

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// pipelineParamValue is the character set for a caller-supplied pipeline param.
// It is the actuation-policy path set plus ':' and '+', so a git ref or an
// image tag fits. The length cap is the same one validateRevision uses.
var pipelineParamValue = regexp.MustCompile(`^[A-Za-z0-9._:@/+-]{1,256}$`)

// runTimeoutPattern is a Tekton duration such as 1h0m0s. The pipeline declares
// it; this only rejects a value that is not a duration.
var runTimeoutPattern = regexp.MustCompile(`^[0-9hms]{2,16}$`)

// declaredPipelineParams reads spec.params names from a Pipeline object.
func declaredPipelineParams(obj *unstructured.Unstructured) map[string]bool {
	out := map[string]bool{}
	if obj == nil {
		return out
	}
	raw, _, _ := unstructured.NestedSlice(obj.Object, "spec", "params")
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		name = strings.TrimSpace(name)
		if name != "" {
			out[name] = true
		}
	}
	return out
}

// mergePipelineParams overlays caller params on the legacy revision/tag set.
// A caller param must be declared on the Pipeline. When both name the same
// param, the caller value wins. Legacy keys the live Pipeline does not declare
// are dropped, so an older Pipeline is not sent a param it cannot accept.
// An empty caller map leaves the legacy set, filtered the same way.
func mergePipelineParams(declared map[string]bool, legacy []map[string]any, extra map[string]string) ([]map[string]any, error) {
	if len(extra) > 0 && len(declared) == 0 {
		return nil, fmt.Errorf("pipeline declares no params")
	}
	for name, value := range extra {
		name = strings.TrimSpace(name)
		if !declared[name] {
			return nil, fmt.Errorf("param %q is not declared by this pipeline", name)
		}
		if !pipelineParamValue.MatchString(strings.TrimSpace(value)) {
			return nil, fmt.Errorf("param %s has characters outside the allow-list or is empty or longer than 256", name)
		}
	}
	merged := map[string]string{}
	for _, p := range legacy {
		name, _ := p["name"].(string)
		value, _ := p["value"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if len(declared) > 0 && !declared[name] {
			continue
		}
		merged[name] = value
	}
	for name, value := range extra {
		merged[strings.TrimSpace(name)] = strings.TrimSpace(value)
	}
	if len(merged) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(merged))
	for name := range merged {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		out = append(out, map[string]any{"name": name, "value": merged[name]})
	}
	return out, nil
}

// applyDeclaredRunExtras copies run settings the Pipeline itself declares via
// annotations onto the PipelineRun. Tekton keeps timeouts and the pod template
// on the run, not on the Pipeline spec.
func applyDeclaredRunExtras(spec map[string]any, ann map[string]string) error {
	if len(ann) == 0 {
		return nil
	}
	if t := strings.TrimSpace(ann["bifrost.io/run-timeout"]); t != "" {
		if !runTimeoutPattern.MatchString(t) || !strings.ContainsAny(t, "hms") {
			return fmt.Errorf("pipeline annotation bifrost.io/run-timeout is not a duration")
		}
		spec["timeouts"] = map[string]any{"pipeline": t}
	}
	if strings.TrimSpace(ann["bifrost.io/run-arch"]) == "amd64" {
		spec["taskRunTemplate"] = map[string]any{
			"serviceAccountName": "default",
			"podTemplate": map[string]any{
				"nodeSelector": map[string]any{"kubernetes.io/arch": "amd64"},
				"tolerations": []any{
					map[string]any{
						"key":      "node-role.kubernetes.io/control-plane",
						"operator": "Exists",
						"effect":   "NoSchedule",
					},
				},
			},
		}
	}
	return nil
}
