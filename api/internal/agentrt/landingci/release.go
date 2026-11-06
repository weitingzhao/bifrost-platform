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
	// Undecided names the runs of P newer than the selected one that have no
	// verdict yet (R5:5.7-2). They were passed over, never trusted.
	Undecided []string
	// Partial names the greens of P that tested only part of the API
	// (api-scope=console). They neither vouch for P nor hide another run.
	Partial []string
	// Stale names the incremental greens of P set aside because no full green
	// of any revision is at most FullMaxAge old at ReadAt.
	Stale []string
	// LastFull* is the newest full-scope green of any revision (any P); a
	// zero LastFullAt means none was found.
	LastFullRun, LastFullCommit string
	LastFullAt                  time.Time
}

// FullMaxAge is how old the last full green may be for an incremental green
// to stand for P at STG. PROD keeps needing a full green of its own commit.
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

// FullFresh: a last full green exists and is at most FullMaxAge old at ReadAt.
func (e ReleaseEvidence) FullFresh() bool {
	return e.LastFullRun != "" && !e.LastFullAt.IsZero() && !e.ReadAt.Before(e.LastFullAt) && e.ReadAt.Sub(e.LastFullAt) <= FullMaxAge
}

// releaseScope: the results vouch for P at STG, as a full run or as an
// incremental one while the last full green is fresh.
func (e ReleaseEvidence) releaseScope(results map[string]string) bool {
	return fullScope(results) || results[APIScopeResult] == APIScopeIncremental && e.FullFresh()
}

// Scope is the api-scope of the run that decided P, for people reading why P
// waits: an incremental green set aside as stale reads incremental, a
// verdict without the result full, and a run with no scope yet (none,
// running) "".
func (e ReleaseEvidence) Scope() string {
	if scope, named := e.Results[APIScopeResult]; named && e.Run != "" {
		return scope
	}
	if e.Run != "" && (e.Status == "Succeeded" || e.Status == "Failed") {
		return APIScopeFull
	}
	if len(e.Stale) > 0 {
		return APIScopeIncremental
	}
	return ""
}

// LastFullAgeHours is the last full green's age at ReadAt in whole hours, -1
// when there is none.
func (e ReleaseEvidence) LastFullAgeHours() int {
	if e.LastFullRun == "" || e.LastFullAt.IsZero() {
		return -1
	}
	return int(e.ReadAt.Sub(e.LastFullAt).Hours())
}

// ScopeNote says which scope decided P and how old the last full green is,
// e.g. "scope=incremental; last full CI green 51 h ago (run r, commit c), cap 48 h".
func (e ReleaseEvidence) ScopeNote() string {
	scope := e.Scope()
	if scope == "" {
		scope = "none"
	}
	maxAge := int(FullMaxAge.Hours())
	if e.LastFullAgeHours() < 0 {
		return fmt.Sprintf("scope=%s; no full CI green found, cap %d h", scope, maxAge)
	}
	return fmt.Sprintf("scope=%s; last full CI green %d h ago (run %s, commit %s), cap %d h", scope, e.LastFullAgeHours(), e.LastFullRun, e.LastFullCommit, maxAge)
}

// Record is the closed ci_accepted.ci shape consumed by B5-a. Never turn an
// unsuccessful observation into a record just because it carries old results.
func (e ReleaseEvidence) Record() (map[string]any, error) {
	if e.Status != "Succeeded" || e.Reason != "" || !e.releaseScope(e.Results) || e.Run == "" || e.UID == "" || e.CreatedAt.IsZero() || e.ReadAt.Before(e.CreatedAt) || e.PipelineRef != "bifrost-ci-platform" || e.RepoURL != PlatformRepoURL || e.PlatformCommit != e.R || !fullSHA.MatchString(e.PlatformCommit) || !fullSHA.MatchString(e.UICommit) || !fullSHA.MatchString(e.InfraCommit) {
		return nil, fmt.Errorf("release CI is not accepted evidence")
	}
	return map[string]any{"name": e.Run, "uid": e.UID, "created_at": e.CreatedAt.UTC().Format(time.RFC3339Nano), "platform_commit": e.PlatformCommit, "ui_commit": e.UICommit, "infra_commit": e.InfraCommit, "pipeline_ref": e.PipelineRef, "repo_url": e.RepoURL, "read_at": e.ReadAt.UTC().Format(time.RFC3339Nano)}, nil
}

