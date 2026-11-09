package delivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
)

const boundaryTask = `apiVersion: tekton.dev/v1
kind: Task
metadata: {name: bifrost-ci-platform-test-api, namespace: cicd}
spec:
  steps:
  - name: unrelated
    script: echo unrelated
  - name: go-test
    computeResources:
      requests: {cpu: 500m, memory: 512Mi}
      limits: {cpu: "4", memory: 3Gi}
    script: |
      go test ./a -timeout=900s
      go test ./b -timeout=900s
`

func TestConfigurationBoundarySelectors(t *testing.T) {
	for _, i := range []int{0, 2, 3} {
		b := configurationBoundaries[i]
		value, err := b.read([]byte(boundaryTask))
		if err != nil {
			t.Fatal(err)
		}
		want := map[int]string{0: "900s", 2: "4", 3: "3Gi"}[i]
		if value != want {
			t.Fatalf("%s: %s", b.id, value)
		}
	}
	if _, err := configurationBoundaries[0].read([]byte(strings.Replace(boundaryTask, "./b -timeout=900s", "./b -timeout=600s", 1))); err == nil {
		t.Fatal("divergent script branches were accepted")
	}
	if err := configurationBoundaries[2].validate("100m", []byte(boundaryTask)); err == nil {
		t.Fatal("limit below request was accepted")
	}
	for _, v := range []string{"0s", "-1s", "1m; echo unsafe", "infinity"} {
		if err := configurationBoundaries[0].validate(v, nil); err == nil {
			t.Fatalf("invalid duration accepted: %s", v)
		}
	}
	if !configurationBoundaries[0].equal("900s", "15m") || !configurationBoundaries[2].equal("4", "4000m") {
		t.Fatal("equivalent quantities differ")
	}
}

func TestConfigurationReadAndPreviewNeverWrite(t *testing.T) {
	gitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("write: %s", r.Method)
			w.WriteHeader(405)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/branches/main") {
			_, _ = w.Write([]byte(`{"commit":{"id":"` + factSHA1 + `"}}`))
			return
		}
		if r.URL.Query().Get("ref") != factSHA1 {
			t.Error("source read was not pinned")
		}
		if strings.HasSuffix(r.URL.Path, "task-ci-platform-test-api.yaml") {
			_, _ = w.Write([]byte(boundaryTask))
			return
		}
		w.WriteHeader(404)
	}))
	defer gitea.Close()
	t.Setenv("GITEA_BASE", gitea.URL)
	s := NewService(&config.ClusterEntry{ID: "test"})
	live := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "Task", "metadata": map[string]any{"name": "bifrost-ci-platform-test-api", "namespace": "cicd"}, "spec": map[string]any{"steps": []any{map[string]any{"name": "go-test", "script": "go test ./... -timeout=600s"}}}}}
	dyn := dynfake.NewSimpleDynamicClient(runtime.NewScheme(), live)
	s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
	h := &Handler{svc: s}
	requests := []map[string]any{
		{"request": map[string]string{"boundary_id": "ci.platform.go_timeout", "value": "10m"}},
		{"request": map[string]string{"boundary_id": "ci.platform.go_timeout", "value": "15m"}},
		{"request": map[string]string{"boundary_id": "ci.platform.go_timeout", "value": "30m"}},
	}
	out, err := h.ConfigurationBoundaries(context.Background(), "ci.platform.go_timeout", "30m", requests)
	if err != nil {
		t.Fatal(err)
	}
	rows := out["rows"].([]map[string]any)
	if rows[0]["source_value"] != "900s" || rows[0]["installed_value"] != "600s" || rows[0]["state"] != "source_differs_from_installed" {
		t.Fatalf("drift: %v", rows[0])
	}
	preview := out["preview"].(map[string]any)
	if !strings.Contains(preview["goal"].(string), factSHA1) || !strings.Contains(preview["goal"].(string), "30m") {
		t.Fatalf("unpinned goal: %v", preview)
	}
	for i, want := range []string{"requested_value_observed", "awaiting_deployment", "awaiting_source_change"} {
		if requests[i]["delivery_state"] != want {
			t.Fatalf("request %d: %v", i, requests[i])
		}
	}
	if _, err = h.ConfigurationBoundaries(context.Background(), "ci.platform.go_timeout", "15m", nil); err == nil {
		t.Fatal("no-op request would create unnecessary delivery")
	}
	for _, action := range dyn.Actions() {
		if action.GetVerb() != "get" {
			t.Fatalf("write: %+v", action)
		}
	}
}
