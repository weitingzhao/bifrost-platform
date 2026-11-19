//bifrost:owner delivery

package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/placement"
)

type contractTransport func(*http.Request) (*http.Response, error)

func (f contractTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func contractService(t *testing.T, pipe *unstructured.Unstructured, visibility string) (*Service, *dynfake.FakeDynamicClient, *[]string) {
	t.Helper()
	s := NewService(&config.ClusterEntry{ID: "test"})
	// One Ready amd64 node: kaniko builds (bifrost-build-ci-infra-tools among
	// them, review 788 C1) pass the build preflight here; the no-node refusal
	// has its own test.
	s.SetClientsetForTest(k8sfake.NewSimpleClientset(readyAmd64Node()))
	// The Tasks infra declares: whether a pipeline runs kaniko is read through them (2026-11-17, proposal B).
	dyn := dynfake.NewSimpleDynamicClient(runtime.NewScheme(), append([]runtime.Object{pipe}, infraTaskObjects(t)...)...)
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
		// Round 2 ruling 3 refuses these frozen cases: the same full SHA
		// is used for both the platform and UI repository.
		switch key {
		case "bifrost-deliver-platform/empty": // STG, no image tag, identical SHAs.
			continue
		case "bifrost-deliver-platform/tagged": // STG, image tag, identical SHAs.
			continue
		case "bifrost-deliver-platform-prod/empty": // PROD, no image tag, identical SHAs.
			continue
		case "bifrost-deliver-platform-prod/tagged": // PROD, image tag, identical SHAs.
			continue
		}
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
			if mainKanikoNames[name] {
				pipe = withKanikoStep(pipe)
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
				if _, _, err := buildPipelineRunSpec(name, factSHA1, tc.tag, extra, pipe, false); err == nil {
					t.Fatal("builder accepted contradictory CI inputs")
				}
			})
		}
	}
}

// mainKanikoNames is the hand-kept list main 2726c47 and 26a10b5 knew. The
// frozen baselines (non_ci_runs_main.json, the ref-check parity) enumerate the
// pipelines it named and were recorded with the amd64 template on each; they
// pin the run builder's output given that flag, not which pipelines get it,
// which kanikoOf reads from the definitions now (2026-11-17, proposal B).
var mainKanikoNames = map[string]bool{
	"bifrost-deliver-stg": true, "bifrost-deliver-prod": true, "bifrost-deliver-platform": true,
	"bifrost-deliver-platform-prod": true, "bifrost-deliver-research": true, "bifrost-build-stg": true,
	"bifrost-build-frontend-stg": true, "bifrost-build-research-dagster": true, "bifrost-build-market-data": true,
	"bifrost-build-flex-query": true, "bifrost-build-ib-gateway": true, "bifrost-build-ci-infra-tools": true,
}

// withKanikoStep: a copy of a baseline pipeline that runs the executor. The
// main baselines hold pipelines without their build steps; their frozen runs
// were recorded while these names were kaniko pipelines (2026-11-17, proposal B).
func withKanikoStep(pipe *unstructured.Unstructured) *unstructured.Unstructured {
	out := pipe.DeepCopy()
	tasks, _, _ := unstructured.NestedSlice(out.Object, "spec", "tasks")
	tasks = append(tasks, map[string]any{"name": "baseline-kaniko", "taskSpec": map[string]any{"steps": []any{
		map[string]any{"name": "build", "image": "gcr.io/kaniko-project/executor:v1.23.2"}}}})
	_ = unstructured.SetNestedSlice(out.Object, tasks, "spec", "tasks")
	return out
}

// infraTaskObjects reads the Tasks infra declares (k8s/cicd/tekton/task-*.yaml).
func infraTaskObjects(t *testing.T) []runtime.Object {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(infraTektonDir(t), "task-*.yaml"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("task definitions: %v", err)
	}
	var out []runtime.Object
	for _, path := range paths {
		for _, obj := range readYAMLObjects(t, path) {
			if obj.GetKind() == "Task" {
				obj.SetNamespace("cicd")
				out = append(out, obj)
			}
		}
	}
	return out
}

// infraTasks looks a Task up among those infra declares.
func infraTasks(t *testing.T) taskGetter {
	t.Helper()
	tasks := map[string]map[string]any{}
	for _, obj := range infraTaskObjects(t) {
		u := obj.(*unstructured.Unstructured)
		tasks[u.GetName()] = u.Object
	}
	return func(name string) (map[string]any, error) {
		if obj, ok := tasks[name]; ok {
			return obj, nil
		}
		return nil, fmt.Errorf("task %s not declared", name)
	}
}

