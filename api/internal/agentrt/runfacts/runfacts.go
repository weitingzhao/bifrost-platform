// Package runfacts reads a finished Tekton PipelineRun as the facts the agent
// runtime records of it: what ran, how it ended, and, for a CI run, which
// commits and trees it tested.
package runfacts

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

var commitID = regexp.MustCompile(`^[0-9a-f]{40}$`)

var pipelineReason = regexp.MustCompile("^[A-Za-z]{1,64}$")

// PipelineRun is what the runtime records of a finished Tekton PipelineRun, and
// false for one that has not finished or whose result is not a clean verdict.
func PipelineRun(run unstructured.Unstructured) (seenfacts.PipelineRun, bool) {
	f := seenfacts.PipelineRun{Run: run.GetName(), UID: string(run.GetUID()), Kind: "other", CreatedAt: run.GetCreationTimestamp().Time}
	f.Pipeline, _, _ = unstructured.NestedString(run.Object, "spec", "pipelineRef", "name")
	switch {
	case strings.HasPrefix(f.Pipeline, "bifrost-ci-"):
		f.Kind = "ci"
	case strings.HasPrefix(f.Pipeline, "bifrost-deliver-"):
		f.Kind = "delivery"
	}
	conditions, _, _ := unstructured.NestedSlice(run.Object, "status", "conditions")
	for _, value := range conditions {
		c, ok := value.(map[string]any)
		if !ok || c["type"] != "Succeeded" {
			continue
		}
		switch c["status"] {
		case "True":
			f.Result = "Succeeded"
		case "False":
			f.Result = "Failed"
		default:
			return seenfacts.PipelineRun{}, false
		}
		reason, _ := c["reason"].(string)
		if pipelineReason.MatchString(reason) {
			f.Reason = reason
		}
		break
	}
	if f.Result == "" {
		return seenfacts.PipelineRun{}, false
	}
	readTime := func(key string) time.Time {
		value, _, _ := unstructured.NestedString(run.Object, "status", key)
		at, _ := time.Parse(time.RFC3339, value)
		return at
	}
	f.StartedAt, f.CompletedAt = readTime("startTime"), readTime("completionTime")
	if f.Result == "Failed" && f.StartedAt.IsZero() {
		f.StartedAt = f.CreatedAt
	}
	f.Repo = run.GetLabels()["bifrost.io/repo"]
	f.Audit = run.GetLabels()["bifrost.io/trigger"] == landingci.FullCITrigger
	if f.Repo == "" {
		f.Repo = map[string]string{
			"bifrost-deliver-platform":      "bifrost-platform",
			"bifrost-deliver-platform-prod": "bifrost-platform",
		}[f.Pipeline]
	}
	params, _, _ := unstructured.NestedSlice(run.Object, "spec", "params")
	for _, value := range params {
		p, ok := value.(map[string]any)
		if !ok {
			continue
		}
		v, _ := p["value"].(string)
		switch p["name"] {
		case "revision":
			f.Revision = v
		}
	}
	results, _, _ := unstructured.NestedSlice(run.Object, "status", "results")
	inputs, own := map[string]*seenfacts.RepoAt{}, map[string]*seenfacts.RepoAt{}
	scopeSeen := false
	for _, value := range results {
		r, _ := value.(map[string]any)
		v, _ := r["value"].(string)
		name, _ := r["name"].(string)
		if f.Pipeline == "bifrost-land" {
			switch name {
			case "commit":
				f.LandedCommit = notice.Redact(v)
			case "change-ids":
				f.ChangeIDs = notice.Redact(v)
			case "outcome":
				f.Outcome = notice.Redact(v)
			}
		}
		switch name {
		case "platform-tree":
			// The tree of the run's own repository: bifrost-ci-infra reports
			// the platform checkout it used under the same name.
			if f.Pipeline != "bifrost-ci-infra" {
				f.Tree = v
			}
		case "tested-by":
			f.TestedBy = v
		case "base-commit":
			// The word none (no base) and anything that is not a commit id
			// name no base.
			if f.Kind == "ci" && commitID.MatchString(v) && len(v) == 40 {
				f.Base = v
			}
		case "api-scope":
			// A pipeline that has no such result tests everything (the
			// scope stays ""). One that has it must say a scope: an empty
			// value, a value that is not a string, or the result named
			// twice is not "everything" but a run nothing can be said of.
			if _, isString := r["value"].(string); !isString || v == "" || scopeSeen {
				f.Scope = evidence.EvidenceScopeUnknown
			} else {
				f.Scope = v
			}
			scopeSeen = true
		}
		// What a CI run tested, for the evidence ledger: <short>-commit and
		// <short>-tree name a repository it used (ciResultRepos), source-* (or
		// repo-*) the repository of the run itself, which wins when both name it.
		short, field, _ := strings.Cut(name, "-")
		repo, into := ciResultRepos[short], inputs
		if short == "source" || short == "repo" {
			repo, into = f.Repo, own
		}
		if f.Kind != "ci" || repo == "" || !commitID.MatchString(v) || (field != "commit" && field != "tree") {
			continue
		}
		if into[repo] == nil {
			into[repo] = &seenfacts.RepoAt{Repo: repo}
		}
		if field == "commit" {
			into[repo].Commit = v
		} else {
			into[repo].Tree = v
		}
	}
	for repo, in := range own {
		inputs[repo] = in
	}
	for _, in := range inputs {
		f.Inputs = append(f.Inputs, *in)
	}
	sort.Slice(f.Inputs, func(i, j int) bool { return f.Inputs[i].Repo < f.Inputs[j].Repo })
	return f, true
}

// ciResultRepos is the repository a CI pipeline result names by its prefix
// (ui-commit, infra-tree); a result with another prefix is not an input.
var ciResultRepos = map[string]string{
	"platform": "bifrost-platform", "ui": "bifrost-ui", "infra": "bifrost-trade-infra",
	"core": "bifrost-trade-core", "api": "bifrost-trade-api", "worker": "bifrost-trade-worker",
	"frontend": "bifrost-trade-frontend", "research": "bifrost-research",
}
