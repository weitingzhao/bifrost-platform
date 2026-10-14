package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

func ciRunAt(name, revision, status string, at time.Time) *unstructured.Unstructured {
	u := ciRun(name, "bifrost-ci-platform", "repo-p", "", revision, status)
	u.SetCreationTimestamp(metav1.NewTime(at))
	return u
}

func ciFacts(t *testing.T, objs ...runtime.Object) PolicyFacts {
	t.Helper()
	s := NewService(&config.ClusterEntry{ID: "test"})
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		pipelineRunGVR: "PipelineRunList",
	}, objs...)
	s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
	return s.PolicyFacts()
}

// CIRun's rule whatever the order runs are listed in (review 161 B4): any
// success wins; otherwise the newest run, Failed or Running.
func TestCIRunOrder(t *testing.T) {
	t1 := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	f := ciFacts(t,
		ciRunAt("ci-a-fail", factSHA1, "False", t2),
		ciRunAt("ci-a-ok", factSHA1, "True", t1),
		ciRunAt("ci-b-fail", factSHA2, "False", t1),
		ciRunAt("ci-b-run", factSHA2, "Unknown", t2),
		ciRunAt("ci-c-run", factSHA3, "Unknown", t1),
		ciRunAt("ci-c-fail", factSHA3, "False", t2),
		// listed older first: the newer Failed must replace the older Running
		ciRunAt("ci-d1-run", factSHA4, "Unknown", t1),
		ciRunAt("ci-d2-fail", factSHA4, "False", t2),
		noVerdict(ciRunAt("ci-e-new", factSHA5, "True", t1)),
	)
	for _, tc := range []struct{ sha, status, run string }{
		{factSHA1, CISucceeded, "ci-a-ok"},
		{factSHA2, CIRunning, "ci-b-run"},
		{factSHA3, CIFailed, "ci-c-fail"},
		{factSHA4, CIFailed, "ci-d2-fail"},
		{factSHA5, CIRunning, "ci-e-new"},
	} {
		if status, run, err := f.CIRun(context.Background(), "repo-p", tc.sha); err != nil || status != tc.status || run != tc.run {
			t.Errorf("%s = %q %q %v; want %q %q", tc.sha[:4], status, run, err, tc.status, tc.run)
		}
	}
	idx, err := f.CIIndex(context.Background())
	if err != nil || len(idx) != 5 {
		t.Fatalf("index = %v %v", idx, err)
	}
}

func TestCIRunListError(t *testing.T) {
	s := NewService(&config.ClusterEntry{ID: "test"})
	s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return nil, errors.New("no cluster") })
	if _, _, err := s.PolicyFacts().CIRun(context.Background(), "repo-p", factSHA1); err == nil {
		t.Fatal("a cluster it cannot reach read as no run")
	}
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		pipelineRunGVR: "PipelineRunList",
	})
	dyn.PrependReactor("list", "pipelineruns", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("list failed")
	})
	s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
	if _, _, err := s.PolicyFacts().CIRun(context.Background(), "repo-p", factSHA1); err == nil {
		t.Fatal("a failed list read as no run")
	}
}

func noVerdict(u *unstructured.Unstructured) *unstructured.Unstructured {
	u.Object["status"] = map[string]any{}
	return u
}

const (
	factSHA4 = "4444444444444444444444444444444444444444"
	factSHA5 = "5555555555555555555555555555555555555555"
)
