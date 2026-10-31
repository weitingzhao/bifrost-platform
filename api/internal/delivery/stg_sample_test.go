package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
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
	kfake "k8s.io/client-go/kubernetes/fake"
)

func sampleWindowAdapter(t *testing.T, window *windowRecord) (*STGReleaseAdapter, *kfake.Clientset) {
	t.Helper()
	var objects []runtime.Object
	if window != nil {
		raw, err := json.Marshal(window)
		if err != nil {
			t.Fatal(err)
		}
		objects = append(objects, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: releaseWindowConfigMap, Namespace: STGReleaseNamespace, ResourceVersion: "1"}, Data: map[string]string{releaseWindowDataKey: string(raw)}})
	}
	core := kfake.NewSimpleClientset(objects...)
	return &STGReleaseAdapter{Core: func() (kubernetes.Interface, error) { return core, nil }, Now: func() time.Time { return stgTestNow }}, core
}

func readSampleWindow(t *testing.T, core *kfake.Clientset) windowRecord {
	t.Helper()
	cm, err := core.CoreV1().ConfigMaps(STGReleaseNamespace).Get(context.Background(), releaseWindowConfigMap, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := parseWindowCM(cm)
	if !ok {
		t.Fatal("unreadable window")
	}
	return rec
}

func TestSTGSampleWindowHoldsOnlyAsItsOwnWho(t *testing.T) {
	t.Parallel()
	const who = "stgrel-bifrost-platform-4242-1"
	a, core := sampleWindowAdapter(t, nil)
	expires, err := a.HoldSampleWindow(context.Background(), who, "TD-460 sample")
	if err != nil || !expires.Equal(stgTestNow.Add(60*time.Minute)) {
		t.Fatal("free window", expires, err)
	}
	rec := readSampleWindow(t, core)
	if rec.Who != who || rec.What != "bifrost-platform" || rec.Env != "stg" || rec.Reason != "TD-460 sample" {
		t.Fatalf("window %+v", rec)
	}
	// B5's own lease generation is never written by a sample.
	cm, err := core.CoreV1().ConfigMaps(STGReleaseNamespace).Get(context.Background(), releaseWindowConfigMap, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cm.ResourceVersion = "7" // the fake tracker does not stamp one; the apiserver does
	if w, err := decodeSTGWindow(cm); err != nil || w.Generation != 0 || w.Who != who {
		t.Fatal("sample wrote a B5 generation", w, err)
	}
	if _, err := a.HoldSampleWindow(context.Background(), who, "renew"); err != nil {
		t.Fatal("same who renews", err)
	}
	var held *WindowHeld
	if _, err := a.HoldSampleWindow(context.Background(), "stgrel-bifrost-platform-4243-1", "other"); !errors.As(err, &held) || held.Who != who {
		t.Fatal("other who must be refused while live", err)
	}
	for _, bad := range []struct{ who, reason string }{{"someone", "x"}, {who, stgWindowGenerationPrefix + "9"}} {
		if _, err := a.HoldSampleWindow(context.Background(), bad.who, bad.reason); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	if readSampleWindow(t, core).Who != who {
		t.Fatal("refused holds changed the window")
	}
}

func TestSTGSampleWindowEndsOnlyItsOwnLiveWindow(t *testing.T) {
	t.Parallel()
	const who = "stgrel-bifrost-platform-4242-1"
	live := stgTestNow.Add(10 * time.Minute).Format(time.RFC3339)
	for _, test := range []struct {
		name   string
		window *windowRecord
		reason string
		ended  bool
	}{
		{"own live window", &windowRecord{Who: who, What: "bifrost-platform", Env: "stg", ExpiresAt: live}, "", true},
		{"own live window, same reason", &windowRecord{Who: who, What: "bifrost-platform", Env: "stg", ExpiresAt: live, Reason: "mine"}, "mine", true},
		{"own who, another reason", &windowRecord{Who: who, What: "bifrost-platform", Env: "stg", ExpiresAt: live, Reason: "renewed"}, "mine", false},
		{"another holder", &windowRecord{Who: "release-sh", What: "bifrost-platform", Env: "stg", ExpiresAt: live}, "", false},
		{"expired", &windowRecord{Who: who, What: "bifrost-platform", Env: "stg", ExpiresAt: stgTestNow.Add(-time.Minute).Format(time.RFC3339)}, "", false},
		{"absent", nil, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			a, core := sampleWindowAdapter(t, test.window)
			ended, err := a.EndSampleWindow(context.Background(), who, test.reason)
			if err != nil || ended != test.ended {
				t.Fatal(ended, err)
			}
			if test.window == nil {
				return
			}
			rec := readSampleWindow(t, core)
			if test.ended != (rec.ExpiresAt == stgTestNow.Format(time.RFC3339)) || rec.Who != test.window.Who {
				t.Fatalf("window after end %+v", rec)
			}
		})
	}
}

func TestSTGSampleHeldRunsReadsTheFinalizerNotTheLabel(t *testing.T) {
	t.Parallel()
	run := func(name string, labels map[string]string, finalizers ...string) *unstructured.Unstructured {
		o := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "PipelineRun"}}
		o.SetName(name)
		o.SetNamespace(STGReleaseNamespace)
		o.SetLabels(labels)
		o.SetFinalizers(finalizers)
		return o
	}
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{pipelineRunGVR: "PipelineRunList"},
		run("stgrel-1-1-aaaaaaaa", map[string]string{"bifrost.io/stg-release": "true"}, STGReleaseFinalizer),
		run("unlabelled-held", nil, "other.example/keep", STGReleaseFinalizer),
		run("released", map[string]string{"bifrost.io/stg-release": "true"}),
		run("other-hold", nil, "other.example/keep"))
	a := &STGReleaseAdapter{Dynamic: func() (dynamic.Interface, error) { return dyn, nil }}
	got, err := a.HeldRuns(context.Background())
	if err != nil || !reflect.DeepEqual(got, []string{"stgrel-1-1-aaaaaaaa", "unlabelled-held"}) {
		t.Fatal(got, err)
	}
	if _, err := a.ReadRun(context.Background(), "missing"); err == nil {
		t.Fatal("missing run read")
	}
}
