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

func TestGiteaCommitTree(t *testing.T) {
	t.Parallel()
	commit, tree := strings.Repeat("a", 40), strings.Repeat("b", 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, _ := r.BasicAuth(); user != "u" || pass != "p" || r.URL.Path != "/api/v1/repos/bifrost/bifrost-ui/git/commits/"+commit {
			http.Error(w, "no", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"sha":"` + commit + `","commit":{"tree":{"sha":"` + tree + `"}}}`))
	}))
	defer srv.Close()
	read := giteaCommitTree(func(context.Context) (delivery.GiteaAccess, error) {
		return delivery.GiteaAccess{Base: srv.URL, Org: "bifrost", User: "u", Pass: "p"}, nil
	}, srv.Client())
	if got, err := read(context.Background(), "bifrost-ui", commit); err != nil || got != tree {
		t.Fatalf("tree %q %v", got, err)
	}
	if _, err := read(context.Background(), "bifrost-platform", commit); err == nil {
		t.Fatal("a commit Gitea does not have gave a tree")
	}
	if _, err := read(context.Background(), "bifrost-ui", "main"); err == nil {
		t.Fatal("a branch name was read as a commit")
	}
}
