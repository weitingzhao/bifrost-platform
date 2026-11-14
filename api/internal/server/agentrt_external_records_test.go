package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

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
			f, ok := pipelineRunFact(run)
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
			withResults, yes := pipelineRunFact(run)
			if !yes || withResults.Body()["tree"] != commitForExternalRecord || withResults.Body()["tested_by"] != "ci-original" {
				t.Fatalf("results: %+v", withResults)
			}
			raw, _ := json.Marshal(f.Body())
			if strings.Contains(string(raw), "private") {
				t.Fatal("private payload retained")
			}
			for _, reason := range []string{"Completed", strings.Repeat("A", 65)} {
				_ = unstructured.SetNestedField(run.Object, []any{map[string]any{"type": "Succeeded", "status": tc.status, "reason": reason}}, "status", "conditions")
				got, ok := pipelineRunFact(run)
				want := reason
				if len(reason) > 64 {
					want = ""
				}
				if !ok || got.Reason != want {
					t.Fatalf("reason: %+v %v", got, ok)
				}
			}
			_ = unstructured.SetNestedField(run.Object, []any{map[string]any{"type": "Succeeded", "status": "Unknown"}}, "status", "conditions")
			if _, ok := pipelineRunFact(run); ok {
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
		old, ok := pipelineRunFact(run)
		if !ok || old.LandedCommit != "" || store.CheckExternal("pipeline_seen", old.Body()) != nil {
			t.Fatalf("legacy: %+v", old)
		}
		_ = unstructured.SetNestedSlice(run.Object, []any{map[string]any{"name": "commit", "value": commitForExternalRecord}, map[string]any{"name": "change-ids", "value": "Ione Itwo"}, map[string]any{"name": "outcome", "value": "landed"}}, "status", "results")
		got, ok := pipelineRunFact(run)
		if !ok || store.CheckExternal("pipeline_seen", got.Body()) != nil {
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

const commitForExternalRecord = "2333e00753f91c74c8b2f4d693a803f8e89e7892"

func TestExternalRecordQuestionObservationNeverReopensExpired(t *testing.T) {
	t.Parallel()
	rt := newQuestionRuntime(filepath.Join(t.TempDir(), "cards"), nil)
	raw := []byte(`{"idempotency_key":"0123456789abcdef","title":"Choose","urgency":"Now","meanwhile":"Wait"}`)
	res := rt.svc.Create(context.Background(), "thread-a", "reporter", "reporter", raw)
	if res.Code >= 300 {
		t.Fatalf("create: %+v", res)
	}
	items, err := rt.svc.Observations()
	if err != nil || len(items) != 1 {
		t.Fatalf("observations: %+v %v", items, err)
	}
	q := items[0]
	now := q.ExpiresAt.Add(time.Second)
	q.Status = "expired"
	terminal, ok := questionObservation(q, now)
	if !ok || terminal.Status != "expired" {
		t.Fatalf("terminal: %+v %v", terminal, ok)
	}
	q.Status = "open"
	if _, ok := questionObservation(q, now); ok {
		t.Fatal("stale open card could reverse terminal observation")
	}
	q.Status = "withdrawn"
	if f, ok := questionObservation(q, now); !ok || f.Status != "withdrawn" {
		t.Fatal("closed card lost")
	}
}

func TestExternalRecordApprovalObservationNeverReopensExpired(t *testing.T) {
	t.Parallel()
	at := time.Now().Add(-time.Hour)
	a := approvals.Approval{Number: 1, Status: approvals.StatusExpired, ExpiresAt: at}
	if _, ok := approvalObservation(a, time.Now()); !ok {
		t.Fatal("terminal card lost")
	}
	a.Status = approvals.StatusPending
	if _, ok := approvalObservation(a, time.Now()); ok {
		t.Fatal("expired pending card recorded")
	}
}

func TestPipelineRecordsDurationFromEnv(t *testing.T) {
	for _, name := range []string{pipelineRecordsEveryEnv, pipelineRecordsFullEveryEnv} {
		for value, want := range map[string]time.Duration{
			"":      0, // the controller's default
			"15s":   15 * time.Second,
			" 1m ":  time.Minute,
			"5s":    5 * time.Second,
			"4s":    0, // under the floor: the default holds
			"-1m":   0,
			"30":    0, // not a duration
			"often": 0,
		} {
			t.Setenv(name, value)
			if got := pipelineRecordsDuration(name); got != want {
				t.Errorf("%s=%q: %s, want %s", name, value, got, want)
			}
		}
	}
	if controller.DefaultPipelineEvery < pipelineRecordsEveryMin || controller.DefaultPipelineFullEvery < controller.DefaultPipelineEvery {
		t.Fatalf("defaults: every %s, full every %s, floor %s", controller.DefaultPipelineEvery, controller.DefaultPipelineFullEvery, pipelineRecordsEveryMin)
	}
}

// The controller is given the intervals and, when the release recorder
// exists, its Kick as what a newly recorded run starts.
func TestWirePipelineRecords(t *testing.T) {
	t.Setenv(pipelineRecordsEveryEnv, "20s")
	t.Setenv(pipelineRecordsFullEveryEnv, "10m")
	c := &controller.Controller{}
	(&Server{}).wirePipelineRecords(c)
	if c.PipelineEvery != 20*time.Second || c.PipelineFullEvery != 10*time.Minute || c.PipelineRecorded != nil {
		t.Fatalf("without a recorder: every %s, full every %s, hook set %v", c.PipelineEvery, c.PipelineFullEvery, c.PipelineRecorded != nil)
	}
	s := &Server{releases: releases.NewHandler(releases.NewService(t.TempDir(), nil))}
	s.wirePipelineRecords(c)
	if c.PipelineRecorded == nil {
		t.Fatal("the release recorder is not kicked by a recorded run")
	}
	c.PipelineRecorded() // no recorder loop runs here: it must return
}

// A CI run's results name what it tested: <short>-commit and <short>-tree a
// repository, source-* the run's own; other results are not inputs, and a
// delivery run has none.
func TestPipelineRunFactReadsWhatACIRunTested(t *testing.T) {
	t.Parallel()
	a, b, c, d := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), strings.Repeat("d", 40)
	run := func(pipeline, repo string, results map[string]string) store.PipelineRun {
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
		f, ok := pipelineRunFact(u)
		if !ok {
			t.Fatal("not a fact")
		}
		return f
	}
	f := run("bifrost-ci-platform", "bifrost-platform", map[string]string{"platform-commit": a, "platform-tree": b, "ui-commit": c, "infra-commit": d,
		"api-scope": "incremental", "base-commit": a, "tested-by": "r", "failed-tests": ""})
	want := []store.RepoAt{{Repo: "bifrost-platform", Commit: a, Tree: b}, {Repo: "bifrost-trade-infra", Commit: d}, {Repo: "bifrost-ui", Commit: c}}
	if !reflect.DeepEqual(f.Inputs, want) || f.Scope != "incremental" || f.Tree != b {
		t.Fatalf("platform run: %+v scope %q", f.Inputs, f.Scope)
	}
	// A core run of the Python pipeline names core twice: the run's own wins.
	f = run("bifrost-ci-python", "bifrost-trade-core", map[string]string{"repo-commit": a, "repo-tree": b, "core-commit": c, "core-tree": d,
		"infra-commit": c, "infra-tree": d, "ui-commit": "main"})
	want = []store.RepoAt{{Repo: "bifrost-trade-core", Commit: a, Tree: b}, {Repo: "bifrost-trade-infra", Commit: c, Tree: d}}
	if !reflect.DeepEqual(f.Inputs, want) || f.Scope != "" {
		t.Fatalf("python run: %+v scope %q", f.Inputs, f.Scope)
	}
	f = run("bifrost-ci-python", "bifrost-research", map[string]string{"source-commit": a, "source-tree": b, "infra-commit": c, "infra-tree": d})
	want = []store.RepoAt{{Repo: "bifrost-research", Commit: a, Tree: b}, {Repo: "bifrost-trade-infra", Commit: c, Tree: d}}
	if !reflect.DeepEqual(f.Inputs, want) {
		t.Fatalf("research run: %+v", f.Inputs)
	}
	// An infra run reports the platform checkout it used as platform-tree:
	// an input, and not the tree of the run's repository.
	f = run("bifrost-ci-infra", "bifrost-trade-infra", map[string]string{"infra-commit": a, "infra-tree": b, "platform-commit": c, "platform-tree": d})
	want = []store.RepoAt{{Repo: "bifrost-platform", Commit: c, Tree: d}, {Repo: "bifrost-trade-infra", Commit: a, Tree: b}}
	if !reflect.DeepEqual(f.Inputs, want) || f.Tree != "" {
		t.Fatalf("infra run: %+v tree %q", f.Inputs, f.Tree)
	}
	if f = run("bifrost-deliver-platform", "", map[string]string{"agentd-commit": a, "platform-commit": a}); len(f.Inputs) != 0 {
		t.Fatalf("delivery run has inputs: %+v", f.Inputs)
	}
}

// The root tree of bifrost-platform at 057dcc1e, as git ls-tree lists it: git
// names that tree 315f7a37… (git rev-parse 057dcc1e^{tree}).
const platformRootTree = `100644 blob 6bae55cb70329356f1aa93f09d8753c071b60cbd .env.example
100644 blob d5437c3acef29b614da4d96b59866a4d20093f19 .gitignore
040000 tree c0c1a0224fd1a700d0c5beec0aee5553e16a6e4a .husky
100644 blob 209e3ef4b6247ce746048d5711befda46206d235 .nvmrc
100644 blob 7d6b23401bd55395714a07dec6f4b5d8dfa48229 CLAUDE.md
100644 blob 261eeb9e9f8b2b4b0d119366dda99c6fd7d35c64 LICENSE
100644 blob 72a1faabbb0f4a798b3f1830a146ca04dd9c9dbb Makefile
100644 blob a2531e382b6ec351091b809be384a3cb65cf354c PROGRESS-daily-ops-checklist-step2.md
100644 blob 355d7793dff6abfcbec4cc9197757a503b4cb0e9 PROGRESS-data-unification.md
100644 blob 1c63a6d21e028be11375fb37ecf30de405f78e01 PROGRESS-observability-hub.md
100644 blob 0dfb152fda39073d44c5365eb7d88062d02f4202 README.md
040000 tree 9ac478b2514964af20a0df1447b43ef082cdde83 agent
040000 tree 6e64bdf6c400c312376a12bdf7e2faa228400b86 api
040000 tree 2bce4888a21211bc9258f6a495eeba40a1293c92 config
040000 tree 9a161b48f1fef5691cc07533e9eb4f9167f153b9 console
040000 tree e3fc092e5d01bd8419bb3cbb089fedbffbe35fde mcp
040000 tree 71dbfa3003be3cf0fc2e4e167d5b353cc317a50a scripts
040000 tree 0ea7461831c51028ca3e8fb082af962dc73d8a8a site`

// The tree of a commit is computed from the entries of its root tree, read a
// page at a time and in any order; Gitea's own "sha" of the answer is the
// commit's id and is not used.
func TestGiteaCommitTree(t *testing.T) {
	t.Parallel()
	commit := "057dcc1ea9584ad95c3bf390434e146e15529c18"
	var all []map[string]string
	for _, line := range strings.Split(platformRootTree, "\n") {
		f := strings.Fields(line)
		all = append([]map[string]string{{"mode": f[0], "type": f[1], "sha": f[2], "path": f[3]}}, all...) // reversed
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, _ := r.BasicAuth(); user != "u" || pass != "p" || r.URL.Path != "/api/v1/repos/bifrost/bifrost-platform/git/trees/"+commit {
			http.Error(w, "no", http.StatusNotFound)
			return
		}
		page := all[:10]
		if r.URL.Query().Get("page") == "2" {
			page = all[10:]
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": commit, "total_count": len(all), "tree": page})
	}))
	defer srv.Close()
	read := giteaCommitTree(func(context.Context) (delivery.GiteaAccess, error) {
		return delivery.GiteaAccess{Base: srv.URL, Org: "bifrost", User: "u", Pass: "p"}, nil
	}, srv.Client())
	if got, err := read(context.Background(), "bifrost-platform", commit); err != nil || got != "315f7a3790d88e322e3ea855ebaa743b4b976876" {
		t.Fatalf("tree %q %v", got, err)
	}
	if _, err := read(context.Background(), "bifrost-ui", commit); err == nil {
		t.Fatal("a commit Gitea does not have gave a tree")
	}
	if _, err := read(context.Background(), "bifrost-platform", "main"); err == nil {
		t.Fatal("a branch name was read as a commit")
	}
	// git's order: a directory sorts as its name and "/" ("a.b" before "a/").
	got, err := gitTreeID([]commitTreeEntry{{Path: "a", Mode: "040000", Type: "tree", SHA: strings.Repeat("1", 40)}, {Path: "a.b", Mode: "100644", Type: "blob", SHA: strings.Repeat("2", 40)}})
	if err != nil || got != treeOfAAndAB {
		t.Fatalf("order: %q %v", got, err)
	}
}

// git mktree's id for a directory "a" and a file "a.b" with those ids.
const treeOfAAndAB = "f9a24078951ed457ecc90ac99b0abf5ecacb0f61"

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
func TestAPipelineRunWithABadScopeIsNotAFullRun(t *testing.T) {
	t.Parallel()
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	fact := func(results ...any) store.PipelineRun {
		u := unstructured.Unstructured{Object: map[string]any{
			"spec":   map[string]any{"pipelineRef": map[string]any{"name": "bifrost-ci-platform"}},
			"status": map[string]any{"completionTime": "2026-11-13T00:00:00Z", "conditions": []any{map[string]any{"type": "Succeeded", "status": "True", "reason": "Succeeded"}}, "results": results},
		}}
		u.SetName("r")
		u.SetUID("uid-r")
		u.SetLabels(map[string]string{"bifrost.io/repo": "bifrost-platform"})
		f, ok := pipelineRunFact(u)
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
		_, is, err := store.RunEvidence(fact(append(append([]any{}, own...), tc.scope...)...))
		if err != nil || is != tc.evidence {
			t.Errorf("%s: evidence %v, %v; want %v", name, is, err, tc.evidence)
		}
	}
}
