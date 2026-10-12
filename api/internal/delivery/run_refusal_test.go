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

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

type contractTransport func(*http.Request) (*http.Response, error)

func (f contractTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func contractService(t *testing.T, pipe *unstructured.Unstructured, visibility string) (*Service, *dynfake.FakeDynamicClient, *[]string) {
	t.Helper()
	s := NewService(&config.ClusterEntry{ID: "test"})
	s.SetClientsetForTest(k8sfake.NewSimpleClientset())
	dyn := dynfake.NewSimpleDynamicClient(runtime.NewScheme(), pipe)
	s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
	paths := []string{}
	s.httpClient = &http.Client{Transport: contractTransport(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		code, body := http.StatusOK, "[]"
		if visibility == "unknown" {
			return nil, fmt.Errorf("probe unavailable")
		}
		if visibility == "partial" && strings.HasSuffix(r.URL.Path, "/tags") {
			code = http.StatusServiceUnavailable
		}
		if strings.Contains(r.URL.Path, "/git/commits/") {
			if visibility == "present" {
				body = `{"sha":"` + factSHA1 + `"}`
			} else {
				code = http.StatusNotFound
			}
		}
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
	})}
	return s, dyn, &paths
}

func assertJSONEqual(t *testing.T, got, want any) {
	t.Helper()
	g, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	w, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(g) != string(w) {
		t.Fatalf("got %s\nwant %s", g, w)
	}
}

func createdContractRun(t *testing.T, dyn *dynfake.FakeDynamicClient) *unstructured.Unstructured {
	t.Helper()
	for _, action := range dyn.Actions() {
		if action.GetVerb() == "create" {
			create := action.(interface{ GetObject() runtime.Object })
			return create.GetObject().(*unstructured.Unstructured)
		}
	}
	t.Fatal("no create call")
	return nil
}

func assertContractRefused(t *testing.T, s *Service, dyn *dynfake.FakeDynamicClient, name, revision string, extra map[string]string, sentence string) {
	t.Helper()
	resp, _, err := s.StartPipelineRun(context.Background(), name, revision, "", "", extra)
	if err == nil || resp.OK || resp.Changed || !strings.Contains(resp.Message, sentence) {
		t.Fatalf("expected refusal naming %q: %+v, %v", sentence, resp, err)
	}
	for _, action := range dyn.Actions() {
		if action.GetVerb() == "create" {
			t.Fatalf("refusal reached create: %v", action)
		}
	}
}

func TestCIRevisionRefusals(t *testing.T) {
	pipes := infraPipelines(t)
	for _, name := range []string{"bifrost-ci-platform", "bifrost-ci-frontend", "bifrost-ci-python"} {
		for _, value := range []string{"", "main", "feature/branch", factSHA1[:39], factSHA1 + "1", strings.Repeat("A", 40), " " + factSHA1, factSHA1 + "\n"} {
			for _, viaParam := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%q/param=%v", name, value, viaParam), func(t *testing.T) {
					s, dyn, _ := contractService(t, pipes[name], "present")
					extra := map[string]string{}
					if name == "bifrost-ci-python" {
						extra["repo"] = pythonCIRepos[0]
					}
					revision := value
					if viaParam {
						revision = ""
						extra["revision"] = value
					}
					assertContractRefused(t, s, dyn, name, revision, extra, "40-character lowercase git SHA")
				})
			}
		}
	}
}

func TestCIForbiddenParamRefusals(t *testing.T) {
	pipes := infraPipelines(t)
	for _, name := range []string{"bifrost-ci-platform", "bifrost-ci-frontend", "bifrost-ci-python"} {
		for _, param := range []string{"giteaBase", "infraRevision", "uiRevision", "coreRevision", "workerRevision", "apiRevision", "frontendRevision", "registry", "image", "tag", "platformApiImage", "frontendImage", "anything", " revision"} {
			t.Run(name+"/"+param, func(t *testing.T) {
				s, dyn, _ := contractService(t, pipes[name], "present")
				extra := map[string]string{param: factSHA1}
				if name == "bifrost-ci-python" {
					extra["repo"] = pythonCIRepos[0]
				}
				assertContractRefused(t, s, dyn, name, factSHA1, extra, "param "+fmt.Sprintf("%q", param))
			})
		}
	}
	for _, name := range []string{"bifrost-ci-platform", "bifrost-ci-frontend"} {
		s, dyn, _ := contractService(t, pipes[name], "present")
		assertContractRefused(t, s, dyn, name, factSHA1, map[string]string{"repo": "bifrost-trade-core"}, `param "repo"`)
	}
	for _, repo := range []string{"", "bifrost-trade-socket", "bifrost-platform", "bifrost-trade-core/other"} {
		s, dyn, _ := contractService(t, pipes["bifrost-ci-python"], "present")
		assertContractRefused(t, s, dyn, "bifrost-ci-python", factSHA1, map[string]string{"repo": repo}, "param repo")
	}
}