// kanikoInInfra: whether infra's declaration of a pipeline runs kaniko, as the
// platform reads it from the cluster.
func kanikoInInfra(t *testing.T, pipe *unstructured.Unstructured) bool {
	t.Helper()
	if pipe == nil {
		return false
	}
	kaniko, _ := kanikoOf(pipe.Object, infraTasks(t))
	return kaniko
}

// 2026-11-17, Owner approved proposal B (duty/proposal-ci-w1-2026-11-17-kaniko-gate-list.md):
// the hand-kept list missed bifrost-build-research-pine and bifrost-ci-finance,
// both kaniko builds, and named bifrost-deliver-platform-prod, which builds
// nothing. Read from infra's declarations, the three come out right, and the
// pipelines that run kaniko through a Task (deliver-stg) are still found.
func TestKanikoIsReadFromThePipelineDefinition(t *testing.T) {
	pipes := infraPipelines(t)
	tasks := infraTasks(t)
	want := map[string]bool{
		"bifrost-build-research-pine":   true,
		"bifrost-deliver-stg":           true,
		"bifrost-deliver-platform":      true,
		"bifrost-deliver-research":      true,
		"bifrost-build-ci-infra-tools":  true,
		"bifrost-deliver-platform-prod": false,
		"bifrost-ci-platform":           false,
	}
	for name, kaniko := range want {
		pipe := pipes[name]
		if pipe == nil {
			t.Fatalf("%s is not declared in infra", name)
		}
		got, err := pipelineRunsKaniko(pipe.Object, tasks)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != kaniko {
			t.Errorf("%s: kaniko = %v, want %v", name, got, kaniko)
		}
	}
	// bifrost-ci-finance is declared outside infra; its build task runs the
	// executor inline, as in the cluster on 2026-11-17.
	finance := map[string]any{"spec": map[string]any{"tasks": []any{map[string]any{
		"name": "build", "taskSpec": map[string]any{"steps": []any{
			map[string]any{"name": "build", "image": "gcr.io/kaniko-project/executor:v1.23.2"}}}}}}}
	if got, err := pipelineRunsKaniko(finance, tasks); err != nil || !got {
		t.Fatalf("an inline executor step is a kaniko build: %v %v", got, err)
	}
	// An image given as a task parameter is read through the parameter default.
	param := map[string]any{"spec": map[string]any{"tasks": []any{map[string]any{
		"name": "build", "taskSpec": map[string]any{
			"params": []any{map[string]any{"name": "builder", "default": "gcr.io/kaniko-project/executor:v1.23.2"}},
			"steps":  []any{map[string]any{"name": "build", "image": "$(params.builder)"}}}}}}}
	if got, _ := pipelineRunsKaniko(param, tasks); !got {
		t.Fatal("an executor image given through a parameter default was missed")
	}
}

// Same origin: a definition the platform cannot read still gets the amd64
// check, and the refusal says the definition was not read.
func TestAnUnreadDefinitionIsCheckedAndNamed(t *testing.T) {
	missing := func(string) (map[string]any, error) { return nil, fmt.Errorf("forbidden") }
	pipe := map[string]any{"spec": map[string]any{"tasks": []any{map[string]any{"name": "build", "taskRef": map[string]any{"name": "some-build"}}}}}
	kaniko, unread := kanikoOf(pipe, missing)
	if !kaniko || !strings.Contains(unread, "not read") {
		t.Fatalf("an unread Task must count as kaniko and say so: %v %q", kaniko, unread)
	}
	resolved := map[string]any{"spec": map[string]any{"tasks": []any{map[string]any{"name": "x", "taskRef": map[string]any{"resolver": "git"}}}}}
	if kaniko, _ := kanikoOf(resolved, missing); !kaniko {
		t.Fatal("a resolver taskRef is not read here and must count as kaniko")
	}

	s, _, _ := contractService(t, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tekton.dev/v1", "kind": "Pipeline",
		"metadata": map[string]any{"name": "other", "namespace": "cicd"}, "spec": map[string]any{}}}, "present")
	s.SetClientsetForTest(k8sfake.NewSimpleClientset())
	// The check a run start makes (StartPipelineRun): the route that ran it on
	// its own went on 2026-11-19 (no traffic, no caller).
	pf := s.buildPreflight(context.Background(), "bifrost-unread", kaniko, unread)
	if pf.BuildReady || !strings.Contains(pf.Reason, "not read") {
		t.Fatalf("with no amd64 node an unread pipeline must be refused and say why: %+v", pf)
	}
	other, why := kanikoOf(map[string]any{"spec": map[string]any{}}, missing)
	pf = s.buildPreflight(context.Background(), "other", other, why)
	if !pf.BuildReady {
		t.Fatalf("a pipeline that runs no kaniko is not held by the amd64 check: %+v", pf)
	}
}

