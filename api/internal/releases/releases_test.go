package releases

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

const (
	shaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	shaC = "cccccccccccccccccccccccccccccccccccccccc"
)

const rulesYAML = `
namespace: cicd
rules:
  - match: { name_prefix: deliver-prod-pinned- }
    lane: app
    env: prod
    deploys: true
  - match: { pipeline: deliver-stg }
    lane: app
    env: stg
    deploys: true
  - match: { pipeline: build-image }
    lane: app
    env: image
`

func writeRules(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, rulesFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadRulesAndMatch(t *testing.T) {
	r, _, err := LoadRules(writeRules(t, rulesYAML))
	if err != nil {
		t.Fatal(err)
	}
	// an inline run's pipeline label is its own name: the prefix rule must win
	if rule, ok := r.Match("deliver-prod-pinned-x1", "deliver-prod-pinned-x1"); !ok || rule.Env != "prod" || !rule.Deploys {
		t.Fatalf("pinned = %+v %v", rule, ok)
	}
	if rule, ok := r.Match("deliver-stg-123", "deliver-stg"); !ok || rule.Env != "stg" {
		t.Fatalf("stg = %+v %v", rule, ok)
	}
	if _, ok := r.Match("ci-frontend-1", "ci-frontend"); ok {
		t.Fatal("ci runs must not match")
	}
	if _, _, err := LoadRules(writeRules(t, "rules:\n  - lane: x\n    env: y\n")); err == nil {
		t.Fatal("a rule that matches nothing must be rejected")
	}
	if r, _, err := LoadRules(t.TempDir()); err != nil || len(r.Rules) != 0 {
		t.Fatalf("missing file = %+v %v, want empty and no error", r, err)
	}
}

func pipelineRun(name, pipeline string, ok bool, done time.Time, pinned map[string]string, labels map[string]string) *unstructured.Unstructured {
	status := "True"
	if !ok {
		status = "False"
	}
	lab := map[string]any{"tekton.dev/pipeline": pipeline}
	for k, v := range labels {
		lab[k] = v
	}
	obj := map[string]any{
		"apiVersion": "tekton.dev/v1", "kind": "PipelineRun",
		"metadata": map[string]any{"name": name, "namespace": "cicd", "labels": lab},
		"spec":     map[string]any{"params": []any{map[string]any{"name": "revision", "value": "main"}}},
		"status": map[string]any{
			"startTime":      done.Add(-3 * time.Minute).Format(time.RFC3339),
			"completionTime": done.Format(time.RFC3339),
			"conditions":     []any{map[string]any{"type": "Succeeded", "status": status}},
		},
	}
	if len(pinned) > 0 {
		var tasks []any
		for task, sha := range pinned {
			tasks = append(tasks, map[string]any{"name": task, "params": []any{
				map[string]any{"name": "url", "value": "$(params.giteaBase)/x.git"},
				map[string]any{"name": "revision", "value": sha},
			}})
		}
		obj["spec"].(map[string]any)["pipelineSpec"] = map[string]any{"tasks": tasks}
	}
	return &unstructured.Unstructured{Object: obj}
}

func cloneTaskRun(run, task, repo, sha string) *unstructured.Unstructured {
	st := map[string]any{}
	if sha != "" {
		st["results"] = []any{map[string]any{"name": "commit", "type": "string", "value": sha + "\n"}}
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tekton.dev/v1", "kind": "TaskRun",
		"metadata": map[string]any{"name": run + "-" + task, "namespace": "cicd", "labels": map[string]any{
			"tekton.dev/pipelineRun": run, "tekton.dev/pipelineTask": task}},
		"spec":   map[string]any{"params": []any{map[string]any{"name": "url", "value": "http://gitea:3000/org/" + repo + ".git"}}},
		"status": st,
	}}
}

