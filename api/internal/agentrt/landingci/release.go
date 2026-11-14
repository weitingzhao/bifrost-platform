package landingci

import (
	"fmt"
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const PlatformRepoURL = "http://gitea.cicd.svc.cluster.local:3000/bifrost/bifrost-platform.git"

// ReleaseEvidence discovers U and infra from CI of exactly P. It does not
// assert they equal a previously frozen snapshot (the controller owns that).
type ReleaseEvidence struct {
	Evidence
	PipelineRef, RepoURL string
	TestedBy             string
	ReadAt               time.Time
	// Undecided names the runs of P newer than the selected one that have no
	// verdict yet (R5:5.7-2). They were passed over, never trusted.
	Undecided []string
	// Partial names the greens of P that tested only part of the API
	// (api-scope=console). They neither vouch for P nor hide another run.
	Partial []string
	// Stale is retained for historical observations. New reads never set it.
	Stale []string
	// LastFull* is the newest full-scope green of any revision (any P); a
	// zero LastFullAt means none was found.
	LastFullRun, LastFullCommit string
	LastFullAt                  time.Time
}

// FullMaxAge remains the landing reuse freshness cap and the historical
// observation cap. It no longer decides STG eligibility.
const FullMaxAge = 48 * time.Hour

// The api-scope result of a ci-platform run: what its test-api step ran.
// A run without it predates the result and ran everything (full).
const (
	APIScopeResult = "api-scope"
	// What the run's test-api tested: its packages (relative to the api
	// module, space separated), the store package's -run pattern ("" for
	// all of it) and why it chose its scope.
	APIPackagesResult = "api-packages"
	APIStoreRunResult = "api-store-run"
	APIScopeWhyResult = "api-scope-why"
	APIScopeFull      = "full"
	APIScopeConsole   = "console"
	// An incremental run tested the API packages its change reaches (R5:5.7-1).
	APIScopeIncremental = "incremental"
)

// fullScope: a run's results say it tested the whole API.
func fullScope(results map[string]string) bool {
	scope, named := results[APIScopeResult]
	return !named || scope == APIScopeFull
}

// releaseScope: the results vouch for P at STG, as a full run or as an
// incremental one, independently of the daily audit.
func (e ReleaseEvidence) releaseScope(results map[string]string) bool {
	return fullScope(results) || results[APIScopeResult] == APIScopeIncremental
}

// Record is the closed ci_accepted.ci shape consumed by B5-a. Never turn an
// unsuccessful observation into a record just because it carries old results.
func (e ReleaseEvidence) Record() (map[string]any, error) {
	if e.Status != "Succeeded" || e.Reason != "" || !e.releaseScope(e.Results) || e.Run == "" || e.UID == "" || e.CreatedAt.IsZero() || e.ReadAt.Before(e.CreatedAt) || e.PipelineRef != "bifrost-ci-platform" || e.RepoURL != PlatformRepoURL || e.PlatformCommit != e.R || !fullSHA.MatchString(e.PlatformCommit) || !fullSHA.MatchString(e.UICommit) || !fullSHA.MatchString(e.InfraCommit) {
		return nil, fmt.Errorf("release CI is not accepted evidence")
	}
	record := map[string]any{"name": e.Run, "uid": e.UID, "created_at": e.CreatedAt.UTC().Format(time.RFC3339Nano), "platform_commit": e.PlatformCommit, "ui_commit": e.UICommit, "infra_commit": e.InfraCommit, "pipeline_ref": e.PipelineRef, "repo_url": e.RepoURL, "read_at": e.ReadAt.UTC().Format(time.RFC3339Nano)}
	if e.TestedBy != "" {
		record["tested_by"] = e.TestedBy
	}
	return record, nil
}

// emptyResultAllowed are the results a green run may leave empty: no failed
// tests, a full run's empty package list and store pattern, and a scope with
// nothing to explain (infra test-api, countdown 20), and the tested-by of a
// run that reused nothing (TD-533; ClaimsReuse). Every other result must be
// named once and non-empty.
var emptyResultAllowed = map[string]bool{
	FailedTestsResult: true, APIPackagesResult: true, APIStoreRunResult: true, APIScopeWhyResult: true,
	"tested-by": true,
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
		if name == "" || count != 1 || values[name] == "" && !emptyResultAllowed[name] {
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
	if scope, named := values[APIScopeResult]; named && scope != APIScopeFull && scope != APIScopeConsole && scope != APIScopeIncremental {
		out.Status, out.Reason = "Unknown", "ci_results"
		return out
	}
	out.PlatformCommit, out.UICommit, out.InfraCommit = values["platform-commit"], values["ui-commit"], values["infra-commit"]
	return out
}