// 2026-11-17, with the communication layer (no bus event carried a cause, so
// git.main.advanced -> the run -> release.deploy.finished could not be
// followed): a start made with a cause names it on the run, with the
// environment whose ledger holds it (event ids are per ledger); anything that
// is not a ledger event's bus id, or no environment, names nothing. Positive control: without the
// LabelCause line in StartPipelineRun the first run has no label.
func TestAStartNamesTheEventThatCausedIt(t *testing.T) {
	pipe := infraPipelines(t)["bifrost-ci-platform"]
	for _, tc := range []struct{ id, env, want, wantEnv string }{
		{"ev-42", "prod", "ev-42", "prod"}, {"42", "prod", "", ""}, {"ev-x", "prod", "", ""}, {"", "prod", "", ""}, {"ev-42", "", "", ""},
	} {
		s, dyn, _ := contractService(t, pipe, "present")
		resp, _, err := s.StartPipelineRun(WithCause(context.Background(), tc.id, tc.env), "bifrost-ci-platform", factSHA1, "", "", nil)
		if err != nil || !resp.OK {
			t.Fatal(resp, err)
		}
		labels := createdContractRun(t, dyn).GetLabels()
		if labels[LabelCause] != tc.want || labels[LabelCauseEnv] != tc.wantEnv {
			t.Fatalf("cause %q env %q: labels %q %q, want %q %q", tc.id, tc.env, labels[LabelCause], labels[LabelCauseEnv], tc.want, tc.wantEnv)
		}
	}
}

// 2026-11-17, with the communication layer: a run started over HTTP names who
// asked for it, the calling thread's actor from its X-Bifrost-Session header,
// as an annotation (a thread actor does not fit a label value); a header that
// is not a session id names nobody. Positive control: without the
// WithRequester line in HandleStartPipelineRun the first run has no
// annotation.
func TestAStartOverHTTPNamesWhoAskedForIt(t *testing.T) {
	pipe := infraPipelines(t)["bifrost-ci-platform"]
	for _, tc := range []struct{ header, want string }{
		{"local_7de640c4-4b19-4a71-a0ac-ced2ce5fc7ff", "thread:local_7de640c4-4b19-4a71-a0ac-ced2ce5fc7ff"},
		{"two words", ""}, {"", ""},
	} {
		s, dyn, _ := contractService(t, pipe, "present")
		h := NewHandlerForTest(s, nil)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/delivery/pipelines/bifrost-ci-platform/runs",
			strings.NewReader(`{"revision":"`+factSHA1+`"}`))
		req.Header.Set("X-Bifrost-Session", tc.header)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "bifrost-ci-platform")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		h.HandleStartPipelineRun(rec, req)
		if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
			t.Fatalf("%q: %d %s", tc.header, rec.Code, rec.Body.String())
		}
		if got := createdContractRun(t, dyn).GetAnnotations()[AnnRequester]; got != tc.want {
			t.Fatalf("header %q: requester %q, want %q", tc.header, got, tc.want)
		}
	}
}

func placementNode(host string, ready, cordoned bool, cpu string) *corev1.Node {
	status := corev1.ConditionTrue
	if !ready {
		status = corev1.ConditionFalse
	}
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: host, Labels: map[string]string{hostIDLabel: host, "kubernetes.io/arch": "amd64"}},
		Spec:       corev1.NodeSpec{Unschedulable: cordoned},
		Status: corev1.NodeStatus{
			Addresses:   []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "ip-" + host}},
			Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpu), corev1.ResourceMemory: resource.MustParse("16Gi")},
			Conditions:  []corev1.NodeCondition{{Type: corev1.NodeReady, Status: status}},
		},
	}
}

