package delivery

import (
	"fmt"
	"slices"
	"sort"
)

var pythonCIRepos = []string{"bifrost-trade-core", "bifrost-trade-api", "bifrost-trade-worker", "bifrost-research"}

func isCIPipeline(name string) bool {
	switch name {
	case "bifrost-ci-platform", "bifrost-ci-frontend", "bifrost-ci-python", "bifrost-ci-infra":
		return true
	}
	return false
}

// Resolve the effective commit before probing its repository. CI callers cannot
// override the trigger's other refs or source settings.
func ciRunInputs(name, revision string, extra map[string]string) (string, string, error) {
	if name == "bifrost-ci-infra" {
		params, err := infraPipelineInputs(name, revision, extra)
		return params["revision"], "bifrost-trade-infra", err
	}
	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if key != "revision" && !(name == "bifrost-ci-python" && key == "repo") {
			return "", "", fmt.Errorf("param %q cannot be set by the caller for CI", key)
		}
	}
	if value, ok := extra["revision"]; ok {
		if revision != "" && revision != value {
			return "", "", fmt.Errorf("revision %q and params.revision %q must match", revision, value)
		}
		revision = value
	}
	if !callerParamSHA.MatchString(revision) {
		return "", "", fmt.Errorf("revision %q must be a 40-character lowercase git SHA", revision)
	}
	repo := ""
	switch name {
	case "bifrost-ci-platform":
		repo = "bifrost-platform"
	case "bifrost-ci-frontend":
		repo = "bifrost-trade-frontend"
	case "bifrost-ci-python":
		repo = extra["repo"]
		if !slices.Contains(pythonCIRepos, repo) {
			return "", "", fmt.Errorf("param repo %q is not an accepted python-ci repository", repo)
		}
	}
	return revision, repo, nil
}

// platformCIRepoURL is the repository bifrost-ci-platform clones P from. W-31
// B5's release reader accepts a platform CI run only when its spec.params name
// exactly this repo-url; it never infers it from the Pipeline default.
const platformCIRepoURL = giteaInClusterBase + "/" + giteaOrg + "/bifrost-platform.git"

func ciRunSpec(name, revision, repo string) map[string]any {
	params := []map[string]any{{"name": "revision", "value": revision}}
	if name == "bifrost-ci-python" {
		params = append(params, map[string]any{"name": "repo", "value": repo})
	} else {
		params = append(params, map[string]any{"name": "uiRevision", "value": "main"})
	}
	if name == "bifrost-ci-platform" {
		params = append(params, map[string]any{"name": "repo-url", "value": platformCIRepoURL})
	}
	size := "2Gi"
	if name == "bifrost-ci-frontend" {
		size = "3Gi"
	}
	return map[string]any{
		"pipelineRef":     map[string]any{"name": name},
		"params":          params,
		"taskRunTemplate": amd64CITaskRunTemplate(),
		"workspaces": []map[string]any{{"name": "source", "volumeClaimTemplate": map[string]any{"spec": map[string]any{
			"accessModes": []any{"ReadWriteOnce"}, "storageClassName": "local-path", "resources": map[string]any{"requests": map[string]any{"storage": size}},
		}}}},
	}
}
