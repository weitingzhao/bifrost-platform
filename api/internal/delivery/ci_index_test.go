package delivery

import (
	"context"
	"errors"
	"strings"
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

func withAPIScope(u *unstructured.Unstructured, scopes ...string) *unstructured.Unstructured {
	results := []any{}
	for _, s := range scopes {
		results = append(results, map[string]any{"name": "api-scope", "value": s})
	}
	u.Object["status"].(map[string]any)["results"] = results
	return u
}

// R5:5.7-1 gives ci-platform an api-scope result: a console-only change runs
// only part of the Go tests. Such a green neither vouches for the commit nor
// stands as its newest run, so it cannot cover a full red; a run without the
// result predates it and ran everything.
func TestCIRunCountsOnlyFullScopeGreens(t *testing.T) {
	t1 := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	f := ciFacts(t,
		ciRunAt("ci-a-red", factSHA1, "False", t1),
		withAPIScope(ciRunAt("ci-a-console", factSHA1, "True", t2), "console"),
		withAPIScope(ciRunAt("ci-b-console", factSHA2, "True", t1), "console"),
		withAPIScope(ciRunAt("ci-c-full", factSHA3, "True", t1), "full"),
		withAPIScope(ciRunAt("ci-c-console", factSHA3, "True", t2), "console"),
		withAPIScope(ciRunAt("ci-c-incremental", factSHA3, "True", t2.Add(time.Minute)), "incremental"),
		ciRunAt("ci-d-legacy", factSHA4, "True", t1),
		withAPIScope(ciRunAt("ci-e-odd", factSHA5, "True", t1), "partial"),
		withAPIScope(ciRunAt("ci-f-twice", factSHA6, "True", t1), "full", "console"),
		withAPIScope(ciRunAt("ci-h-twice", factSHA8, "True", t1), "console", "full"),
		withAPIScope(ciRunAt("ci-g-console-red", factSHA7, "False", t2), "console"),
		// PROD needs a full green of its exact commit: incremental is not one,
		// even with a recent full green elsewhere (ci-c-full).
		withAPIScope(ciRunAt("ci-i-incremental", factSHA9, "True", t2), "incremental"),
		ciRunAt("ci-g-green", factSHA7, "True", t1),
	)
	for _, tc := range []struct{ sha, status, run string }{
		{factSHA1, CIFailed, "ci-a-red"},
		{factSHA2, "", ""},
		{factSHA3, CISucceeded, "ci-c-full"},
		{factSHA4, CISucceeded, "ci-d-legacy"},
		{factSHA5, "", ""},
		{factSHA6, "", ""},
		{factSHA7, CISucceeded, "ci-g-green"},
		{factSHA8, "", ""},
		{factSHA9, "", ""},
	} {
		if status, run, err := f.CIRun(context.Background(), "repo-p", tc.sha); err != nil || status != tc.status || run != tc.run {
			t.Errorf("%s = %q %q %v; want %q %q", tc.sha[:4], status, run, err, tc.status, tc.run)
		}
	}
}

const (
	factSHA6 = "6666666666666666666666666666666666666666"
	factSHA7 = "7777777777777777777777777777777777777777"
	factSHA8 = "8888888888888888888888888888888888888888"
	factSHA9 = "9999999999999999999999999999999999999999"
)

func TestCIRunUnverifiedReuseDoesNotCount(t *testing.T) {
	at := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	for _, value := range []any{"missing-original", "", nil} {
		for _, olderRed := range []bool{false, true} {
			reuse := withAPIScope(ciRunAt("reuse", factSHA1, "True", at.Add(time.Hour)), "full")
			results := reuse.Object["status"].(map[string]any)["results"].([]any)
			reuse.Object["status"].(map[string]any)["results"] = append(results, map[string]any{"name": "tested-by", "value": value})
			objects := []runtime.Object{reuse}
			wantStatus, wantRun := "Unknown", "reuse"
			if olderRed {
				objects = append(objects, ciRunAt("red", factSHA1, "False", at))
			}
			f := ciFacts(t, objects...)
			status, run, err := f.CIRun(context.Background(), "repo-p", factSHA1)
			if err == nil || !strings.Contains(err.Error(), "reuse") || status != wantStatus || run != wantRun {
				t.Errorf("tested-by=%v olderRed=%v: got %q %q %v; want %q %q", value, olderRed, status, run, err, wantStatus, wantRun)
			}
			idx, err := f.CIIndex(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if _, seen := idx["repo-p@"+factSHA1]; !seen {
				t.Errorf("tested-by=%v olderRed=%v: index presence = %v", value, olderRed, seen)
			}
		}
	}
}