func TestCIRefPreflightOneRepository(t *testing.T) {
	pipes := infraPipelines(t)
	for _, name := range []string{"bifrost-ci-platform", "bifrost-ci-frontend", "bifrost-ci-python"} {
		repos := []string{"bifrost-platform"}
		if name == "bifrost-ci-frontend" {
			repos = []string{"bifrost-trade-frontend"}
		}
		if name == "bifrost-ci-python" {
			repos = pythonCIRepos
		}
		for _, repo := range repos {
			for _, visibility := range []string{"present", "missing", "unknown", "partial"} {
				t.Run(name+"/"+repo+"/"+visibility, func(t *testing.T) {
					s, dyn, paths := contractService(t, pipes[name], visibility)
					extra := map[string]string{"revision": factSHA2}
					if name == "bifrost-ci-python" {
						extra["repo"] = repo
					}
					// The transport accepts commit lookups; verify we probe the effective caller SHA.
					if visibility == "missing" {
						assertContractRefused(t, s, dyn, name, "", extra, "missing in: "+repo)
					} else {
						resp, _, err := s.StartPipelineRun(context.Background(), name, "", "", "", extra)
						if err != nil || !resp.OK {
							t.Fatal(resp, err)
						}
						run := createdContractRun(t, dyn)
						if run.GetLabels()["bifrost.io/revision"] != factSHA2 {
							t.Fatal(run.GetLabels())
						}
					}
					commitProbed := false
					for _, path := range *paths {
						if !strings.HasPrefix(path, "/api/v1/repos/bifrost/"+repo+"/") {
							t.Fatalf("probed another repository: %s", path)
						}
						if strings.HasSuffix(path, "/git/commits/"+factSHA2) {
							commitProbed = true
						}
					}
					if !commitProbed {
						t.Fatal("effective revision was not probed")
					}
				})
			}
		}
	}
}

func TestPipelineDeclarationRefusals(t *testing.T) {
	cases := []struct {
		name     string
		spec     any
		sentence string
	}{
		{"workspace", map[string]any{"workspaces": []any{map[string]any{"name": "owed-source"}}}, `workspace "owed-source"`},
		{"param", map[string]any{"params": []any{map[string]any{"name": "owed-param"}}}, `param "owed-param"`},
		{"bad spec", "not-a-spec", "spec cannot be read"},
		{"bad workspaces", map[string]any{"workspaces": "bad"}, "workspaces cannot be read"},
		{"bad params", map[string]any{"params": map[string]any{}}, "params cannot be read"},
		{"null workspaces", map[string]any{"workspaces": nil}, "workspaces cannot be read"},
		{"bad item", map[string]any{"params": []any{"bad"}}, "params cannot be read"},
		{"missing name", map[string]any{"params": []any{map[string]any{"type": "string"}}}, "params name cannot be read"},
		{"bad type", map[string]any{"params": []any{map[string]any{"name": "source", "type": true}}}, "type declaration cannot be read"},
		{"bad default", map[string]any{"params": []any{map[string]any{"name": "source", "default": nil}}}, "default declaration cannot be read"},
		{"duplicate param", map[string]any{"params": []any{map[string]any{"name": "source"}, map[string]any{"name": "source"}}}, "params name cannot be read"},
		{"bad optional", map[string]any{"workspaces": []any{map[string]any{"name": "source", "optional": "true"}}}, "optional declaration cannot be read"},
	}
	for _, name := range []string{"unknown", "bifrost-ci-platform", "bifrost-smoke"} {
		for _, tc := range cases {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				pipe := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "Pipeline", "metadata": map[string]any{"name": name, "namespace": "cicd"}, "spec": tc.spec}}
				s, dyn, _ := contractService(t, pipe, "unknown")
				assertContractRefused(t, s, dyn, name, factSHA1, nil, tc.sentence)
			})
		}
	}
	t.Run("absent spec", func(t *testing.T) {
		pipe := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "Pipeline", "metadata": map[string]any{"name": "unknown", "namespace": "cicd"}}}
		s, dyn, _ := contractService(t, pipe, "unknown")
		assertContractRefused(t, s, dyn, "unknown", factSHA1, nil, "spec cannot be read")
	})
}