func TestBuildRecordSources(t *testing.T) {
	done := time.Date(2026, 10, 6, 18, 0, 0, 0, time.UTC)
	run := pipelineRun("deliver-prod-pinned-x1", "deliver-prod-pinned-x1", true, done,
		map[string]string{"clone-api": shaB}, map[string]string{"bifrost.io/from-stg-run": "deliver-stg-9"})
	trs := []unstructured.Unstructured{
		*cloneTaskRun("deliver-prod-pinned-x1", "clone-core", "core", shaA), // result
		*cloneTaskRun("deliver-prod-pinned-x1", "clone-api", "api", ""),     // no result, pinned in spec
		*cloneTaskRun("deliver-prod-pinned-x1", "clone-ui", "ui", ""),       // neither
		{Object: map[string]any{"metadata": map[string]any{"name": "build", "labels": map[string]any{"tekton.dev/pipelineTask": "build"}}}},
	}
	rec := buildRecord(run, Rule{Lane: "app", Env: "prod", Deploys: true}, trs, done)
	if rec.Repos["core"] != (RepoBuild{SHA: shaA, Source: "result"}) || rec.Repos["api"] != (RepoBuild{SHA: shaB, Source: "pinned"}) {
		t.Fatalf("repos = %+v", rec.Repos)
	}
	if strings.Join(rec.Missing, ",") != "clone-ui" || rec.FromRun != "deliver-stg-9" || rec.Revision != "main" || !rec.CompletedAt.Equal(done) {
		t.Fatalf("record = %+v", rec)
	}
}

func newFakeService(t *testing.T, objs ...runtime.Object) (*Service, *k8sfake.Clientset) {
	t.Helper()
	core := k8sfake.NewSimpleClientset()
	scheme := runtime.NewScheme()
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		pipelineRunGVR: "PipelineRunList", taskRunGVR: "TaskRunList",
	}, objs...)
	s := NewService(writeRules(t, rulesYAML), func() (kubernetes.Interface, dynamic.Interface, error) { return core, dyn, nil })
	return s, core
}

func TestTickRecordsOnceAndListReadsBack(t *testing.T) {
	t1 := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)
	s, core := newFakeService(t,
		pipelineRun("deliver-stg-1", "deliver-stg", true, t1, nil, nil),
		cloneTaskRun("deliver-stg-1", "clone-core", "core", shaA),
		pipelineRun("deliver-stg-2", "deliver-stg", true, t2, nil, nil),
		cloneTaskRun("deliver-stg-2", "clone-core", "core", shaC),
		pipelineRun("deliver-stg-3", "deliver-stg", false, t2, nil, nil), // failed: not a release
		pipelineRun("ci-frontend-1", "ci-frontend", true, t2, nil, nil),  // no rule
	)
	r := s.Tick(context.Background())
	if r.Runs != 4 || r.Matched != 3 || r.Recorded != 2 || len(r.Errors) != 0 {
		t.Fatalf("first tick = %+v", r)
	}
	if again := s.Tick(context.Background()); again.Recorded != 0 {
		t.Fatalf("second tick re-recorded: %+v", again)
	}
	cm, err := core.CoreV1().ConfigMaps("cicd").Get(context.Background(), "release-deliver-stg-2", metav1.GetOptions{})
	if err != nil || cm.Labels[labelEnv] != "stg" || cm.Labels[labelRecord] != recordVersion {
		t.Fatalf("configmap = %+v %v", cm, err)
	}
	recs, err := s.List(context.Background())
	if err != nil || len(recs) != 2 || recs[0].Run != "deliver-stg-2" || recs[0].Repos["core"].SHA != shaC {
		t.Fatalf("list = %+v %v", recs, err)
	}
	if st := s.Status(); st.Rules != 3 || st.LastTick == nil || st.Namespace != "cicd" {
		t.Fatalf("status = %+v", st)
	}
}

func TestNoRulesRecordsNothing(t *testing.T) {
	s := NewService(t.TempDir(), func() (kubernetes.Interface, dynamic.Interface, error) {
		t.Fatal("clients must not be needed without rules")
		return nil, nil, nil
	})
	if s.Enabled() {
		t.Fatal("enabled without rules")
	}
	s.Start(context.Background(), time.Hour) // returns without starting a loop
}

