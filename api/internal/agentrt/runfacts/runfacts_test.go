package runfacts

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
)

func headRun(name, pipeline string, created time.Time, condition string, params map[string]string) unstructured.Unstructured {
	var ps []any
	for k, v := range params {
		ps = append(ps, map[string]any{"name": k, "value": v})
	}
	u := unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": name},
		"spec":     map[string]any{"pipelineRef": map[string]any{"name": pipeline}, "params": ps},
	}}
	u.SetCreationTimestamp(metav1.NewTime(created))
	if condition != "" {
		u.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": condition}}}
	}
	return u
}

func TestPipelineRunFact(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, pipeline, status, repo, url, wantRepo, kind string
		start                                             bool
	}{
		{"landing-ci-1", "bifrost-ci-platform", "True", "platform", "https://example/ignored.git", "platform", "ci", true},
		{"delivery-1", "bifrost-deliver-platform", "False", "", "https://example/platform.git", "bifrost-platform", "delivery", false},
		{"delivery-prod", "bifrost-deliver-platform-prod", "True", "", "", "bifrost-platform", "delivery", true},
		{"delivery-labelled", "bifrost-deliver-platform-prod", "True", "custom", "", "custom", "delivery", true},
		{"delivery-other", "bifrost-deliver-other", "True", "", "https://example/platform.git", "", "delivery", true},
		{"other-1", "other", "True", "", "", "", "other", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{"pipelineRef": map[string]any{"name": tc.pipeline}, "params": []any{
					map[string]any{"name": "repo-url", "value": tc.url},
					map[string]any{"name": "revision", "value": commitForExternalRecord},
					map[string]any{"name": "secret", "value": "private"},
				}},
				"status": map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": tc.status, "reason": "two words", "message": "private"}}, "completionTime": at.Add(time.Minute).Format(time.RFC3339)},
			}}
			run.SetName(tc.name)
			run.SetUID(types.UID("uid"))
			run.SetCreationTimestamp(metav1.NewTime(at))
			run.SetLabels(map[string]string{"bifrost.io/repo": tc.repo})
			if tc.start {
				_ = unstructured.SetNestedField(run.Object, at.Format(time.RFC3339), "status", "startTime")
			}
			f, ok := PipelineRun(run)
			result := "Succeeded"
			if tc.status == "False" {
				result = "Failed"
			}
			if !ok || f.Result != result || f.Kind != tc.kind || f.Repo != tc.wantRepo || f.Revision != commitForExternalRecord || !f.StartedAt.Equal(at) || f.Reason != "" {
				t.Fatalf("fact: %+v %v", f, ok)
			}
			if _, exists := f.Body()["tree"]; exists {
				t.Fatal("legacy tree present")
			}
			if _, exists := f.Body()["tested_by"]; exists {
				t.Fatal("legacy tested_by present")
			}
			_ = unstructured.SetNestedSlice(run.Object, []any{map[string]any{"name": "platform-tree", "value": commitForExternalRecord}, map[string]any{"name": "tested-by", "value": "ci-original"}}, "status", "results")
			withResults, yes := PipelineRun(run)
			if !yes || withResults.Body()["tree"] != commitForExternalRecord || withResults.Body()["tested_by"] != "ci-original" {
				t.Fatalf("results: %+v", withResults)
			}
			raw, _ := json.Marshal(f.Body())
			if strings.Contains(string(raw), "private") {
				t.Fatal("private payload retained")
			}
			for _, reason := range []string{"Completed", strings.Repeat("A", 65)} {
				_ = unstructured.SetNestedField(run.Object, []any{map[string]any{"type": "Succeeded", "status": tc.status, "reason": reason}}, "status", "conditions")
				got, ok := PipelineRun(run)
				want := reason
				if len(reason) > 64 {
					want = ""
				}
				if !ok || got.Reason != want {
					t.Fatalf("reason: %+v %v", got, ok)
				}
			}
			_ = unstructured.SetNestedField(run.Object, []any{map[string]any{"type": "Succeeded", "status": "Unknown"}}, "status", "conditions")
			if _, ok := PipelineRun(run); ok {
				t.Fatal("unfinished run recorded")
			}
		})
	}
}

func TestLandResultsOptionalAndScoped(t *testing.T) {
	for _, pipeline := range []string{"bifrost-land", "bifrost-ci-platform"} {
		run := headRun("land-one", pipeline, time.Now().UTC(), "True", map[string]string{"revision": commitForExternalRecord})
		run.SetUID("uid-one")
		_ = unstructured.SetNestedField(run.Object, time.Now().UTC().Format(time.RFC3339), "status", "startTime")
		_ = unstructured.SetNestedField(run.Object, time.Now().UTC().Format(time.RFC3339), "status", "completionTime")
		old, ok := PipelineRun(run)
		if !ok || old.LandedCommit != "" || seenfacts.CheckExternal("pipeline_seen", old.Body()) != nil {
			t.Fatalf("legacy: %+v", old)
		}
		_ = unstructured.SetNestedSlice(run.Object, []any{map[string]any{"name": "commit", "value": commitForExternalRecord}, map[string]any{"name": "change-ids", "value": "Ione Itwo"}, map[string]any{"name": "outcome", "value": "landed"}}, "status", "results")
		got, ok := PipelineRun(run)
		if !ok || seenfacts.CheckExternal("pipeline_seen", got.Body()) != nil {
			t.Fatalf("new: %+v", got)
		}
		if pipeline == "bifrost-land" {
			if got.LandedCommit != commitForExternalRecord || got.ChangeIDs != "Ione Itwo" || got.Outcome != "landed" {
				t.Fatal(got)
			}
		} else if got.LandedCommit != "" || got.ChangeIDs != "" || got.Outcome != "" {
			t.Fatal(got)
		}
	}
}

