// Package landingci owns landing CI evidence and idempotent CI creation. It
// does not import delivery: the server supplies only Kubernetes transports.
package landingci

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var pipelineRunGVR = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}
var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
var landingCIName = regexp.MustCompile(`^landing-ci-[0-9a-f]{24}-[12]$`)

// Client's factories are lazy; construction reads neither credentials nor K8s.
type Client struct {
	Namespace string
	Dynamic   func() (dynamic.Interface, error)
	ConfigMap func(context.Context, string) (map[string]string, bool, error)
}

func (c *Client) namespace() string {
	if c.Namespace != "" {
		return c.Namespace
	}
	return "cicd"
}
func (c *Client) resource() (dynamic.ResourceInterface, error) {
	if c.Dynamic == nil {
		return nil, fmt.Errorf("landing CI transport unavailable")
	}
	d, err := c.Dynamic()
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, fmt.Errorf("landing CI transport unavailable")
	}
	return d.Resource(pipelineRunGVR).Namespace(c.namespace()), nil
}

// Key pins a prepared candidate and its sibling checkouts. Siblings is keyed
// by repository name, not result name; Base remains candidate provenance for
// platform (its pipeline does not have a base parameter).
type Key struct {
	Repo, Revision, Base string
	Siblings             map[string]string
	Generation           int
}

// Repository describes the pipeline's result and sibling parameter names.
type Repository struct {
	Pipeline, CandidateResult     string
	SiblingResults, SiblingParams map[string]string
}

func repository(repo string) (Repository, bool) {
	r, ok := landing.LandingRepos[repo]
	if !ok {
		return Repository{}, false
	}
	if repo == "bifrost-trade-infra" {
		return Repository{Pipeline: r.CIPipeline, CandidateResult: r.CIResults["revision"],
			SiblingResults: map[string]string{"bifrost-platform": r.CIResults["platform"]},
			SiblingParams:  map[string]string{"bifrost-platform": "platformRevision"}}, true
	}
	return Repository{Pipeline: r.CIPipeline, CandidateResult: r.CIResults["revision"],
		SiblingResults: map[string]string{"bifrost-ui": r.CIResults["ui"], "bifrost-trade-infra": r.CIResults["infra"]},
		SiblingParams:  map[string]string{"bifrost-ui": "uiRevision", "bifrost-trade-infra": "infraRevision"}}, true
}
func (k Key) validate() (Repository, error) {
	r, ok := repository(k.Repo)
	if !ok || !fullSHA.MatchString(k.Revision) || !fullSHA.MatchString(k.Base) || k.Generation < 0 || len(k.Siblings) != len(r.SiblingResults) {
		return r, fmt.Errorf("invalid landing CI key")
	}
	for repo := range r.SiblingResults {
		if !fullSHA.MatchString(k.Siblings[repo]) {
			return r, fmt.Errorf("invalid landing CI sibling %s", repo)
		}
	}
	if k.Repo == "bifrost-trade-infra" && k.Generation < 1 {
		return r, fmt.Errorf("invalid landing CI generation")
	}
	return r, nil
}

type Evidence struct {
	landing.CIRun
	InfraCommit string
	Results     map[string]string
	Reason      string
}

// Read selects before validating results. S6 §6.2's ordering retains runs
// without status/results, including running and failed runs, and never falls
// back to an older success. LatestCI also preserves its conflicting-time tie
// rule. platform keeps its existing revision-based candidate selection.
func (c *Client) Read(ctx context.Context, key Key, after time.Time) (out Evidence, err error) {
	repo, err := key.validate()
	if err != nil {
		return Evidence{}, err
	}
	defer func() {
		if key.Repo == "bifrost-trade-infra" {
			out.CIRun.Results = map[string]string{}
			for _, name := range landing.LandingRepos[key.Repo].CIResults {
				if value, ok := out.Results[name]; ok {
					out.CIRun.Results[name] = value
				}
			}
		}
	}()
	latest, item, err := c.latest(ctx, repo.Pipeline, key.Revision, after, func(item unstructured.Unstructured) (bool, bool) {
		if key.Repo == "bifrost-trade-infra" {
			match := infraCandidate(item, key)
			return match, match
		}
		trusted := strictCandidate(item, key.Repo, key.Revision)
		return trusted, trusted
	})
	out = Evidence{CIRun: latest}
	if err != nil {
		return out, err
	}
	out.Results, _ = namedValues(item, "status", "results")
	out.InfraCommit = out.Results["infra-commit"]
	if latest.Status != "Succeeded" {
		return out, nil
	}
	if key.Repo == "bifrost-trade-infra" && latest.UID == "" {
		out.Status, out.Reason = "Unknown", "ci_identity"
		return out, nil
	}
	ref, _, _ := unstructured.NestedMap(item.Object, "spec", "pipelineRef")
	if !reflect.DeepEqual(ref, map[string]any{"name": repo.Pipeline}) {
		out.Status, out.Reason = "Unknown", "ci_pipeline_mismatch"
		return out, nil
	}
	out.Results, out.Reason = checkedResults(item, repo, key)
	out.PlatformCommit, out.UICommit, out.InfraCommit = out.Results["platform-commit"], out.Results["ui-commit"], out.Results["infra-commit"]
	switch out.Reason {
	case "ci_result_missing":
		out.Status = "Unknown"
	case "ci_result_mismatch":
		out.Status = "Failed"
	}
	return out, nil
}