func TestOptionalWorkspaceAndDefaultParamAllowed(t *testing.T) {
	pipe := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{
		"workspaces": []any{map[string]any{"name": "optional", "optional": true}},
		"params":     []any{map[string]any{"name": "defaulted", "default": ""}},
	}}}
	if err := validateRunRequirements(pipe, &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}); err != nil {
		t.Fatal(err)
	}
}

func TestCIRunsIgnoreReleaseWindow(t *testing.T) {
	for name, pipe := range infraPipelines(t) {
		if !isCIPipeline(name) {
			continue
		}
		t.Run(name, func(t *testing.T) {
			s, dyn, _ := contractService(t, pipe, "present")
			s.SetClientsetForTest(k8sfake.NewSimpleClientset(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: releaseWindowConfigMap, Namespace: "cicd"}, Data: map[string]string{releaseWindowDataKey: `{"who":"another-owner","what":"bifrost-platform"}`}}))
			extra := map[string]string{}
			if name == "bifrost-ci-python" {
				extra["repo"] = pythonCIRepos[0]
			}
			resp, _, err := s.StartPipelineRun(context.Background(), name, factSHA1, "", "", extra)
			if err != nil || !resp.OK {
				t.Fatal(resp, err)
			}
			createdContractRun(t, dyn)
		})
	}
}

// Compare the complete created object to main's spec baseline. Only the
// timestamp-derived name is normalized; every other field is byte compared.
func TestNonCICreatedRunsUnchangedFromMain(t *testing.T) {
	raw, err := os.ReadFile("testdata/non_ci_runs_main.json")
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
	pipes := baselinePipelines(t)
	for key, want := range baseline {
		parts := strings.Split(key, "/")
		name := parts[0]
		// This name has never had a workspace case; it is intentionally now refused.
		if name == "bifrost-clone-frontend-smoke" {
			continue
		}
		t.Run(key, func(t *testing.T) {
			tag := ""
			if parts[1] == "tagged" {
				tag = "1.2.3"
			}
			pipe := pipes[name]
			if pipe == nil {
				pipe = &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "Pipeline", "metadata": map[string]any{"name": name, "namespace": "cicd"}, "spec": map[string]any{}}}
			}
			s, dyn, _ := contractService(t, pipe, "unknown")
			cs := k8sfake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "ci", Labels: map[string]string{"kubernetes.io/arch": "amd64"}}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}})
			if repos, known := requiredRepos(name); known {
				b, err := json.Marshal(map[string]any{"who": "tester", "what": sortedKeys(repos)})
				if err != nil {
					t.Fatal(err)
				}
				_, err = cs.CoreV1().ConfigMaps("cicd").Create(context.Background(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: releaseWindowConfigMap}, Data: map[string]string{releaseWindowDataKey: string(b)}}, metav1.CreateOptions{})
				if err != nil {
					t.Fatal(err)
				}
			}
			s.SetClientsetForTest(cs)
			resp, _, err := s.StartPipelineRun(context.Background(), name, factSHA1, tag, "tester", nil)
			if err != nil || !resp.OK {
				t.Fatal(resp, err)
			}
			run := createdContractRun(t, dyn)
			if !strings.HasPrefix(run.GetName(), name+"-") {
				t.Fatal(run.GetName())
			}
			run.SetName("timestamp-name")
			expected := map[string]any{"apiVersion": "tekton.dev/v1", "kind": "PipelineRun", "metadata": map[string]any{"name": "timestamp-name", "namespace": "cicd", "labels": map[string]any{"tekton.dev/pipeline": name, "bifrost.io/trigger": "platform-api", "bifrost.io/revision": want.Revision}}, "spec": want.Spec}
			assertJSONEqual(t, run.Object, expected)
		})
	}
}

