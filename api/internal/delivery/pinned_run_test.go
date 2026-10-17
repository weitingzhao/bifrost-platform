package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

type pinnedMirror struct {
	refs      map[string]string
	missing   map[string]bool
	posts     map[string]int
	gets      map[string]int
	mode      string
	paths     []string
	afterSync map[string]int
}

func pinnedService(t *testing.T, name string, refs map[string]string, missing []string, mode string) (*Service, *dynfake.FakeDynamicClient, *pinnedMirror) {
	t.Helper()
	pipe := baselinePipelines(t)[name]
	if pipe == nil {
		pipe = &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "Pipeline", "metadata": map[string]any{"name": name, "namespace": "cicd"}, "spec": map[string]any{}}}
	}
	s, dyn, _ := contractService(t, pipe, "present")
	repos := reposForPipeline(name)
	s.SetPolicy(&actuationpolicy.Policy{Mirrors: actuationpolicy.Mirrors{Repos: repos}})
	window, _ := json.Marshal(map[string]any{"who": "tester", "what": sortedKeys(requiredRepoSet(name))})
	objects := []runtime.Object{&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "ci", Labels: map[string]string{"kubernetes.io/arch": "amd64"}}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}}}
	if _, known := requiredRepos(name); known {
		objects = append(objects, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: releaseWindowConfigMap, Namespace: "cicd"}, Data: map[string]string{releaseWindowDataKey: string(window)}})
	}
	s.SetClientsetForTest(k8sfake.NewSimpleClientset(objects...))
	m := &pinnedMirror{refs: refs, missing: map[string]bool{}, posts: map[string]int{}, gets: map[string]int{}, mode: mode, afterSync: map[string]int{}}
	for _, repo := range missing {
		m.missing[repo] = true
	}
	s.httpClient = &http.Client{Transport: contractTransport(func(r *http.Request) (*http.Response, error) {
		// The repository itself: every one here is a pull mirror (mirror sync
		// asks first, CUT-gitea-primary 0b). Not counted with the reads below.
		if r.Method == http.MethodGet && !strings.Contains(strings.TrimPrefix(r.URL.Path, "/api/v1/repos/bifrost/"), "/") {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"mirror":true}`)), Header: http.Header{}, Request: r}, nil
		}
		m.paths = append(m.paths, r.Method+" "+r.URL.Path)
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/repos/bifrost/"), "/")
		repo := parts[0]
		if _, known := refs[repo]; !known {
			t.Fatalf("unexpected repository %s", r.URL.Path)
		}
		code, body := http.StatusOK, "[]"
		if r.Method == http.MethodPost {
			if !strings.HasSuffix(r.URL.Path, "/mirror-sync") {
				t.Fatalf("unexpected POST %s", r.URL.Path)
			}
			m.posts[repo]++
			if mode == "recover" {
				m.missing[repo] = false
			}
			if mode == "sync error" {
				code = http.StatusServiceUnavailable
			}
			if mode == "sync timeout" {
				<-r.Context().Done()
				return nil, r.Context().Err()
			}
		} else {
			m.gets[repo]++
			if m.posts[repo] > 0 && (mode == "delayed branch" || mode == "delayed tag" || mode == "delayed commit") {
				m.afterSync[repo]++
				if m.afterSync[repo] >= 3 {
					m.missing[repo] = false
				}
			}
			transient := false
			if m.posts[repo] > 0 && repo == "bifrost-ui" && (mode == "poll error missing" || mode == "poll error recover") && strings.Contains(r.URL.Path, "/git/commits/") {
				m.afterSync[repo]++
				transient = m.afterSync[repo] == 1
				if mode == "poll error recover" && m.afterSync[repo] >= 3 {
					m.missing[repo] = false
				}
			}
			if strings.Contains(r.URL.Path, "/git/commits/") {
				ref := parts[len(parts)-1]
				if ref != refs[repo] {
					t.Fatalf("%s probed ref %s, want %s", repo, ref, refs[repo])
				}
				if transient {
					code = http.StatusInternalServerError
				} else if mode == "platform unknown" && repo == "bifrost-platform" {
					code = http.StatusInternalServerError
				} else if mode == "unknown commit" {
					code = http.StatusServiceUnavailable
				} else if m.missing[repo] {
					code = http.StatusNotFound
				} else {
					body = `{"sha":"` + ref + `"}`
				}
			} else if strings.HasSuffix(r.URL.Path, "/branches") {
				if !m.missing[repo] && !callerParamSHA.MatchString(refs[repo]) && mode != "delayed tag" {
					body = fmt.Sprintf(`[{"name":%q,"commit":{"id":%q}}]`, refs[repo], factSHA1)
				}
				if mode == "partial" {
					code = http.StatusServiceUnavailable
				}
			} else if strings.HasSuffix(r.URL.Path, "/tags") && mode == "delayed tag" && !m.missing[repo] {
				body = fmt.Sprintf(`[{"name":%q,"commit":{"sha":%q}}]`, refs[repo], factSHA1)
			} else if !strings.HasSuffix(r.URL.Path, "/tags") {
				t.Fatalf("unexpected GET %s", r.URL.Path)
			}
		}
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
	})}
	return s, dyn, m
}

func requiredRepoSet(name string) map[string]bool { repos, _ := requiredRepos(name); return repos }

func platformRefs(revision, ui string) map[string]string {
	return map[string]string{"bifrost-platform": revision, "bifrost-ui": ui}
}

func assertPinnedCall(t *testing.T, s *Service, dyn *dynfake.FakeDynamicClient, name, revision string, params map[string]string, sentence string) {
	t.Helper()
	timeout := time.Second
	if sentence != "" {
		timeout = 30 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	resp, _, err := s.StartPipelineRun(ctx, name, revision, "", "tester", params)
	if sentence != "" {
		if err == nil || resp.OK || resp.Changed || !strings.Contains(resp.Message, sentence) {
			t.Fatalf("want refusal %q: %+v %v", sentence, resp, err)
		}
		assertNoContractCreate(t, dyn)
	} else {
		if err != nil || !resp.OK {
			t.Fatal(resp, err)
		}
		createdContractRun(t, dyn)
	}
}

func assertPinnedSync(t *testing.T, m *pinnedMirror, want bool) {
	t.Helper()
	for repo := range m.refs {
		count := m.posts[repo]
		if count > 1 || (want && count != 1) || (!want && count != 0) {
			t.Fatalf("sync %s = %d, want once=%v", repo, count, want)
		}
	}
}

func TestPinnedPlatformDelivery(t *testing.T) {
	for _, name := range []string{"bifrost-deliver-platform", "bifrost-deliver-platform-prod"} {
		for _, tc := range []struct {
			name, revision, ui, mode string
			missing                  []string
			sentence                 string
		}{
			{name: "pinned", revision: factSHA1, ui: factSHA2},
			{name: "main with pinned UI", revision: "main", ui: factSHA2},
			{name: "platform missing", revision: factSHA1, ui: factSHA2, missing: []string{"bifrost-platform"}, sentence: fmt.Sprintf("bifrost-platform (ref %q)", factSHA1)},
			{name: "platform unknown UI missing", revision: factSHA1, ui: factSHA2, mode: "platform unknown", missing: []string{"bifrost-ui"}, sentence: fmt.Sprintf("bifrost-ui (ref %q)", factSHA2)},
			{name: "UI missing", revision: factSHA1, ui: factSHA2, missing: []string{"bifrost-ui"}, sentence: fmt.Sprintf("bifrost-ui (ref %q)", factSHA2)},
			{name: "both missing", revision: factSHA1, ui: factSHA2, missing: []string{"bifrost-platform", "bifrost-ui"}, sentence: fmt.Sprintf("bifrost-platform (ref %q), bifrost-ui (ref %q)", factSHA1, factSHA2)},
			{name: "omit UI", revision: factSHA1, missing: []string{"bifrost-ui"}, sentence: fmt.Sprintf("bifrost-ui (ref %q) — pass uiRevision", factSHA1)},
			{name: "recover after sync", revision: factSHA1, ui: factSHA2, mode: "recover", missing: []string{"bifrost-ui"}},
			{name: "branch recover", revision: "feature/pinned", mode: "recover", missing: []string{"bifrost-platform", "bifrost-ui"}},
			{name: "branch still missing", revision: "feature/pinned", missing: []string{"bifrost-platform"}, sentence: `bifrost-platform (ref "feature/pinned")`},
			{name: "sync error", revision: factSHA1, ui: factSHA2, mode: "sync error", missing: []string{"bifrost-ui"}, sentence: fmt.Sprintf("bifrost-ui (ref %q); mirror sync did not complete", factSHA2)},
			{name: "sync timeout", revision: factSHA1, ui: factSHA2, mode: "sync timeout", missing: []string{"bifrost-ui"}, sentence: fmt.Sprintf("bifrost-ui (ref %q); mirror sync did not complete", factSHA2)},
			{name: "unknown commit", revision: factSHA1, ui: factSHA2, mode: "unknown commit"},
			{name: "partial visibility", revision: factSHA1, ui: factSHA2, mode: "partial", missing: []string{"bifrost-ui"}},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				ui := tc.ui
				if ui == "" {
					ui = tc.revision
				}
				s, dyn, m := pinnedService(t, name, platformRefs(tc.revision, ui), tc.missing, tc.mode)
				params := map[string]string{}
				if tc.ui != "" {
					params["uiRevision"] = tc.ui
				}
				assertPinnedCall(t, s, dyn, name, tc.revision, params, tc.sentence)
				assertPinnedSync(t, m, len(tc.missing) > 0 && tc.mode != "partial" && tc.name != "omit UI")
			})
		}
	}
}

func TestCommitProbeErrorsAreUnknown(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError} {
		s, _, _ := pinnedService(t, "bifrost-deliver-platform", platformRefs(factSHA1, factSHA2), nil, "")
		s.httpClient = &http.Client{Transport: contractTransport(func(r *http.Request) (*http.Response, error) {
			status, body := http.StatusOK, "[]"
			if strings.Contains(r.URL.Path, "/git/commits/") {
				status = code
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		pf := s.RefPreflight(context.Background(), "bifrost-deliver-platform", factSHA1)
		if pf.Reachability != probe.ReachFail || len(pf.Missing) != 0 {
			t.Fatal(pf)
		}
	}
}

func TestPinnedRunBaseline(t *testing.T) {
	raw, err := os.ReadFile("testdata/pinned_runs.json")
	if err != nil {
		t.Fatal(err)
	}
	var baseline map[string]struct {
		Spec     map[string]any `json:"spec"`
		Revision string         `json:"revision"`
	}
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatal(err)
	}
	for name, want := range baseline {
		params := map[string]string{"uiRevision": factSHA2}
		refs := platformRefs(factSHA1, factSHA2)

		s, dyn, m := pinnedService(t, name, refs, nil, "")
		assertPinnedCall(t, s, dyn, name, factSHA1, params, "")
		run := createdContractRun(t, dyn)
		run.SetName("timestamp-name")
		expected := map[string]any{"apiVersion": "tekton.dev/v1", "kind": "PipelineRun", "metadata": map[string]any{"name": "timestamp-name", "namespace": "cicd", "labels": map[string]any{"tekton.dev/pipeline": name, "bifrost.io/trigger": "platform-api", "bifrost.io/revision": want.Revision}}, "spec": want.Spec}
		assertJSONEqual(t, run.Object, expected)
		assertPinnedSync(t, m, false)
	}
}

func TestRunRepoRefsReadsDeclarationDefaults(t *testing.T) {
	pipe := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{"params": []any{map[string]any{"name": "uiRevision", "default": factSHA2}}}}}
	refs := runRepoRefs("bifrost-deliver-platform", factSHA1, map[string]any{}, pipe, []string{"bifrost-platform", "bifrost-ui"})
	assertJSONEqual(t, refs, platformRefs(factSHA1, factSHA2))
}

func TestFullSHAUsesCommitNotSameNamedBranch(t *testing.T) {
	for _, exists := range []bool{true, false} {
		t.Run(fmt.Sprint(exists), func(t *testing.T) {
			s, _, _ := pinnedService(t, "bifrost-deliver-platform", platformRefs(factSHA1, factSHA2), nil, "")
			lookedUp := false
			s.httpClient = &http.Client{Transport: contractTransport(func(r *http.Request) (*http.Response, error) {
				user, pass, ok := r.BasicAuth()
				if !ok || user != "test-user" || pass != "test-pass" {
					t.Fatal("existing basic auth was not used")
				}
				code, body := http.StatusOK, "[]"
				switch {
				case strings.Contains(r.URL.Path, "/git/commits/"):
					lookedUp = true
					// This object may be reachable only from a non-default branch. Commit
					// lookup does not depend on the default branch or listing its tip.
					body = `{"sha":"` + factSHA1 + `"}`
					if !exists {
						code = http.StatusNotFound
					}
				case strings.HasSuffix(r.URL.Path, "/branches"):
					body = fmt.Sprintf(`[{"name":"main","commit":{"id":%q}},{"name":%q,"commit":{"id":%q}}]`, factSHA2, factSHA1, factSHA2)
				}
				return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			st := s.repoRefStatus(context.Background(), "bifrost-platform", factSHA1, "test-user", "test-pass")
			if !lookedUp || st.Exists != exists || (exists && st.Kind != "commit") || (!exists && st.Kind != "missing") {
				t.Fatal(st)
			}
		})
	}
}
