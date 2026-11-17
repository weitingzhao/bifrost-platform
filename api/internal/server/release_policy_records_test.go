//bifrost:owner delivery

package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)

// Owner 2026-11-15: a card must not go stale because STG moved on (#568
// waited 5h31m and every lane stopped). What the release policy reads of a
// source pipeline is every retained record, newest first, and only runs that
// succeeded have one: a delivery that failed vouches for no commit.
func TestShippedListsEverySuccessfulRecordNewestFirst(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "release-rules.yaml"),
		[]byte("namespace: cicd\nrules:\n  - match: { pipeline: deliver-platform }\n    lane: platform\n    env: stg\n    deploys: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	var objs []runtime.Object
	delivery := func(n string, hour int, succeeded, digit string) {
		done := day.Add(time.Duration(hour) * time.Hour)
		name := "deliver-platform-" + n
		objs = append(objs,
			tektonObject("PipelineRun", name, map[string]any{"tekton.dev/pipeline": "deliver-platform"},
				map[string]any{"params": []any{map[string]any{"name": "revision", "value": "main"}}},
				map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": succeeded}},
					"startTime": done.Add(-time.Minute).Format(time.RFC3339), "completionTime": done.Format(time.RFC3339)}),
			tektonObject("TaskRun", name+"-clone-platform", map[string]any{"tekton.dev/pipelineRun": name, "tekton.dev/pipelineTask": "clone-platform"},
				map[string]any{"params": []any{map[string]any{"name": "url", "value": "http://gitea/org/bifrost-platform.git"}}},
				map[string]any{"results": []any{map[string]any{"name": "commit", "value": strings.Repeat(digit, 40)}}}))
	}
	delivery("1", 1, "True", "1")
	delivery("2", 2, "True", "2")
	delivery("3", 3, "False", "3") // the newest delivery failed
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}: "PipelineRunList",
		{Group: "tekton.dev", Version: "v1", Resource: "taskruns"}:     "TaskRunList",
	}, objs...)
	core := k8sfake.NewSimpleClientset()
	svc := releases.NewService(dir, func() (kubernetes.Interface, dynamic.Interface, error) { return core, dyn, nil })
	if r := svc.Tick(ctx); r.Recorded != 2 || len(r.Errors) != 0 {
		t.Fatalf("tick = %+v", r)
	}
	got, err := recordDeployed{svc: svc}.Shipped(ctx, "deliver-platform")
	if err != nil || len(got) != 2 {
		t.Fatalf("shipped = %+v err %v", got, err)
	}
	for i, digit := range []string{"2", "1"} {
		if got[i].Repos["bifrost-platform"] != strings.Repeat(digit, 40) || !got[i].Succeeded {
			t.Fatalf("record %d = %+v", i, got[i])
		}
	}
	if other, err := (recordDeployed{svc: svc}).Shipped(ctx, "deliver-elsewhere"); err != nil || len(other) != 0 {
		t.Fatalf("another pipeline: %+v err %v", other, err)
	}
}