func checkedResults(item unstructured.Unstructured, repo Repository, key Key) (map[string]string, string) {
	values, counts := namedValues(item, "status", "results")
	expected := map[string]string{repo.CandidateResult: key.Revision}
	for sibling, result := range repo.SiblingResults {
		expected[result] = key.Siblings[sibling]
	}
	if key.Repo == "bifrost-trade-infra" {
		results := landing.LandingRepos[key.Repo].CIResults
		expected[results["base"]], expected[results["tools"]] = key.Base, key.Base
		expected[results["policy"]], expected[results["purpose"]] = "pass", "landing"
	}
	missing, mismatch := false, false
	for name, expectedValue := range expected {
		if counts[name] != 1 || values[name] == "" {
			missing = true
			continue
		}
		if values[name] != expectedValue {
			mismatch = true
		}
	}
	if missing {
		return values, "ci_result_missing"
	}
	if mismatch {
		return values, "ci_result_mismatch"
	}
	return values, ""
}

// Select by the full immutable parameter key, never by results or labels.
// Generation is part of that key even when two preparations have the same R.
func infraCandidate(item unstructured.Unstructured, key Key) bool {
	if _, inline, _ := unstructured.NestedFieldNoCopy(item.Object, "spec", "pipelineSpec"); inline {
		return false
	}
	values, counts := namedValues(item, "spec", "params")
	for name, want := range infraParams(key) {
		if counts[name] != 1 || values[name] != want {
			return false
		}
	}
	return true
}

func infraParams(key Key) map[string]string {
	return map[string]string{"revision": key.Revision, "base": key.Base,
		"platformRevision": key.Siblings["bifrost-platform"], "generation": strconv.Itoa(key.Generation), "purpose": "landing"}
}

// LandingCI is the existing v1 adapter. It preserves platform's prior status
// and optional result behavior until the prepared-key consumers are wired.
// Unlike Read, its candidate set must include every legacy pipeline/revision
// match. Conflicting identity stays in the ordering as Unknown, never deleted
// in a way that could revive an older success.
// New grant/STG evidence consumers must use Read, never this adapter.
func (c *Client) LandingCI(ctx context.Context, revision string, after time.Time) (landing.CIRun, error) {
	if !fullSHA.MatchString(revision) {
		return landing.CIRun{}, fmt.Errorf("invalid landing revision")
	}
	run, _, err := c.latest(ctx, "bifrost-ci-platform", revision, after, func(item unstructured.Unstructured) (bool, bool) {
		params, _, _ := unstructured.NestedSlice(item.Object, "spec", "params")
		for _, entry := range params {
			p, _ := entry.(map[string]any)
			if p["name"] == "revision" {
				// The baseline uses the first revision parameter. Preserve
				// that candidate even if a later duplicate contradicts it.
				return p["value"] == revision, strictCandidate(item, "bifrost-platform", revision)
			}
		}
		return false, false
	})
	return run, err
}

// strictCandidate is the repository-aware Read filter, not the legacy
// candidate predicate. Legacy only uses it to downgrade conflicting evidence.
func strictCandidate(item unstructured.Unstructured, repo, revision string) bool {
	if _, inline, _ := unstructured.NestedFieldNoCopy(item.Object, "spec", "pipelineSpec"); inline {
		return false
	}
	params, counts := namedValues(item, "spec", "params")
	label := item.GetLabels()["bifrost.io/repo"]
	return counts["revision"] == 1 && params["revision"] == revision && (label == "" || label == repo)
}

