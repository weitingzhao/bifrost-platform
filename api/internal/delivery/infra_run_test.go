package delivery

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func infraRunPipelines(t *testing.T) map[string]*unstructured.Unstructured {
	t.Helper()
	pipes := map[string]*unstructured.Unstructured{}
	for _, obj := range readYAMLObjects(t, "testdata/infra_pipelines.yaml") {
		pipes[obj.GetName()] = obj
	}
	return pipes
}

func infraLandingParams() map[string]string {
	return map[string]string{"revision": factSHA1, "base": factSHA2, "platformRevision": strings.Repeat("c", 40), "generation": "1", "purpose": "landing"}
}

func TestInfraPipelineRunsMatchDeclarations(t *testing.T) {
	for name, pipe := range infraRunPipelines(t) {
		cases := []map[string]string{{"revision": factSHA1}}
		if name == "bifrost-ci-infra" {
			cases = append(cases, infraLandingParams())
		}
		for i, input := range cases {
			t.Run(fmt.Sprintf("%s/%d", name, i), func(t *testing.T) {
				for _, viaParam := range []bool{false, true} {
					revision := factSHA1
					extra := maps.Clone(input)
					if viaParam {
						revision = ""
					} else {
						delete(extra, "revision")
					}
					s, dyn, paths := contractService(t, pipe, "present")
					resp, _, err := s.StartPipelineRun(context.Background(), name, revision, "", "tester", extra)
					if err != nil || !resp.OK {
						t.Fatal(resp, err)
					}
					run := createdContractRun(t, dyn)
					if err := validateRunRequirements(pipe, run); err != nil {
						t.Fatal(err)
					}
					params, _, _ := unstructured.NestedSlice(run.Object, "spec", "params")
					got := map[string]string{}
					for _, raw := range params {
						p := raw.(map[string]any)
						got[p["name"].(string)] = p["value"].(string)
					}
					want := maps.Clone(input)
					if name == "bifrost-ci-infra" && len(want) == 1 {
						want["base"], want["platformRevision"], want["generation"], want["purpose"] = "", "", "", "push"
					}
					assertJSONEqual(t, got, want)
					if len(params) != len(declaredPipelineParams(pipe)) {
						t.Fatal("run must send every declared parameter exactly once", params)
					}
					ws, _, _ := unstructured.NestedSlice(run.Object, "spec", "workspaces")
					if len(ws) != 1 || ws[0].(map[string]any)["name"] != "source" || ws[0].(map[string]any)["volumeClaimTemplate"] == nil {
						t.Fatal("missing source PVC", ws)
					}
					arch, _, _ := unstructured.NestedString(run.Object, "spec", "taskRunTemplate", "podTemplate", "nodeSelector", "kubernetes.io/arch")
					if arch != pipe.GetAnnotations()["bifrost.io/run-arch"] {
						t.Fatal("run architecture differs from Pipeline", arch)
					}
					if run.GetLabels()["bifrost.io/revision"] != factSHA1 {
						t.Fatal(run.GetLabels())
					}
					if name == "bifrost-ci-infra" && run.GetLabels()["bifrost.io/repo"] != "bifrost-trade-infra" {
						t.Fatal(run.GetLabels())
					}
					for _, path := range *paths {
						if !strings.Contains(path, "/bifrost-trade-infra/") {
							t.Fatal("probed an unrelated repository", path)
						}
					}
				}
			})
		}
	}
}

func TestInfraPipelineRequiredBindings(t *testing.T) {
	for name, pipe := range infraRunPipelines(t) {
		spec, _, err := buildPipelineRunSpec(name, factSHA1, "", nil, pipe)
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"workspaces", "params"} {
			run, err := unstructuredFrom(map[string]any{"spec": spec})
			if err != nil {
				t.Fatal(err)
			}
			unstructured.RemoveNestedField(run.Object, "spec", field)
			if err := validateRunRequirements(pipe, run); err == nil {
				t.Fatalf("%s accepted missing %s", name, field)
			}
		}
		for _, field := range []string{"workspaces", "params"} {
			bad := pipe.DeepCopy()
			unstructured.RemoveNestedField(bad.Object, "spec", field)
			s, dyn, _ := contractService(t, bad, "present")
			assertContractRefused(t, s, dyn, name, factSHA1, nil, "not declared")
		}
		for field, entry := range map[string]any{"workspaces": map[string]any{"name": "new-source"}, "params": map[string]any{"name": "new-required", "type": "string"}} {
			bad := pipe.DeepCopy()
			declared, _, _ := unstructured.NestedSlice(bad.Object, "spec", field)
			if err := unstructured.SetNestedSlice(bad.Object, append(declared, entry), "spec", field); err != nil {
				t.Fatal(err)
			}
			s, dyn, _ := contractService(t, bad, "present")
			assertContractRefused(t, s, dyn, name, factSHA1, nil, "required")
		}
	}
}

