package server

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/runfacts"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// A CI run's results name what it tested: <short>-commit and <short>-tree a
// repository, source-* the run's own; other results are not inputs, and a
// delivery run has none.
// What a run failed on, from its failed tasks: test with a test named; build
// when a task that names its tests named none; nothing when no failed task
// names tests (the console's), or none is kept.
func TestRunFailure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		failed []delivery.FailedTask
		tasks  []string
		tests  []string
		class  string
	}{
		{"tests", []delivery.FailedTask{{Task: "test-api", NamesTests: true, Tests: []string{"b.TestB", "a.TestA"}}, {Task: "test-api-store", NamesTests: true, Tests: []string{"a.TestA"}}},
			[]string{"test-api", "test-api-store"}, []string{"a.TestA", "b.TestB"}, "test"},
		{"did not compile", []delivery.FailedTask{{Task: "test-api", NamesTests: true}}, []string{"test-api"}, nil, "build"},
		{"console", []delivery.FailedTask{{Task: "test-console"}}, []string{"test-console"}, nil, ""},
		{"a test and the console", []delivery.FailedTask{{Task: "test-api", NamesTests: true, Tests: []string{"a.TestA"}}, {Task: "test-console"}},
			[]string{"test-api", "test-console"}, []string{"a.TestA"}, "test"},
		{"none kept", nil, nil, nil, ""},
	} {
		tasks, tests, class := runFailure(tc.failed)
		if !reflect.DeepEqual(tasks, tc.tasks) || !reflect.DeepEqual(tests, tc.tests) || class != tc.class {
			t.Errorf("%s: %v %v %q", tc.name, tasks, tests, class)
		}
	}
}

// A run that has the api-scope result and says nothing valid in it is not a
// full run. 2026-11-13, independent review of the A1 fix, G1: the value was
// read as "" and an empty scope means full (a pipeline with no such result
// tests everything), so a platform run whose scope came out empty would have
// been recorded as a full pass of the whole repository. No such run was in
// the cluster that day (137 succeeded platform runs, all with a valid scope).
// Origin: 2026-11-16. The base an incremental CI run was judged against
// (base-commit, written by test-api since infra 002bb598) is what its pass
// rests on; the ledger's verdict follows it (ledgerCIScoped). The word none,
// an empty value and a result of another kind of run name no base.
func TestAPipelineRunWithABadScopeIsNotAFullRun(t *testing.T) {
	t.Parallel()
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	fact := func(results ...any) seenfacts.PipelineRun {
		u := unstructured.Unstructured{Object: map[string]any{
			"spec":   map[string]any{"pipelineRef": map[string]any{"name": "bifrost-ci-platform"}},
			"status": map[string]any{"completionTime": "2026-11-13T00:00:00Z", "conditions": []any{map[string]any{"type": "Succeeded", "status": "True", "reason": "Succeeded"}}, "results": results},
		}}
		u.SetName("r")
		u.SetUID("uid-r")
		u.SetLabels(map[string]string{"bifrost.io/repo": "bifrost-platform"})
		f, ok := runfacts.PipelineRun(u)
		if !ok {
			t.Fatal("not a fact")
		}
		return f
	}
	own := []any{map[string]any{"name": "platform-commit", "value": a}, map[string]any{"name": "platform-tree", "value": b}}
	for name, tc := range map[string]struct {
		scope    []any
		evidence bool
	}{
		"no such result (a pipeline without scopes)": {nil, true},
		"full":                     {[]any{map[string]any{"name": "api-scope", "value": "full"}}, true},
		"an empty value":           {[]any{map[string]any{"name": "api-scope", "value": ""}}, false},
		"a value that is no scope": {[]any{map[string]any{"name": "api-scope", "value": "everything"}}, false},
		"not a string":             {[]any{map[string]any{"name": "api-scope", "value": []any{"full"}}}, false},
		"named twice":              {[]any{map[string]any{"name": "api-scope", "value": "full"}, map[string]any{"name": "api-scope", "value": "console"}}, false},
	} {
		_, is, err := evidence.RunEvidence(controller.EvidenceRun(fact(append(append([]any{}, own...), tc.scope...)...)))
		if err != nil || is != tc.evidence {
			t.Errorf("%s: evidence %v, %v; want %v", name, is, err, tc.evidence)
		}
	}
}

func TestPipelineRunFactReadsTheBaseOfAnIncrementalRun(t *testing.T) {
	t.Parallel()
	base := strings.Repeat("b", 40)
	fact := func(pipeline, value string) seenfacts.PipelineRun {
		u := unstructured.Unstructured{Object: map[string]any{
			"spec": map[string]any{"pipelineRef": map[string]any{"name": pipeline}},
			"status": map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": "True", "reason": "Succeeded"}},
				"results": []any{map[string]any{"name": "api-scope", "value": "incremental"}, map[string]any{"name": "base-commit", "value": value}}},
		}}
		u.SetName("r")
		u.SetLabels(map[string]string{"bifrost.io/repo": "bifrost-platform"})
		f, ok := runfacts.PipelineRun(u)
		if !ok {
			t.Fatalf("%s %q: not a fact", pipeline, value)
		}
		return f
	}
	if got := fact("bifrost-ci-platform", base).Base; got != base {
		t.Fatalf("base = %q", got)
	}
	for _, value := range []string{"none", "", "main", base[:12]} {
		if got := fact("bifrost-ci-platform", value).Base; got != "" {
			t.Errorf("base-commit %q read as %q", value, got)
		}
	}
	if got := fact("bifrost-deliver-platform", base).Base; got != "" {
		t.Errorf("a delivery run's base-commit read as %q", got)
	}
	// It reaches the row: the statement of a passed incremental row names it.
	p := fact("bifrost-ci-platform", base)
	p.UID, p.CompletedAt = "uid", time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	p.Inputs = []seenfacts.RepoAt{{Repo: "bifrost-platform", Commit: strings.Repeat("a", 40), Tree: strings.Repeat("c", 40)}}
	row, is, err := evidence.RunEvidence(controller.EvidenceRun(p))
	if err != nil || !is || evidence.IncrementalBase(row) != base {
		t.Fatalf("row base = %q (is %v, %v)", evidence.IncrementalBase(row), is, err)
	}
}
