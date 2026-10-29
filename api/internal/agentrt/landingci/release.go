package landingci

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const PlatformRepoURL = "http://gitea.cicd.svc.cluster.local:3000/bifrost/bifrost-platform.git"

// ReleaseEvidence discovers U and infra from CI of exactly P. It does not
// assert they equal a previously frozen snapshot (the controller owns that).
type ReleaseEvidence struct {
	Evidence
	PipelineRef, RepoURL string
	ReadAt               time.Time
}

// Record is the closed ci_accepted.ci shape consumed by B5-a. Never turn an
// unsuccessful observation into a record just because it carries old results.
func (e ReleaseEvidence) Record() (map[string]any, error) {
	if e.Status != "Succeeded" || e.Reason != "" || e.Run == "" || e.UID == "" || e.CreatedAt.IsZero() || e.ReadAt.Before(e.CreatedAt) || e.PipelineRef != "bifrost-ci-platform" || e.RepoURL != PlatformRepoURL || e.PlatformCommit != e.R || !fullSHA.MatchString(e.PlatformCommit) || !fullSHA.MatchString(e.UICommit) || !fullSHA.MatchString(e.InfraCommit) {
		return nil, fmt.Errorf("release CI is not accepted evidence")
	}
	return map[string]any{"name": e.Run, "uid": e.UID, "created_at": e.CreatedAt.UTC().Format(time.RFC3339Nano), "platform_commit": e.PlatformCommit, "ui_commit": e.UICommit, "infra_commit": e.InfraCommit, "pipeline_ref": e.PipelineRef, "repo_url": e.RepoURL, "read_at": e.ReadAt.UTC().Format(time.RFC3339Nano)}, nil
}

// ReadRelease implements B5 §4 without changing Read/legacy selection rules.
// Missing repo-url is unknown, never inferred from a label or today's Pipeline
// default. Older CI installations must supply that evidence before B5 opens.
func (c *Client) ReadRelease(ctx context.Context, revision string) (ReleaseEvidence, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out := ReleaseEvidence{PipelineRef: "bifrost-ci-platform", RepoURL: PlatformRepoURL}
	out.R, out.Status = revision, "Missing"
	if !fullSHA.MatchString(revision) || c.namespace() != "cicd" {
		return out, fmt.Errorf("invalid release CI revision")
	}
	items, err := c.pipelineRuns(ctx, out.PipelineRef)
	out.ReadAt = time.Now().UTC()
	if err != nil {
		return out, err
	}
	var matches []unstructured.Unstructured
	for _, item := range items {
		params, counts := namedValues(item, "spec", "params")
		// Keep malformed duplicate revisions in the ordering if any entry
		// names P, so a contradictory duplicate cannot revive older green.
		rawParams, _, _ := unstructured.NestedSlice(item.Object, "spec", "params")
		matchesRevision := false
		for _, raw := range rawParams {
			param, _ := raw.(map[string]any)
			matchesRevision = matchesRevision || param["name"] == "revision" && param["value"] == revision
		}
		if !matchesRevision {
			continue
		}
		if counts["repo-url"] == 1 && params["repo-url"] != PlatformRepoURL {
			continue
		}
		if item.GetCreationTimestamp().Time.IsZero() || item.GetCreationTimestamp().Time.After(out.ReadAt) {
			out.Status, out.Reason = "Unknown", "ci_identity"
			return out, nil
		}
		matches = append(matches, item)
	}
	if len(matches) == 0 {
		return out, nil
	}
	sort.Slice(matches, func(i, j int) bool {
		a, b := matches[i].GetCreationTimestamp(), matches[j].GetCreationTimestamp()
		if a.Equal(&b) {
			return matches[i].GetName() < matches[j].GetName()
		}
		return a.After(b.Time)
	})
	chosen := matches[0]
	selected := releaseResult(chosen, revision)
	out.Evidence = selected
	for _, item := range matches[1:] {
		if !item.GetCreationTimestamp().Time.Equal(chosen.GetCreationTimestamp().Time) {
			break
		}
		other := releaseResult(item, revision)
		if other.Status != selected.Status || other.Reason != selected.Reason || !reflect.DeepEqual(other.Results, selected.Results) {
			out.Status, out.Reason = "Unknown", "ci_tie"
		}
	}
	return out, nil
}

func releaseResult(item unstructured.Unstructured, revision string) Evidence {
	var out Evidence
	out.Run, out.UID, out.R = item.GetName(), string(item.GetUID()), revision
	out.CreatedAt, out.Status = item.GetCreationTimestamp().Time, runStatus(item)
	out.Results, _ = namedValues(item, "status", "results")
	ref, _, _ := unstructured.NestedMap(item.Object, "spec", "pipelineRef")
	params, counts := namedValues(item, "spec", "params")
	if !strictCandidate(item, "bifrost-platform", revision) || !reflect.DeepEqual(ref, map[string]any{"name": "bifrost-ci-platform"}) || counts["repo-url"] != 1 || params["repo-url"] != PlatformRepoURL || out.UID == "" || out.Run == "" {
		out.Status, out.Reason = "Unknown", "ci_identity"
		return out
	}
	if out.Status != "Succeeded" {
		return out
	}
	values, resultCounts := namedValues(item, "status", "results")
	for name, count := range resultCounts {
		if name == "" || count != 1 || values[name] == "" {
			out.Status, out.Reason = "Unknown", "ci_results"
			return out
		}
	}
	for _, name := range []string{"platform-commit", "ui-commit", "infra-commit"} {
		if !fullSHA.MatchString(values[name]) || name == "platform-commit" && values[name] != revision {
			out.Status, out.Reason = "Unknown", "ci_results"
			return out
		}
	}
	out.PlatformCommit, out.UICommit, out.InfraCommit = values["platform-commit"], values["ui-commit"], values["infra-commit"]
	return out
}