// 2026-11-18, the coordinator: the daily unit run does not land on 05, where two
// tasks timed out. Unlike a release it can wait, so the slow disk is refused.
// Positive control: MaxDiskTier 2 in UnitTask picks 05 here.
func TestUnitsAreNeverPlacedOnTheSlowDisk(t *testing.T) {
	s := NewService(&config.ClusterEntry{ID: "test"})
	s.SetClientsetForTest(k8sfake.NewSimpleClientset(placementNode("ubt-k3s-05", true, false, "16")))
	s.SetNodeMetricsReader(func(context.Context) (map[string]NodeMetrics, error) {
		return map[string]NodeMetrics{"ip-ubt-k3s-05": {WriteLatencySeconds: 0.161, MemoryAvailable: 0.6}}, nil
	})
	p, measured := s.PlaceTask(context.Background(), UnitTask)
	if !measured || p.Node != "" || !strings.Contains(p.Why, "too slow") {
		t.Fatalf("placement = %+v %v", p, measured)
	}
}

// Same origin: metrics that cannot be read leave the run to the scheduler and
// say so, and a platform with no reader changes nothing about its runs.
// Positive control: treating the error as an empty answer loses "could not be read".
func TestBuildPlacementWithoutMetricsLeavesTheRunAlone(t *testing.T) {
	s := NewService(&config.ClusterEntry{ID: "test"})
	s.SetClientsetForTest(k8sfake.NewSimpleClientset(placementNode("ubt-k3s-07", true, false, "16")))
	spec := map[string]any{}
	if _, placed := s.placeBuild(context.Background(), "bifrost-deliver-platform", spec); placed || len(spec) != 0 {
		t.Fatalf("placed without a reader: %v", spec)
	}
	s.SetNodeMetricsReader(func(context.Context) (map[string]NodeMetrics, error) { return nil, errors.New("prometheus down") })
	p, placed := s.placeBuild(context.Background(), "bifrost-deliver-platform", spec)
	if !placed || p.Node != "" || !strings.Contains(p.Why, "could not be read") || len(spec) != 0 {
		t.Fatalf("placement = %+v %v, spec %v", p, placed, spec)
	}
}

// Same origin: the choice reaches the run, beside the amd64 selector kaniko
// needs; another pipeline's run is left alone.
func TestAStartPinsOnlyThePlacedPipeline(t *testing.T) {
	s := NewService(&config.ClusterEntry{ID: "test"})
	s.SetClientsetForTest(k8sfake.NewSimpleClientset(placementNode("ubt-k3s-07", true, false, "16")))
	s.SetNodeMetricsReader(func(context.Context) (map[string]NodeMetrics, error) {
		return map[string]NodeMetrics{"ip-ubt-k3s-07": {WriteLatencySeconds: 0.007, MemoryAvailable: 0.6}}, nil
	})
	spec := map[string]any{"taskRunTemplate": amd64CITaskRunTemplate()}
	p, placed := s.placeBuild(context.Background(), "bifrost-deliver-platform", spec)
	if !placed || p.Node != "ubt-k3s-07" {
		t.Fatalf("placement = %+v %v", p, placed)
	}
	selector := spec["taskRunTemplate"].(map[string]any)["podTemplate"].(map[string]any)["nodeSelector"].(map[string]any)
	if selector[hostIDLabel] != "ubt-k3s-07" || selector["kubernetes.io/arch"] != "amd64" {
		t.Fatalf("selector = %v", selector)
	}
	other := map[string]any{}
	if _, placed := s.placeBuild(context.Background(), "bifrost-ci-platform", other); placed || len(other) != 0 {
		t.Fatalf("another pipeline was placed: %v", other)
	}
}