func TestInfraPipelineInputRefusals(t *testing.T) {
	for name, pipe := range infraRunPipelines(t) {
		for _, value := range []string{"", "main", factSHA1[:39], factSHA1 + "a", strings.Repeat("A", 40), " " + factSHA1, factSHA1 + "\n"} {
			for _, viaParam := range []bool{false, true} {
				revision, extra := value, map[string]string{}
				if viaParam {
					revision, extra["revision"] = "", value
				}
				s, dyn, _ := contractService(t, pipe, "present")
				assertContractRefused(t, s, dyn, name, revision, extra, "40-character lowercase git SHA")
			}
		}
		for _, key := range []string{"giteaBase", "image", "registry", "repo", "uiRevision", " revision"} {
			s, dyn, _ := contractService(t, pipe, "present")
			assertContractRefused(t, s, dyn, name, factSHA1, map[string]string{key: factSHA2}, "cannot be set")
		}
		s, dyn, _ := contractService(t, pipe, "present")
		assertContractRefused(t, s, dyn, name, factSHA1, map[string]string{"revision": factSHA2}, "must match")
	}
	const name = "bifrost-ci-infra"
	pipe := infraRunPipelines(t)[name]
	for _, key := range []string{"base", "platformRevision", "generation"} {
		input := infraLandingParams()
		delete(input, key)
		s, dyn, _ := contractService(t, pipe, "present")
		assertContractRefused(t, s, dyn, name, factSHA1, input, key)
	}
	for key, values := range map[string][]string{
		"base":             {"main", strings.Repeat("A", 40), factSHA2[:39], " " + factSHA2},
		"platformRevision": {"main", strings.Repeat("A", 40), factSHA2[:39], factSHA2 + "\n"},
		"generation":       {"0", "-1", "01", "+1", "1.1", "a", "9223372036854775808"},
		"purpose":          {"", "deploy", " landing"},
	} {
		for _, value := range values {
			input := infraLandingParams()
			input[key] = value
			s, dyn, _ := contractService(t, pipe, "present")
			assertContractRefused(t, s, dyn, name, factSHA1, input, key)
		}
	}
}

func TestInfraPipelineDeclarationsStayCurrent(t *testing.T) {
	live := infraPipelines(t)
	for name, fixture := range infraRunPipelines(t) {
		if live[name] == nil {
			t.Fatal("infra Pipeline missing", name)
		}
		assertJSONEqual(t, fixture.Object, live[name].Object)
	}
}

func readyAmd64Node() *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "ci", Labels: map[string]string{"kubernetes.io/arch": "amd64"}},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}}
}

// The ci-infra tools image is built with kaniko (review 788 C1, landing review
// of t_01M6796F06X57VF6T36PNFCQWV): with no Ready amd64 node the build preflight
// refuses it before any PipelineRun is created.
func TestCIInfraToolsBuildNeedsAnAmd64Node(t *testing.T) {
	if !isKanikoPipeline("bifrost-build-ci-infra-tools") {
		t.Fatal("the ci-infra tools build runs kaniko and must be placed like one")
	}
	pipe := infraRunPipelines(t)["bifrost-build-ci-infra-tools"]
	if pipe == nil {
		t.Fatal("bifrost-build-ci-infra-tools declaration missing")
	}
	s, dyn, _ := contractService(t, pipe, "present")
	s.SetClientsetForTest(k8sfake.NewSimpleClientset())
	resp, _, err := s.StartPipelineRun(context.Background(), "bifrost-build-ci-infra-tools", factSHA1, "", "tester", nil)
	if err == nil && resp.OK {
		t.Fatal("started a kaniko build with no Ready amd64 node")
	}
	if !strings.Contains(resp.Message, "amd64") {
		t.Fatalf("refusal does not name the missing amd64 node: %+v %v", resp, err)
	}
	for _, a := range dyn.Actions() {
		if a.GetVerb() == "create" {
			t.Fatal("a PipelineRun was created despite the refusal")
		}
	}
}