// ReadRelease implements B5 §4 without changing Read/legacy selection rules.
// Missing repo-url is unknown, never inferred from a label or today's Pipeline
// default. Older CI installations must supply that evidence before B5 opens.
//
// R5:5.7-2 amends §4.3 for one case only: a newer run of P that has no
// verdict yet (normally main's push CI, still running over the landing CI of
// the same commit) does not hide the newest run that has one. A newer red or
// unusable run is a verdict and still wins over an older green.
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
	out.LastFullRun, out.LastFullCommit, out.LastFullAt = lastFull(items, out.ReadAt)
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
	// A green that ran only the console's share of the API (api-scope=console)
	// says nothing about the rest of P: set it aside before selecting. A red
	// of any scope stays a verdict. An incremental green counts as a full one
	// only while the last full green is at most FullMaxAge old; otherwise it
	// is set aside the same way (Stale).
	kept := matches[:0]
	for _, item := range matches {
		r := releaseResult(item, revision)
		green := r.Status == "Succeeded" && r.Reason == ""
		if green && r.Results[APIScopeResult] == APIScopeConsole {
			out.Partial = append(out.Partial, item.GetName())
			continue
		}
		if green && r.Results[APIScopeResult] == APIScopeIncremental && !out.FullFresh() {
			out.Stale = append(out.Stale, item.GetName())
			continue
		}
		kept = append(kept, item)
	}
	matches = kept
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
	// Pass over strictly newer runs that have no verdict yet and whose own
	// identity is valid. With nothing decided, report the newest as before.
	// An equal creation time is a tie, never "newer": it keeps ci_tie.
	skip := 0
	for skip < len(matches) && undecided(matches[skip], revision) {
		skip++
	}
	if skip == len(matches) {
		skip = 0
	}
	for skip > 0 && matches[skip-1].GetCreationTimestamp().Time.Equal(matches[skip].GetCreationTimestamp().Time) {
		skip--
	}
	for _, item := range matches[:skip] {
		out.Undecided = append(out.Undecided, item.GetName())
	}
	chosen := matches[skip]
	selected := releaseResult(chosen, revision)
	out.Evidence = selected
	for _, item := range matches[skip+1:] {
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

// lastFull finds the newest bifrost-ci-platform green of any revision whose
// identity is valid as releaseResult checks it and that tested the whole API
// (api-scope full, or none: older runs ran everything). A run created after
// readAt is not trusted to be the last one.
func lastFull(items []unstructured.Unstructured, readAt time.Time) (string, string, time.Time) {
	var run, commit string
	var at time.Time
	for _, item := range items {
		params, counts := namedValues(item, "spec", "params")
		created := item.GetCreationTimestamp().Time
		if counts["revision"] != 1 || created.IsZero() || created.After(readAt) {
			continue
		}
		r := releaseResult(item, params["revision"])
		if r.Status != "Succeeded" || r.Reason != "" || !fullScope(r.Results) {
			continue
		}
		if run == "" || created.After(at) || created.Equal(at) && r.Run < run {
			run, commit, at = r.Run, r.PlatformCommit, created
		}
	}
	return run, commit, at
}

// undecided: the run's identity is valid and Tekton has not given a verdict.
// Succeeded=Unknown is non-terminal by definition (Running, Pending, finally
// tasks after a cancel); a run with no Succeeded condition has not started.
// A completion time, or more than one Succeeded condition, is never undecided.
func undecided(item unstructured.Unstructured, revision string) bool {
	if releaseResult(item, revision).Reason != "" {
		return false
	}
	if _, done, _ := unstructured.NestedFieldNoCopy(item.Object, "status", "completionTime"); done {
		return false
	}
	conds, _, _ := unstructured.NestedSlice(item.Object, "status", "conditions")
	n, open := 0, false
	for _, cond := range conds {
		m, _ := cond.(map[string]any)
		if m["type"] != "Succeeded" {
			continue
		}
		n++
		open = m["status"] == "Unknown"
	}
	return n == 0 || n == 1 && open
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
	if scope, named := values[APIScopeResult]; named && scope != APIScopeFull && scope != APIScopeConsole && scope != APIScopeIncremental {
		out.Status, out.Reason = "Unknown", "ci_results"
		return out
	}
	out.PlatformCommit, out.UICommit, out.InfraCommit = values["platform-commit"], values["ui-commit"], values["infra-commit"]
	return out
}