func (c *Client) latest(ctx context.Context, pipeline, revision string, after time.Time, candidate func(unstructured.Unstructured) (include, trusted bool)) (landing.CIRun, unstructured.Unstructured, error) {
	all, err := c.pipelineRuns(ctx, pipeline)
	if err != nil {
		return landing.CIRun{}, unstructured.Unstructured{}, err
	}
	var runs []landing.CIRun
	items := map[string]unstructured.Unstructured{}
	for _, item := range all {
		include, trusted := candidate(item)
		if !include {
			continue
		}
		run := landing.CIRun{Run: item.GetName(), UID: string(item.GetUID()), R: revision, Status: runStatus(item), CreatedAt: item.GetCreationTimestamp().Time}
		landingName := landingCIName.MatchString(run.Run)
		if pipeline == landing.LandingRepos["bifrost-trade-infra"].CIPipeline {
			landingName = landing.LandingRepos["bifrost-trade-infra"].CINamePattern.MatchString(run.Run)
		}
		if !trusted || landingName && item.GetLabels()["bifrost.io/trigger"] != "agentrt-landing-drill" {
			run.Status = "Unknown"
		}
		run.PlatformCommit, run.UICommit, run.InfraCommit = landingCIResults(item)
		runs = append(runs, run)
		items[run.Run] = item
	}
	latest := landing.LatestCI(runs, revision, after)
	return latest, items[latest.Run], nil
}

// pipelineRuns is shared by pinned landing reads, legacy reads and release
// selection. Never truncate the list: a newer red run may be on the last page.
func (c *Client) pipelineRuns(ctx context.Context, pipeline string) ([]unstructured.Unstructured, error) {
	res, err := c.resource()
	if err != nil {
		return nil, err
	}
	var items []unstructured.Unstructured
	seenPages := map[string]bool{}
	continuation := ""
	for {
		list, err := res.List(ctx, metav1.ListOptions{Limit: 500, Continue: continuation})
		if err != nil {
			return nil, err
		}
		for _, item := range list.Items {
			name, _, _ := unstructured.NestedString(item.Object, "spec", "pipelineRef", "name")
			if name != pipeline {
				continue
			}
			items = append(items, item)
		}
		continuation = list.GetContinue()
		if continuation == "" {
			break
		}
		if seenPages[continuation] {
			return nil, fmt.Errorf("landing CI pagination repeated")
		}
		seenPages[continuation] = true
	}
	return items, nil
}
func runStatus(item unstructured.Unstructured) string {
	conds, _, _ := unstructured.NestedSlice(item.Object, "status", "conditions")
	status, count := "Unknown", 0
	for _, cond := range conds {
		m, _ := cond.(map[string]any)
		if m["type"] != "Succeeded" {
			continue
		}
		count++
		switch m["status"] {
		case "True":
			status = "Succeeded"
		case "False":
			status = "Failed"
		case "Unknown":
			if m["reason"] == "Running" || m["reason"] == "Started" || m["reason"] == "Pending" {
				status = "Running"
			}
		}
	}
	if count != 1 {
		return "Unknown"
	}
	return status
}

// namedValues keeps counts so duplicates can never masquerade as one result.
func namedValues(item unstructured.Unstructured, fields ...string) (map[string]string, map[string]int) {
	list, _, _ := unstructured.NestedSlice(item.Object, fields...)
	values, counts := map[string]string{}, map[string]int{}
	for _, entry := range list {
		m, _ := entry.(map[string]any)
		name, _ := m["name"].(string)
		value, _ := m["value"].(string)
		counts[name]++
		values[name] = value
	}
	return values, counts
}
func landingCIResults(item unstructured.Unstructured) (platform, ui, infra string) {
	values, counts := namedValues(item, "status", "results")
	names := landing.LandingRepos["bifrost-platform"].CIResults
	if counts[names["infra"]] == 1 && fullSHA.MatchString(values[names["infra"]]) {
		infra = values[names["infra"]]
	}
	if counts[names["revision"]] == 1 && fullSHA.MatchString(values[names["revision"]]) {
		platform = values[names["revision"]]
	}
	if counts[names["ui"]] == 1 && fullSHA.MatchString(values[names["ui"]]) {
		ui = values[names["ui"]]
	}
	return
}