// 2026-11-18, the Owner: CI on 05 moves to 07, the slow disk only as a fallback.
// A CI run is created by the webhook's trigger, so the platform writes the choice
// into the binding it reads; only a change is written, and a binding that is not
// there is no error. Positive control: writing on every pass fails the count of
// patches; a choice left out of the patch fails the value checks.
func TestPublishCINodeWritesTheChoiceWhenItChanges(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	binding := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "triggers.tekton.dev/v1beta1", "kind": "TriggerBinding",
		"metadata": map[string]any{"name": ciNodeBinding, "namespace": "cicd"},
		"spec":     map[string]any{"params": []any{map[string]any{"name": "ciNode", "value": ciNoNode}}}}}
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{triggerBindingGVR: "TriggerBindingList"}, binding)
	s := NewService(&config.ClusterEntry{ID: "test"})
	s.SetClientsetForTest(k8sfake.NewSimpleClientset(placementNode("ubt-k3s-07", true, false, "16"), placementNode("ubt-k3s-05", true, false, "16")))
	s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
	metrics := map[string]NodeMetrics{
		"ip-ubt-k3s-07": {WriteLatencySeconds: 0.007, CPUBusy: 0.2, DiskBusy: 0.1, MemoryAvailable: 0.6},
		"ip-ubt-k3s-05": {WriteLatencySeconds: 0.16, CPUBusy: 0.2, DiskBusy: 0.1, MemoryAvailable: 0.6},
	}
	s.SetNodeMetricsReader(func(context.Context) (map[string]NodeMetrics, error) { return metrics, nil })
	read := func(name string) string {
		got, err := dyn.Resource(triggerBindingGVR).Namespace(s.PipelinesNamespace()).Get(ctx, ciNodeBinding, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return boundParam(got.Object, name)
	}
	patches := func() (n int) {
		for _, a := range dyn.Actions() {
			if a.GetVerb() == "patch" {
				n++
			}
		}
		return n
	}
	for i := 0; i < 2; i++ {
		if err := s.PublishCINode(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if read("ciNode") != "ubt-k3s-07" || read("ciNodeKey") != hostIDLabel || read("ciNodeValue") != "ubt-k3s-07" || patches() != 1 {
		t.Fatalf("node %q key %q value %q patches %d", read("ciNode"), read("ciNodeKey"), read("ciNodeValue"), patches())
	}
	m := metrics["ip-ubt-k3s-07"]
	m.CPUBusy = 0.9
	metrics["ip-ubt-k3s-07"] = m
	if err := s.PublishCINode(ctx); err != nil || read("ciNode") != "ubt-k3s-05" || !strings.Contains(read("ciNodeWhy"), "CPU 90% busy") {
		t.Fatalf("%v node %q why %q", err, read("ciNode"), read("ciNodeWhy"))
	}
	metrics = map[string]NodeMetrics{}
	if err := s.PublishCINode(ctx); err != nil || read("ciNode") != ciNoNode || read("ciNodeKey") != ciAnyNodeKey || read("ciNodeValue") != ciAnyNodeValue {
		t.Fatalf("nothing fits leaves the run to the scheduler: %v %q", err, read("ciNode"))
	}
}

// 2026-11-19, the compute line: ubt-k3s-02 has no memory for the test database a CI
// run now keeps in memory, so runs leave it even when it is the quietest. A build
// holds no such database and may still go there. Positive control: the same nodes
// under CITask choose 02.
func TestACIRunStaysOffTheNodeWithoutMemoryToSpare(t *testing.T) {
	s := NewService(&config.ClusterEntry{ID: "test"})
	s.SetClientsetForTest(k8sfake.NewSimpleClientset(labelled(placementNode("ubt-k3s-02", true, false, "16"), placement.PostgresPrimary), placementNode("ubt-k3s-04", true, false, "16")))
	s.SetNodeMetricsReader(func(context.Context) (map[string]NodeMetrics, error) {
		return map[string]NodeMetrics{
			"ip-ubt-k3s-02": {WriteLatencySeconds: 0.001, CPUBusy: 0.05, DiskBusy: 0.05, MemoryAvailable: 0.6},
			"ip-ubt-k3s-04": {WriteLatencySeconds: 0.001, CPUBusy: 0.5, DiskBusy: 0.1, MemoryAvailable: 0.6},
		}, nil
	})
	ciRun := CIRunTask
	ciRun.MinAvailableMemory = 0 // the fake nodes report little memory; the floor has its own test
	if p, _ := s.placeTask(context.Background(), ciRun, ""); p.Node != "ubt-k3s-04" {
		t.Fatalf("run: %+v", p)
	}
	if p, _ := s.placeTask(context.Background(), CITask, ""); p.Node != "ubt-k3s-02" {
		t.Fatalf("build: %+v", p)
	}
}

// Same origin: with no node chosen a run is left to the scheduler, so the nodes it
// must avoid are a hard rule in the pod template too. Positive control: the plain
// template has no affinity.
func TestAPlatformCIRunIsKeptOffTheExcludedNodes(t *testing.T) {
	if _, ok := amd64CITaskRunTemplate()["podTemplate"].(map[string]any)["affinity"]; ok {
		t.Fatal("the plain template has an affinity")
	}
	// 2026-11-19: by label, not node name (the etcd node and the database primary,
	// today ubt-k3s-01 and ubt-k3s-02): one term, both expressions must hold.
	got, err := json.Marshal(ciRunTaskRunTemplate()["podTemplate"].(map[string]any)["affinity"])
	if err != nil {
		t.Fatal(err)
	}
	want := `{"nodeAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":{"nodeSelectorTerms":[{"matchExpressions":[` +
		`{"key":"node-role.kubernetes.io/etcd","operator":"NotIn","values":["true"]},` +
		`{"key":"bifrost.io/postgres-role","operator":"NotIn","values":["primary"]}]}]}}}`
	if string(got) != want {
		t.Fatalf("affinity %s, want %s", got, want)
	}
}

// labelled gives the node the rules' labels.
func labelled(n *corev1.Node, rules ...placement.LabelRule) *corev1.Node {
	for _, r := range rules {
		n.Labels[r.Key] = r.Value
	}
	return n
}

// Origin: 2026-11-19, node names became labels (CI line ask). The etcd node and the
// database primary are excluded by their labels, so a node renamed or replaced keeps
// the rule, and a node that only has the old name is no longer excluded.
// Positive control: excluding by the name ubt-k3s-01 instead makes the first check
// choose cp-renamed (the quieter node) and the second refuse ubt-k3s-01.
func TestCINeverGoesToTheEtcdNodeWhateverItIsCalled(t *testing.T) {
	place := func(task TaskProfile, nodes ...runtime.Object) BuildPlacement {
		s := NewService(&config.ClusterEntry{ID: "test"})
		s.SetClientsetForTest(k8sfake.NewSimpleClientset(nodes...))
		s.SetNodeMetricsReader(func(context.Context) (map[string]NodeMetrics, error) {
			return map[string]NodeMetrics{
				"ip-cp-renamed": {WriteLatencySeconds: 0.001, CPUBusy: 0.05, DiskBusy: 0.05, MemoryAvailable: 0.6},
				"ip-db-renamed": {WriteLatencySeconds: 0.001, CPUBusy: 0.05, DiskBusy: 0.05, MemoryAvailable: 0.6},
				"ip-ubt-k3s-01": {WriteLatencySeconds: 0.001, CPUBusy: 0.5, DiskBusy: 0.1, MemoryAvailable: 0.6},
				"ip-ubt-k3s-02": {WriteLatencySeconds: 0.001, CPUBusy: 0.5, DiskBusy: 0.1, MemoryAvailable: 0.6},
			}, nil
		})
		p, _ := s.placeTask(context.Background(), task, "")
		return p
	}
	cp := func() *corev1.Node {
		return labelled(placementNode("cp-renamed", true, false, "16"), placement.EtcdNode)
	}
	old := func() *corev1.Node { return placementNode("ubt-k3s-01", true, false, "16") }
	for name, task := range map[string]TaskProfile{"CITask": CITask, "CIRunTask": CIRunTask, "UnitTask": UnitTask} {
		task.MinAvailableMemory = 0 // the fake nodes report little memory
		if p := place(task, cp(), old()); p.Node != "ubt-k3s-01" {
			t.Fatalf("%s: %+v, want the etcd-labelled node excluded and ubt-k3s-01 chosen", name, p)
		}
	}
	db := labelled(placementNode("db-renamed", true, false, "16"), placement.PostgresPrimary)
	run := CIRunTask
	run.MinAvailableMemory = 0
	if p := place(run, db, placementNode("ubt-k3s-02", true, false, "16")); p.Node != "ubt-k3s-02" {
		t.Fatalf("CIRunTask: %+v, want the primary-labelled node excluded and ubt-k3s-02 chosen", p)
	}
}

// 2026-11-18 (Law 39): migration 0035 held every PROD delivery until someone asked
// for the run by hand, though the signed policy covered it. The hook runs before a
// platform PROD delivery and no other. Positive control: dropping the pipeline
// check makes the second start call it.
func TestOnlyAPlatformProdStartPrepares(t *testing.T) {
	pipe := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "Pipeline",
		"metadata": map[string]any{"name": prodPlatformPipeline, "namespace": "cicd"}, "spec": map[string]any{}}}
	s, _, _ := contractService(t, pipe, "present")
	var got []string
	s.SetProdPrepare(func(_ context.Context, revision string) { got = append(got, revision) })
	_, _, _ = s.StartPipelineRun(context.Background(), prodPlatformPipeline, factSHA1, "", "", nil)
	_, _, _ = s.StartPipelineRun(context.Background(), "bifrost-ci-platform", factSHA1, "", "", nil)
	if len(got) != 1 || got[0] != factSHA1 {
		t.Fatalf("prepared %v", got)
	}
}
