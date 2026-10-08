package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Release-window gate (TD-162). The rules match scripts/release/window_decision.py
// decide_api: one ConfigMap, `what` is a comma-separated repo list, and a caller
// who is not the holder is refused.

const releaseWindowConfigMap = "bifrost-release-window"

// Pipelines that refuse to start unless the window is already open for their repo.
var guardedPipelineRepos = map[string]map[string]bool{
	"bifrost-deliver-research":       {"bifrost-research": true},
	"bifrost-build-research-dagster": {"bifrost-research": true},
	"bifrost-build-market-data":      {"bifrost-platform-plugin-market-data": true},
	"bifrost-build-flex-query":       {"bifrost-platform-plugin-flex-query": true},
	"bifrost-build-ib-gateway":       {"bifrost-platform-plugin": true},
}

var tradePipelineRepos = map[string]map[string]bool{
	"bifrost-deliver-stg": setOf(
		"bifrost-trade-core", "bifrost-trade-api", "bifrost-trade-worker",
		"bifrost-trade-frontend", "bifrost-trade-infra"),
	"bifrost-deliver-prod": setOf(
		"bifrost-trade-core", "bifrost-trade-api", "bifrost-trade-worker",
		"bifrost-trade-frontend", "bifrost-trade-infra"),
	"bifrost-deliver-platform":      {"bifrost-platform": true, "bifrost-ui": true},
	"bifrost-deliver-platform-prod": {"bifrost-platform": true, "bifrost-ui": true},
}

// Revisions that must be a full git SHA (TD-95, TD-122).
var shaRevisionPipelines = map[string]bool{
	"bifrost-deliver-research":       true,
	"bifrost-build-research-dagster": true,
	"bifrost-build-ib-gateway":       true,
}

func setOf(names ...string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}

func parseWhat(what string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(what, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out[part] = true
		}
	}
	return out
}

func intersects(a, b map[string]bool) bool {
	for k := range a {
		if b[k] {
			return true
		}
	}
	return false
}

func isFullGitSHA(revision string) bool {
	rev := strings.TrimSpace(revision)
	if len(rev) != 40 {
		return false
	}
	for _, c := range rev {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func requiresFullSHA(pipeline string) bool {
	return shaRevisionPipelines[pipeline]
}

func mustHoldWindow(pipeline string) bool {
	_, ok := guardedPipelineRepos[pipeline]
	return ok
}

func requiredRepos(pipeline string) (map[string]bool, bool) {
	if repos, ok := guardedPipelineRepos[pipeline]; ok {
		return repos, true
	}
	if repos, ok := tradePipelineRepos[pipeline]; ok {
		return repos, true
	}
	return nil, false
}

func sortedKeys(set map[string]bool) string {
	names := make([]string, 0, len(set))
	for k := range set {
		names = append(names, k)
	}
	// Small fixed sets; a simple sort keeps the refusal text stable.
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return strings.Join(names, ",")
}

// decideReleaseWindow returns "" to allow, or a REFUSED message.
// found is false when the ConfigMap is absent. callerWho must equal the
// holder's who whenever a window is open.
func decideReleaseWindow(found bool, window map[string]any, pipeline, callerWho string) string {
	if !found || window == nil {
		if mustHoldWindow(pipeline) {
			repos, _ := requiredRepos(pipeline)
			return fmt.Sprintf(
				"REFUSED: no release window for %s. Open one first: release.sh hold --what %s",
				pipeline, sortedKeys(repos))
		}
		return ""
	}
	whatStr, _ := window["what"].(string)
	holder, _ := window["who"].(string)
	if holder == "" {
		holder = "unknown"
	}
	what := parseWhat(whatStr)
	repos, known := requiredRepos(pipeline)
	if !known {
		return fmt.Sprintf(
			"REFUSED: release window held by %s (what=%s); %s is not part of that release",
			holder, whatStr, pipeline)
	}
	if !intersects(what, repos) {
		return fmt.Sprintf(
			"REFUSED: release window held by someone else (who=%s what=%s); %s needs one of %s",
			holder, whatStr, pipeline, sortedKeys(repos))
	}
	if strings.TrimSpace(callerWho) == "" {
		return fmt.Sprintf(
			"REFUSED: missing who; pass who matching the release window holder (who=%s what=%s)",
			holder, whatStr)
	}
	if strings.TrimSpace(callerWho) != holder {
		return fmt.Sprintf(
			"REFUSED: release window held by someone else (who=%s what=%s)",
			holder, whatStr)
	}
	return ""
}

func (s *Service) readReleaseWindow(ctx context.Context) (bool, map[string]any, error) {
	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		return false, nil, err
	}
	cm, err := clientset.CoreV1().ConfigMaps(s.PipelinesNamespace()).Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	raw := ""
	if cm.Data != nil {
		raw = cm.Data["window.json"]
	}
	if strings.TrimSpace(raw) == "" {
		return false, nil, nil
	}
	var window map[string]any
	if err := json.Unmarshal([]byte(raw), &window); err != nil {
		return false, nil, fmt.Errorf("release window is not JSON: %w", err)
	}
	return true, window, nil
}

func (s *Service) releaseWindowMessage(ctx context.Context, pipeline, callerWho string) string {
	found, window, err := s.readReleaseWindow(ctx)
	if err != nil {
		if mustHoldWindow(pipeline) {
			return fmt.Sprintf("REFUSED: cannot read the release window (%v)", err)
		}
		return ""
	}
	return decideReleaseWindow(found, window, pipeline, callerWho)
}
