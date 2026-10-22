package delivery

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// TD-420 at creation, the legacy and CI pipelines (review 510 B2): a signed
// full SHA Gitea answers for is fine, one it does not have is refused with the
// repository and the SHA, and one whose lookup fails is unknown and does not
// refuse (the legacy status would call it missing). A branch is not looked up.
func TestRequestRefsLegacyAndCIAtCreation(t *testing.T) {
	const present, missing, failing = factSHA1, factSHA2, "3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c"
	for _, name := range []string{"bifrost-deliver-stg", "bifrost-ci-platform"} {
		pipe := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "Pipeline",
			"metadata": map[string]any{"name": name, "namespace": "cicd"}, "spec": map[string]any{}}}
		s := NewService(&config.ClusterEntry{ID: "test"})
		s.SetClientsetForTest(k8sfake.NewSimpleClientset())
		dyn := dynfake.NewSimpleDynamicClient(k8sruntime.NewScheme(), pipe)
		s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
		var paths []string
		s.httpClient = &http.Client{Transport: contractTransport(func(r *http.Request) (*http.Response, error) {
			paths = append(paths, r.URL.Path)
			code, body := http.StatusOK, "[]"
			switch {
			case strings.HasSuffix(r.URL.Path, "/git/commits/"+present):
				body = `{"sha":"` + present + `"}`
			case strings.HasSuffix(r.URL.Path, "/git/commits/"+failing):
				code, body = http.StatusServiceUnavailable, "busy"
			case strings.Contains(r.URL.Path, "/git/commits/"):
				code, body = http.StatusNotFound, "{}"
			}
			return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
		})}
		check := func(rev string) error {
			return s.CheckRequestRefs(context.Background(), map[string]any{"name": name, "revision": rev, "who": "owner"})
		}
		if err := check(present); err != nil {
			t.Errorf("%s, a commit Gitea has: %v", name, err)
		}
		if err := check(missing); err == nil || !strings.Contains(err.Error(), "revision is missing in: ") ||
			!strings.Contains(err.Error(), missing) || !strings.Contains(err.Error(), reposForPipeline(name)[0]+" (ref") {
			t.Errorf("%s, a commit Gitea does not have: %v", name, err)
		}
		if err := check(failing); err != nil {
			t.Errorf("%s, a commit whose lookup fails: %v (unknown is not missing)", name, err)
		}
		paths = nil
		err := check("main")
		switch {
		case isCIPipeline(name) && (err == nil || !strings.Contains(err.Error(), "40-character")):
			t.Errorf("%s runs a full SHA, as at the run: %v", name, err)
		case !isCIPipeline(name) && err != nil:
			t.Errorf("%s, a branch: %v", name, err)
		case len(paths) != 0:
			t.Errorf("%s, a branch was looked up: %v", name, paths)
		}
	}
}