func TestParamSHAAndIncompleteRecordIsCompletedLater(t *testing.T) {
	done := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	// a pipelineRef run whose revision is a full SHA: clones without a result use it
	run := pipelineRun("deliver-stg-p", "deliver-stg", true, done, nil, nil)
	run.Object["spec"].(map[string]any)["params"] = []any{map[string]any{"name": "revision", "value": shaB}}
	rec := buildRecord(run, Rule{Lane: "app", Env: "stg"}, []unstructured.Unstructured{*cloneTaskRun("deliver-stg-p", "clone-core", "core", "")}, done)
	if rec.Repos["core"] != (RepoBuild{SHA: shaB, Source: "param"}) || len(rec.Missing) != 0 {
		t.Fatalf("param record = %+v", rec)
	}

	// a clone at its own param (bifrost-ui at uiRevision) never takes the run's revision
	withRev := func(tr *unstructured.Unstructured, rev string) unstructured.Unstructured {
		params := tr.Object["spec"].(map[string]any)["params"].([]any)
		tr.Object["spec"].(map[string]any)["params"] = append(params, map[string]any{"name": "revision", "value": rev})
		return *tr
	}
	rec = buildRecord(run, Rule{Lane: "app", Env: "stg"}, []unstructured.Unstructured{
		withRev(cloneTaskRun("deliver-stg-p", "clone-ui", "ui", ""), shaC),
		withRev(cloneTaskRun("deliver-stg-p", "clone-web", "web", ""), "main"),
	}, done)
	if rec.Repos["ui"] != (RepoBuild{SHA: shaC, Source: "param"}) || rec.Repos["web"] != (RepoBuild{}) ||
		strings.Join(rec.Missing, ",") != "clone-web" {
		t.Fatalf("own-param record = %+v", rec)
	}

	// a run at a moving ref with no result is stored incomplete …
	s, core := newFakeService(t,
		pipelineRun("deliver-stg-m", "deliver-stg", true, done, nil, nil),
		cloneTaskRun("deliver-stg-m", "clone-core", "core", ""),
	)
	if r := s.Tick(context.Background()); r.Recorded != 1 {
		t.Fatalf("tick = %+v", r)
	}
	cm, _ := core.CoreV1().ConfigMaps("cicd").Get(context.Background(), "release-deliver-stg-m", metav1.GetOptions{})
	if cm.Labels[labelComplete] != "false" {
		t.Fatalf("labels = %+v", cm.Labels)
	}
	// … is left alone while nothing new is known …
	if r := s.Tick(context.Background()); r.Recorded != 0 {
		t.Fatalf("unchanged rebuild rewrote the record: %+v", r)
	}
	// … and is completed once the run yields the commit
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		pipelineRunGVR: "PipelineRunList", taskRunGVR: "TaskRunList",
	}, pipelineRun("deliver-stg-m", "deliver-stg", true, done, nil, nil), cloneTaskRun("deliver-stg-m", "clone-core", "core", shaC))
	s.clients = func() (kubernetes.Interface, dynamic.Interface, error) { return core, dyn, nil }
	if r := s.Tick(context.Background()); r.Recorded != 1 {
		t.Fatalf("completing tick = %+v", r)
	}
	recs, _ := s.List(context.Background())
	if len(recs) != 1 || recs[0].Repos["core"].SHA != shaC || len(recs[0].Missing) != 0 {
		t.Fatalf("completed = %+v", recs)
	}
}

func TestRecorderWanted(t *testing.T) {
	cases := []struct {
		flag, k8s string
		want      bool
	}{
		{"", "", false},         // laptop: read only
		{"", "10.43.0.1", true}, // in-cluster
		{"on", "", true},        // explicit opt-in
		{"off", "10.43.0.1", false},
	}
	for _, c := range cases {
		t.Setenv("PLATFORM_RELEASE_RECORDER", c.flag)
		t.Setenv("KUBERNETES_SERVICE_HOST", c.k8s)
		if got := RecorderWanted(); got != c.want {
			t.Errorf("flag=%q k8s=%q: got %v want %v", c.flag, c.k8s, got, c.want)
		}
	}
}
