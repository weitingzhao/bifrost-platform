package delivery

import (
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func isInfraPipeline(name string) bool {
	return name == "bifrost-ci-infra" || name == "bifrost-build-ci-infra-tools"
}

// Infra CI has an explicit five-parameter contract. These exceptions do not
// widen the caller override rules for other pipelines or source/image settings.
func infraPipelineInputs(name, revision string, extra map[string]string) (map[string]string, error) {
	params := map[string]string{"revision": revision}
	if name == "bifrost-ci-infra" {
		params["base"], params["platformRevision"], params["generation"], params["purpose"] = "", "", "", "push"
	}
	for _, key := range sortedKeysString(extra) {
		if _, allowed := params[key]; !allowed {
			return nil, fmt.Errorf("param %q cannot be set by the caller for %s", key, name)
		}
		if key == "revision" && revision != "" && revision != extra[key] {
			return nil, fmt.Errorf("revision %q and params.revision %q must match", revision, extra[key])
		}
		params[key] = extra[key]
	}
	if !callerParamSHA.MatchString(params["revision"]) {
		return nil, fmt.Errorf("revision %q must be a 40-character lowercase git SHA", params["revision"])
	}
	if name == "bifrost-ci-infra" {
		purpose := params["purpose"]
		if purpose != "push" && purpose != "landing" {
			return nil, fmt.Errorf("param purpose must be push or landing")
		}
		for _, key := range []string{"base", "platformRevision"} {
			value := params[key]
			if (purpose == "landing" || value != "") && !callerParamSHA.MatchString(value) {
				return nil, fmt.Errorf("param %s must be a 40-character lowercase git SHA", key)
			}
		}
		if value := params["generation"]; purpose == "landing" || value != "" {
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n <= 0 || strconv.FormatInt(n, 10) != value {
				return nil, fmt.Errorf("param generation must be a positive decimal integer")
			}
		}
	}
	return params, nil
}

func infraPipelineRunSpec(name, revision, tag string, extra map[string]string, pipe *unstructured.Unstructured) (map[string]any, string, error) {
	if tag != "" {
		return nil, revision, fmt.Errorf("tag cannot be set for %s; the pipeline fixes its image identity", name)
	}
	values, err := infraPipelineInputs(name, revision, extra)
	if err != nil {
		return nil, revision, err
	}
	declared, err := pipelineDeclarations(pipe, "params")
	if err != nil {
		return nil, revision, err
	}
	declarations := map[string]map[string]any{}
	for _, raw := range declared {
		p := raw.(map[string]any)
		declarations[p["name"].(string)] = p
	}
	params := []map[string]any{}
	for _, key := range sortedKeysString(values) {
		declaration, exists := declarations[key]
		if !exists {
			return nil, revision, fmt.Errorf("param %q is not declared by this pipeline", key)
		}
		if kind, ok := declaration["type"]; ok && kind != "string" {
			return nil, revision, fmt.Errorf("param %q must be declared as string", key)
		}
		params = append(params, map[string]any{"name": key, "value": values[key]})
	}
	workspaces, err := pipelineDeclarations(pipe, "workspaces")
	if err != nil {
		return nil, revision, err
	}
	source := false
	for _, raw := range workspaces {
		source = source || raw.(map[string]any)["name"] == "source"
	}
	if !source {
		return nil, revision, fmt.Errorf("required workspace %q is not declared by this pipeline", "source")
	}
	spec := map[string]any{
		"pipelineRef": map[string]any{"name": name}, "params": params,
		"workspaces": pipelineRunWorkspaces(name), "taskRunTemplate": amd64CITaskRunTemplate(),
	}
	run, err := unstructuredFrom(map[string]any{"spec": spec})
	if err == nil {
		err = validateRunRequirements(pipe, run)
	}
	if err != nil {
		return nil, revision, err
	}
	return spec, values["revision"], nil
}
