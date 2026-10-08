package delivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

const (
	factSHA1 = "1111111111111111111111111111111111111111"
	factSHA2 = "2222222222222222222222222222222222222222"
	factSHA3 = "3333333333333333333333333333333333333333"
)

func ciRun(name, pipeline, repoParam, repoLabel, revision, status string) *unstructured.Unstructured {
	labels := map[string]any{"tekton.dev/pipeline": pipeline}
	if repoLabel != "" {
		labels["bifrost.io/repo"] = repoLabel
	}
	params := []any{map[string]any{"name": "revision", "value": revision}}
	if repoParam != "" {
		params = append(params, map[string]any{"name": "repo", "value": repoParam})
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tekton.dev/v1", "kind": "PipelineRun",
		"metadata": map[string]any{"name": name, "namespace": "cicd", "labels": labels},
		"spec":     map[string]any{"params": params},
		"status":   map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": status}}},
	}}
}

func newFactsService(t *testing.T, cms ...*corev1.ConfigMap) PolicyFacts {
	t.Helper()
	gitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/repos/bifrost/repo-app/branches/main":
			_, _ = w.Write([]byte(`{"name":"main","commit":{"id":"` + factSHA1 + `"}}`))
		case "/api/v1/repos/bifrost/repo-app/tags/v1.0.0":
			_, _ = w.Write([]byte(`{"name":"v1.0.0","commit":{"sha":"` + factSHA2 + `"}}`))
		case "/api/v1/repos/bifrost/repo-app/git/commits/" + factSHA1:
			_, _ = w.Write([]byte(`{"sha":"` + factSHA1 + `"}`))
		case "/api/v1/repos/bifrost/repo-app/compare/" + factSHA2 + "..." + factSHA1:
			_, _ = w.Write([]byte(`{"files":[{"filename":"src/b.py"},{"filename":"docs/a.md"}]}`))
		case "/api/v1/repos/bifrost/repo-broken/branches/main":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(gitea.Close)
	t.Setenv("GITEA_BASE", gitea.URL)

	s := NewService(&config.ClusterEntry{ID: "test"})
	objs := make([]runtime.Object, 0, len(cms))
	for _, cm := range cms {
		objs = append(objs, cm)
	}
	s.SetClientsetForTest(k8sfake.NewSimpleClientset(objs...))
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		pipelineRunGVR: "PipelineRunList",
	},
		ciRun("ci-py-1", "bifrost-ci-python", "repo-app", "", factSHA1, "True"),
		ciRun("ci-fe-1", "bifrost-ci-frontend", "", "repo-ui", factSHA2, "True"),
		ciRun("ci-py-2", "bifrost-ci-python", "repo-app", "", factSHA3, "False"),
		ciRun("deliver-1", "bifrost-deliver-stg", "repo-app", "", factSHA2, "True"),
	)
	s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
	return s.PolicyFacts()
}

func TestPolicyFactsGitea(t *testing.T) {
	f := newFactsService(t)
	ctx := context.Background()
	if head, err := f.MainHead(ctx, "repo-app"); err != nil || head != factSHA1 {
		t.Fatalf("MainHead = %q %v", head, err)
	}
	if _, err := f.MainHead(ctx, "repo-broken"); err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("broken MainHead err = %v", err)
	}
	if _, err := f.MainHead(ctx, "repo-gone"); err == nil {
		t.Fatal("missing main gave no error")
	}
	if sha, ok, err := f.TagCommit(ctx, "repo-app", "v1.0.0"); err != nil || !ok || sha != factSHA2 {
		t.Fatalf("TagCommit = %q %v %v", sha, ok, err)
	}
	if _, ok, err := f.TagCommit(ctx, "repo-app", "v9"); err != nil || ok {
		t.Fatalf("missing tag = %v %v", ok, err)
	}
	if ok, err := f.CommitExists(ctx, "repo-app", factSHA1); err != nil || !ok {
		t.Fatalf("CommitExists = %v %v", ok, err)
	}
	if ok, err := f.CommitExists(ctx, "repo-app", factSHA3); err != nil || ok {
		t.Fatalf("unknown commit = %v %v", ok, err)
	}
	files, err := f.ChangedFiles(ctx, "repo-app", factSHA2, factSHA1)
	if err != nil || strings.Join(files, ",") != "docs/a.md,src/b.py" {
		t.Fatalf("ChangedFiles = %v %v", files, err)
	}
	if _, err := f.ChangedFiles(ctx, "repo-app", factSHA3, factSHA1); err == nil {
		t.Fatal("a failed compare gave no error")
	}
}

func TestPolicyFactsCI(t *testing.T) {
	f := newFactsService(t)
	ctx := context.Background()
	for _, tc := range []struct {
		repo, sha string
		want      bool
	}{
		{"repo-app", factSHA1, true},  // repo param
		{"repo-ui", factSHA2, true},   // bifrost.io/repo label
		{"repo-app", factSHA3, false}, // failed run
		{"repo-app", factSHA2, false}, // a deliver run is not CI
	} {
		if got, err := f.Succeeded(ctx, tc.repo, tc.sha); err != nil || got != tc.want {
			t.Errorf("Succeeded(%s, %s) = %v %v", tc.repo, tc.sha[:4], got, err)
		}
	}
}

func TestPolicyFactsWindowAndConfigMaps(t *testing.T) {
	window := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: releaseWindowConfigMap, Namespace: "cicd"},
		Data:       map[string]string{"window.json": `{"who":"agent@mac","what":"bifrost-research"}`},
	}
	freeze := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "bifrost-release-freeze", Namespace: "cicd"},
		Data:       map[string]string{"frozen": "false"},
	}
	f := newFactsService(t, window, freeze)
	ctx := context.Background()
	if msg := f.HeldBy(ctx, "bifrost-deliver-research", "agent@mac"); msg != "" {
		t.Fatalf("holder refused: %s", msg)
	}
	if msg := f.HeldBy(ctx, "bifrost-deliver-research", "someone@else"); !strings.Contains(msg, "held by someone else") {
		t.Fatalf("other caller = %q", msg)
	}
	if msg := f.HeldBy(ctx, "bifrost-deliver-research", ""); !strings.Contains(msg, "no who") {
		t.Fatalf("empty who = %q", msg)
	}
	if data, found, err := f.ConfigMap(ctx, "bifrost-release-freeze"); err != nil || !found || data["frozen"] != "false" {
		t.Fatalf("freeze = %v %v %v", data, found, err)
	}
	if _, found, err := f.ConfigMap(ctx, "bifrost-release-policy"); err != nil || found {
		t.Fatalf("missing policy = %v %v", found, err)
	}

	none := newFactsService(t)
	if msg := none.HeldBy(ctx, "bifrost-deliver-research", "agent@mac"); !strings.Contains(msg, "no release window is open") {
		t.Fatalf("no window = %q", msg)
	}
}
