package releases

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