func TestDeliverPipelineDeclarationRefusals(t *testing.T) {
	const name = "bifrost-deliver-platform-prod"
	for _, tc := range []struct {
		field, owed, sentence string
	}{
		{"workspaces", "owed-source", `workspace "owed-source"`},
		{"params", "owed-param", `param "owed-param"`},
	} {
		t.Run(tc.field, func(t *testing.T) {
			pipe := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "tekton.dev/v1", "kind": "Pipeline",
				"metadata": map[string]any{"name": name, "namespace": "cicd"},
				"spec":     map[string]any{tc.field: []any{map[string]any{"name": tc.owed}}},
			}}
			s, dyn, _ := contractService(t, pipe, "unknown")
			s.SetClientsetForTest(k8sfake.NewSimpleClientset(
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "ci", Labels: map[string]string{"kubernetes.io/arch": "amd64"}}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: releaseWindowConfigMap, Namespace: "cicd"}, Data: map[string]string{releaseWindowDataKey: `{"who":"tester","what":"bifrost-platform"}`}},
			))
			resp, _, err := s.StartPipelineRun(context.Background(), name, factSHA1, "", "tester", nil)
			if err == nil || resp.OK || resp.Changed || !strings.Contains(resp.Message, tc.sentence) {
				t.Fatalf("expected refusal naming %q: %+v, %v", tc.sentence, resp, err)
			}
			assertNoContractCreate(t, dyn)
		})
	}
}

func assertNoContractCreate(t *testing.T, dyn *dynfake.FakeDynamicClient) {
	t.Helper()
	for _, action := range dyn.Actions() {
		if action.GetVerb() == "create" {
			t.Fatalf("refusal reached create: %v", action)
		}
	}
}

func TestCIContradictoryInputsRefused(t *testing.T) {
	for _, name := range []string{"bifrost-ci-platform", "bifrost-ci-frontend", "bifrost-ci-python"} {
		for _, tc := range []struct {
			name, tag string
			extra     map[string]string
			words     []string
		}{
			{"revision conflict", "", map[string]string{"revision": factSHA2}, []string{"revision", "params.revision", factSHA1, factSHA2}},
			{"image tag", "1.2.3", nil, []string{"tag", "1.2.3", "CI builds no image"}},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				pipe := &unstructured.Unstructured{Object: map[string]any{
					"apiVersion": "tekton.dev/v1", "kind": "Pipeline",
					"metadata": map[string]any{"name": name, "namespace": "cicd"}, "spec": map[string]any{},
				}}
				extra := map[string]string{}
				for k, v := range tc.extra {
					extra[k] = v
				}
				if name == "bifrost-ci-python" {
					extra["repo"] = pythonCIRepos[0]
				}
				s, dyn, paths := contractService(t, pipe, "present")
				resp, _, err := s.StartPipelineRun(context.Background(), name, factSHA1, tc.tag, "", extra)
				if err == nil || resp.OK || resp.Changed {
					t.Fatalf("expected refusal: %+v, %v", resp, err)
				}
				for _, word := range tc.words {
					if !strings.Contains(resp.Message, word) {
						t.Fatalf("refusal missing %q: %s", word, resp.Message)
					}
				}
				assertNoContractCreate(t, dyn)
				if len(*paths) != 0 {
					t.Fatal("invalid inputs reached preflight", *paths)
				}
				if _, _, err := buildPipelineRunSpec(name, factSHA1, tc.tag, extra, pipe); err == nil {
					t.Fatal("builder accepted contradictory CI inputs")
				}
			})
		}
	}
}

func TestPythonCIRefPreflightResponse(t *testing.T) {
	s := NewService(&config.ClusterEntry{ID: "test"})
	before := time.Now().UTC()
	resp := s.RefPreflight(context.Background(), "bifrost-ci-python", factSHA1)
	if resp.Ready || resp.Reachability != probe.ReachFail || !strings.Contains(resp.Detail, "param repo") || resp.ClusterID != "test" {
		t.Fatal(resp)
	}
	if resp.GeneratedAt.Before(before) || resp.GeneratedAt.After(time.Now().UTC()) {
		t.Fatal("invalid timestamp", resp.GeneratedAt)
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"repos", "missing"} {
		if list, ok := fields[key].([]any); !ok || len(list) != 0 {
			t.Fatalf("%s must be an empty list: %s", key, b)
		}
	}
	if fields["generated_at"] == "0001-01-01T00:00:00Z" {
		t.Fatal(string(b))
	}
}
