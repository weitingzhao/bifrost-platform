package releases

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

// Current (for the agent runtime's gates_passed) reads the newest deploying
// record strictly: an unreadable record or a newer delivery run makes it
// unsettled, never the previous deployment.
func TestCurrent(t *testing.T) {
	ctx := context.Background()
	t1 := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	t2 := t1.Add(24 * time.Hour)
	core := k8sfake.NewSimpleClientset()
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		pipelineRunGVR: "PipelineRunList", taskRunGVR: "TaskRunList",
	},
		pipelineRun("deliver-stg-1", "deliver-stg", true, t1, nil, nil),
		cloneTaskRun("deliver-stg-1", "clone-core", "core", shaA),
		pipelineRun("deliver-stg-2", "deliver-stg", true, t2, nil, nil),
		cloneTaskRun("deliver-stg-2", "clone-core", "core", shaC),
		pipelineRun("build-image-1", "build-image", true, t2.Add(time.Hour), nil, nil), // builds, deploys nothing
	)
	s := NewService(writeRules(t, rulesYAML), func() (kubernetes.Interface, dynamic.Interface, error) { return core, dyn, nil })
	if r := s.Tick(ctx); r.Recorded != 3 || len(r.Errors) != 0 {
		t.Fatalf("tick = %+v", r)
	}
	rec, err := s.Current(ctx, "app", "stg")
	if err != nil || rec.Run != "deliver-stg-2" || rec.Repos["core"].SHA != shaC {
		t.Fatalf("current = %+v %v", rec, err)
	}
	if _, err = s.Current(ctx, "app", "prod"); !errors.Is(err, ErrNoDeployment) {
		t.Fatalf("prod = %v", err)
	}

	// another lane's unreadable record is not this one's business
	_, err = core.CoreV1().ConfigMaps("cicd").Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "release-other", Labels: map[string]string{labelRecord: recordVersion, labelLane: "other", labelEnv: "stg"}},
		Data:       map[string]string{dataKey: "{bad"},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rec, err = s.Current(ctx, "app", "stg"); err != nil || rec.Run != "deliver-stg-2" {
		t.Fatalf("with another lane's bad record = %+v %v", rec, err)
	}

	// a delivery started after the newest record: in flight, failed, or not recorded yet
	for _, newer := range []struct {
		name string
		ok   bool
	}{{"deliver-stg-3", false}, {"deliver-stg-4", true}} {
		run := pipelineRun(newer.name, "deliver-stg", newer.ok, t2.Add(2*time.Hour), nil, nil)
		if _, err = dyn.Resource(pipelineRunGVR).Namespace("cicd").Create(ctx, run, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err = s.Current(ctx, "app", "stg"); !errors.Is(err, ErrUnsettled) {
			t.Fatalf("with %s newer than the record = %v", newer.name, err)
		}
		if err = dyn.Resource(pipelineRunGVR).Namespace("cicd").Delete(ctx, newer.name, metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	// the newest record unreadable, its run already pruned from Tekton:
	// unsettled, not the older deployment
	if err = dyn.Resource(pipelineRunGVR).Namespace("cicd").Delete(ctx, "deliver-stg-2", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if rec, err = s.Current(ctx, "app", "stg"); err != nil || rec.Run != "deliver-stg-2" {
		t.Fatalf("with its run pruned = %+v %v", rec, err)
	}
	cm, err := core.CoreV1().ConfigMaps("cicd").Get(ctx, "release-deliver-stg-2", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cm.Data[dataKey] = "{bad"
	if _, err = core.CoreV1().ConfigMaps("cicd").Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if rec, err = s.Current(ctx, "app", "stg"); !errors.Is(err, ErrUnsettled) {
		t.Fatalf("with the newest record unreadable = %+v %v", rec, err)
	}
	if recs, _ := s.List(ctx); len(recs) == 0 || recs[0].Run == "deliver-stg-2" {
		t.Fatalf("List, which skips what it cannot read, = %+v", recs)
	}
}

// Review 162 A2, A3: a run that may have changed the environment after the
// record, by how and when it ended, and a record the labels chose that does
// not read as a deployment.
func TestCurrentEdges(t *testing.T) {
	ctx := context.Background()
	t1 := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	fresh := func(t *testing.T, extra ...runtime.Object) (*Service, *k8sfake.Clientset, dynamic.Interface) {
		t.Helper()
		core := k8sfake.NewSimpleClientset()
		objs := append([]runtime.Object{
			pipelineRun("deliver-stg-a", "deliver-stg", true, t1, nil, nil),
			cloneTaskRun("deliver-stg-a", "clone-core", "core", shaA),
		}, extra...)
		dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
			pipelineRunGVR: "PipelineRunList", taskRunGVR: "TaskRunList",
		}, objs...)
		s := NewService(writeRules(t, rulesYAML), func() (kubernetes.Interface, dynamic.Interface, error) { return core, dyn, nil })
		if r := s.Tick(ctx); r.Recorded < 1 || len(r.Errors) != 0 {
			t.Fatalf("tick = %+v", r)
		}
		return s, core, dyn
	}
	withStatus := func(u *unstructured.Unstructured, status map[string]any) *unstructured.Unstructured {
		u.Object["status"] = status
		return u
	}
	for _, c := range []struct {
		name    string
		run     *unstructured.Unstructured
		settled bool
	}{
		// started before the record's run, finished after it: may have rolled out last
		{"earlier start, later finish", withStatus(pipelineRun("deliver-stg-b", "deliver-stg", true, t1, nil, nil), map[string]any{
			"startTime": t1.Add(-2 * time.Hour).Format(time.RFC3339), "completionTime": t1.Add(time.Hour).Format(time.RFC3339),
			"conditions": []any{map[string]any{"type": "Succeeded", "status": "True"}}}), false},
		{"finished at the same time", pipelineRun("deliver-stg-b", "deliver-stg", false, t1, nil, nil), false},
		{"running", withStatus(pipelineRun("deliver-stg-b", "deliver-stg", true, t1, nil, nil),
			map[string]any{"startTime": t1.Add(-2 * time.Hour).Format(time.RFC3339)}), false},
		{"never started", withStatus(pipelineRun("deliver-stg-b", "deliver-stg", true, t1, nil, nil), map[string]any{}), false},
		{"finished before the record", pipelineRun("deliver-stg-b", "deliver-stg", false, t1.Add(-time.Minute), nil, nil), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, _, dyn := fresh(t)
			if _, err := dyn.Resource(pipelineRunGVR).Namespace("cicd").Create(ctx, c.run, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
			rec, err := s.Current(ctx, "app", "stg")
			if c.settled != (err == nil) || (err != nil && !errors.Is(err, ErrUnsettled)) || (err == nil && rec.Run != "deliver-stg-a") {
				t.Fatalf("current = %+v %v", rec, err)
			}
		})
	}
	for _, body := range []string{`{}`, `{"run":"deliver-stg-z","lane":"app","env":"prod","deploys":true,"started_at":"2026-10-06T12:00:00Z","completed_at":"2026-10-06T12:03:00Z"}`,
		`{"run":"deliver-stg-z","lane":"app","env":"stg","started_at":"2026-10-06T12:00:00Z","completed_at":"2026-10-06T12:03:00Z"}`} {
		s, core, _ := fresh(t)
		_, err := core.CoreV1().ConfigMaps("cicd").Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "release-deliver-stg-z", Labels: map[string]string{labelRecord: recordVersion, labelLane: "app", labelEnv: "stg"}},
			Data:       map[string]string{dataKey: body},
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if rec, err := s.Current(ctx, "app", "stg"); !errors.Is(err, ErrUnsettled) {
			t.Fatalf("a record %s = %+v %v", body, rec, err)
		}
	}
	broken := NewService(writeRules(t, rulesYAML), func() (kubernetes.Interface, dynamic.Interface, error) { return nil, nil, errors.New("no cluster") })
	if _, err := broken.Current(ctx, "app", "stg"); err == nil {
		t.Fatal("no cluster read as a deployment")
	}
}
