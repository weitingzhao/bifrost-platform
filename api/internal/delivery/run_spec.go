package delivery

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func buildPipelineRunSpec(pipelineName, rev, tag string, extra map[string]string, pipe *unstructured.Unstructured) (map[string]any, string, error) {
	if isCIPipeline(pipelineName) {
		if tag != "" {
			return nil, rev, fmt.Errorf("tag %q cannot be set for CI: CI builds no image", tag)
		}
		commit, repo, err := ciRunInputs(pipelineName, rev, extra)
		if err != nil {
			return nil, rev, err
		}
		return ciRunSpec(pipelineName, commit, repo), commit, nil
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
			return nil, rev, mergeErr
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
		return nil, rev, err
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
		spec["taskRunSpecs"] = platformTaskRunSpecs(pipelineName, pipe)
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
	return spec, rev, nil
}