// Origin: 2026-11-21. Land refusals lived only in the land TaskRun, kept one
// day (7 days: 107 refused, 61 of them with a green CI), so nobody could count
// why. A refused bifrost-land run now carries its refusal as a run result
// (infra pipeline-land.yaml's finally task) and pipeline_seen records it.
func TestARefusedLandRecordsItsRefusal(t *testing.T) {
	const why = "refused: the branch is behind main and Gitea could not rebase it (409 rebase failed because of conflict). Rebase it onto main and push it to a ready/ branch again"
	for _, pipeline := range []string{"bifrost-land", "bifrost-ci-platform"} {
		run := headRun("land-two", pipeline, time.Now().UTC(), "False", map[string]string{"revision": commitForExternalRecord})
		run.SetUID("uid-two")
		_ = unstructured.SetNestedField(run.Object, time.Now().UTC().Format(time.RFC3339), "status", "startTime")
		_ = unstructured.SetNestedField(run.Object, time.Now().UTC().Format(time.RFC3339), "status", "completionTime")
		_ = unstructured.SetNestedSlice(run.Object, []any{map[string]any{"name": "refusal", "value": why}}, "status", "results")
		got, ok := PipelineRun(run)
		if !ok || got.Result != "Failed" {
			t.Fatalf("%s: %+v", pipeline, got)
		}
		b := got.Body()
		if err := seenfacts.CheckExternal("pipeline_seen", b); err != nil {
			t.Fatalf("%s: %v", pipeline, err)
		}
		if want := map[bool]string{true: why}[pipeline == "bifrost-land"]; got.Refusal != want || (want == "") != (b["refusal"] == nil) {
			t.Fatalf("%s: refusal %q, body %v", pipeline, got.Refusal, b["refusal"])
		}
	}
}

const commitForExternalRecord = "2333e00753f91c74c8b2f4d693a803f8e89e7892"

func TestPipelineRunFactReadsWhatACIRunTested(t *testing.T) {
	t.Parallel()
	a, b, c, d := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), strings.Repeat("d", 40)
	run := func(pipeline, repo string, results map[string]string) seenfacts.PipelineRun {
		var list []any
		for k, v := range results {
			list = append(list, map[string]any{"name": k, "value": v})
		}
		u := unstructured.Unstructured{Object: map[string]any{
			"spec":   map[string]any{"pipelineRef": map[string]any{"name": pipeline}},
			"status": map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": "True", "reason": "Succeeded"}}, "results": list},
		}}
		u.SetName("r")
		u.SetLabels(map[string]string{"bifrost.io/repo": repo})
		f, ok := PipelineRun(u)
		if !ok {
			t.Fatal("not a fact")
		}
		return f
	}
	f := run("bifrost-ci-platform", "bifrost-platform", map[string]string{"platform-commit": a, "platform-tree": b, "ui-commit": c, "infra-commit": d,
		"api-scope": "incremental", "base-commit": a, "tested-by": "r", "failed-tests": ""})
	want := []seenfacts.RepoAt{{Repo: "bifrost-platform", Commit: a, Tree: b}, {Repo: "bifrost-trade-infra", Commit: d}, {Repo: "bifrost-ui", Commit: c}}
	if !reflect.DeepEqual(f.Inputs, want) || f.Scope != "incremental" || f.Tree != b {
		t.Fatalf("platform run: %+v scope %q", f.Inputs, f.Scope)
	}
	// A core run of the Python pipeline names core twice: the run's own wins.
	f = run("bifrost-ci-python", "bifrost-trade-core", map[string]string{"repo-commit": a, "repo-tree": b, "core-commit": c, "core-tree": d,
		"infra-commit": c, "infra-tree": d, "ui-commit": "main"})
	want = []seenfacts.RepoAt{{Repo: "bifrost-trade-core", Commit: a, Tree: b}, {Repo: "bifrost-trade-infra", Commit: c, Tree: d}}
	if !reflect.DeepEqual(f.Inputs, want) || f.Scope != "" {
		t.Fatalf("python run: %+v scope %q", f.Inputs, f.Scope)
	}
	f = run("bifrost-ci-python", "bifrost-research", map[string]string{"source-commit": a, "source-tree": b, "infra-commit": c, "infra-tree": d})
	want = []seenfacts.RepoAt{{Repo: "bifrost-research", Commit: a, Tree: b}, {Repo: "bifrost-trade-infra", Commit: c, Tree: d}}
	if !reflect.DeepEqual(f.Inputs, want) {
		t.Fatalf("research run: %+v", f.Inputs)
	}
	// An infra run reports the platform checkout it used as platform-tree:
	// an input, and not the tree of the run's repository.
	f = run("bifrost-ci-infra", "bifrost-trade-infra", map[string]string{"infra-commit": a, "infra-tree": b, "platform-commit": c, "platform-tree": d})
	want = []seenfacts.RepoAt{{Repo: "bifrost-platform", Commit: c, Tree: d}, {Repo: "bifrost-trade-infra", Commit: a, Tree: b}}
	if !reflect.DeepEqual(f.Inputs, want) || f.Tree != "" {
		t.Fatalf("infra run: %+v tree %q", f.Inputs, f.Tree)
	}
	if f = run("bifrost-deliver-platform", "", map[string]string{"agentd-commit": a, "platform-commit": a}); len(f.Inputs) != 0 {
		t.Fatalf("delivery run has inputs: %+v", f.Inputs)
	}
}
