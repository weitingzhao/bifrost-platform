package landingci

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func s5RealFixture(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile("testdata/tekton-real-landing-ci.json")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err = json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func s5RealObject(t *testing.T, name string) *unstructured.Unstructured {
	t.Helper()
	return &unstructured.Unstructured{Object: s5RealFixture(t)[name].(map[string]any)}
}

// paramDefaults overlays actual values on a spec's declared defaults, as
// Tekton does before it substitutes $(params.*).
func paramDefaults(spec map[string]any, actual map[string]string) map[string]string {
	out := map[string]string{}
	for _, v := range sliceS5(spec["params"]) {
		p := v.(map[string]any)
		if d, ok := p["default"].(string); ok {
			out[p["name"].(string)] = d
		}
	}
	for k, v := range actual {
		out[k] = v
	}
	return out
}

// The resolved pipeline Tekton stored for a real platform run is the
// reviewed manifest after parameter/context substitution and Tekton's
// inert defaults; the plain comparison the reader used before never matches.
func TestS5RealTektonPipelineSpec(t *testing.T) {
	fixture := s5RealFixture(t)
	run := s5RealObject(t, "pipelineRun")
	manifest := fixture["pipelineYAML"].(map[string]any)
	resolved, _, _ := unstructured.NestedMap(run.Object, "status", "pipelineSpec")
	params, _ := namedValues(*run, "spec", "params")
	want := s5Substitute(manifest, paramDefaults(manifest, params), map[string]string{
		"pipelineRun.uid": string(run.GetUID()), "pipelineRun.name": run.GetName(), "pipelineRun.namespace": run.GetNamespace()})
	if s5Same(resolved, manifest) {
		t.Fatal("fixture no longer shows the substituted, defaulted form")
	}
	if !s5Resolved(resolved, want) {
		t.Fatalf("real resolved pipeline differs from the normalized manifest\n%s\n%s", mustJSON(s5Tekton(resolved)), mustJSON(s5Tekton(want)))
	}
	// Normalization is not a wildcard: any other change still differs.
	tasks := resolved["tasks"].([]any)
	tasks[0].(map[string]any)["params"].([]any)[1].(map[string]any)["value"] = strings.Repeat("f", 40)
	if s5Resolved(resolved, want) {
		t.Fatal("changed task parameter accepted")
	}
}

func TestS5RealTektonTaskRunShape(t *testing.T) {
	fixture := s5RealFixture(t)
	run := s5RealObject(t, "pipelineRun")
	task := s5RealObject(t, "taskRun")
	manifest := fixture["taskYAML"].(map[string]any)
	// Tekton defaults a Task result's type to string; the S5 Task manifests
	// write it explicitly (asserted below), so the reader needs no rule.
	for _, v := range sliceS5(manifest["results"]) {
		v.(map[string]any)["type"] = "string"
	}
	params, _ := namedValues(*task, "spec", "params")
	want := s5Substitute(manifest, paramDefaults(manifest, params), nil)
	// The clone task also uses workspace/result paths; the S5 tasks use
	// neither (asserted below), so the reader substitutes parameters only.
	raw, _ := json.Marshal(want)
	text := strings.NewReplacer("$(workspaces.output.path)", "/workspace/output", "$(results.commit.path)", "/tekton/results/commit").Replace(string(raw))
	_ = json.Unmarshal([]byte(text), &want)
	resolved, _, _ := unstructured.NestedMap(task.Object, "status", "taskSpec")
	if !s5Resolved(resolved, want) {
		t.Fatalf("real resolved task differs\n%s\n%s", mustJSON(s5Tekton(resolved)), mustJSON(s5Tekton(want)))
	}
	ref, _, _ := unstructured.NestedMap(task.Object, "spec", "taskRef")
	if ref["kind"] != "Task" || !s5TaskRef(ref, "bifrost-git-clone-gitea") || s5TaskRef(ref, "other") ||
		s5TaskRef(map[string]any{"kind": "ClusterTask", "name": "bifrost-git-clone-gitea"}, "bifrost-git-clone-gitea") {
		t.Fatal("taskRef default", ref)
	}
	// A TaskRun's timeout is its pipeline task's, else the run's, as stored.
	pipeline, _, _ := unstructured.NestedString(run.Object, "spec", "timeouts", "pipeline")
	if got, _, _ := unstructured.NestedString(task.Object, "spec", "timeout"); got != pipeline || got != "1h0m0s" {
		t.Fatal("untimed task", got, pipeline)
	}
	timed := s5RealObject(t, "timedTaskRun")
	got, _, _ := unstructured.NestedString(timed.Object, "spec", "timeout")
	for _, v := range sliceS5(fixture["pipelineYAML"].(map[string]any)["tasks"]) {
		if p := v.(map[string]any); p["name"] == "test-console" && s5Duration(p["timeout"]) != got {
			t.Fatal("pipeline task timeout", p["timeout"], got)
		}
	}
	for stage, spec := range s5TestContract(t).Tasks {
		for _, v := range sliceS5(spec["results"]) {
			if v.(map[string]any)["type"] != "string" {
				t.Fatal("S5 task result without explicit type", stage)
			}
		}
		if b, _ := json.Marshal(spec); strings.Contains(string(b), "$(workspaces.") || strings.Contains(string(b), "$(results.") || strings.Contains(string(b), "$(context.") {
			t.Fatal("S5 task uses a substitution the reader does not apply", stage)
		}
	}
}

// What platformSpec sends and what Tekton stores differ only by the claim
// template's round-trip fields and the defaults Tekton adds when absent
// (S5 always sends both timeouts and serviceAccountName).
func TestS5RealTektonRunSpecRoundTrip(t *testing.T) {
	run := s5RealObject(t, "pipelineRun")
	params, _ := namedValues(*run, "spec", "params")
	sent := platformSpec("bifrost-ci-platform", params)
	stored, _, _ := unstructured.NestedMap(run.Object, "spec")
	delete(stored, "timeouts")
	delete(stored["taskRunTemplate"].(map[string]any), "serviceAccountName")
	if s5Same(stored, sent) {
		t.Fatal("fixture no longer shows the round-trip fields")
	}
	if !s5Resolved(stored, sent) {
		t.Fatalf("stored run spec differs\n%s\n%s", mustJSON(s5Tekton(stored)), mustJSON(s5Tekton(sent)))
	}
	ws := stored["workspaces"].([]any)[0].(map[string]any)["volumeClaimTemplate"].(map[string]any)
	ws["metadata"] = map[string]any{"labels": map[string]any{"x": "y"}}
	if s5Resolved(stored, sent) {
		t.Fatal("claim template metadata accepted")
	}
}

// s5Duration is exactly metav1.Duration's JSON form.
func TestS5DurationIsMetav1Encoding(t *testing.T) {
	for _, in := range []string{"600s", "1200s", "1h", "10m0s", "90s", "1h0m0s"} {
		var d metav1.Duration
		if err := json.Unmarshal([]byte(`"`+in+`"`), &d); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(d)
		if string(b) != `"`+s5Duration(in).(string)+`"` {
			t.Fatal(in, string(b), s5Duration(in))
		}
	}
	if s5Duration("600") != "600" {
		t.Fatal("unparsable duration rewritten")
	}
}

// The real Pod's Tekton-owned mounts include /workspace on an emptyDir; the
// reader accepts it there only.
func TestS5RealTektonPodMounts(t *testing.T) {
	_, mounts := tektonVolumes(t)
	found := false
	for _, m := range mounts {
		if m.(map[string]any)["mountPath"] == "/workspace" {
			found = m.(map[string]any)["name"] == "tekton-internal-workspace"
		}
	}
	if !found {
		t.Fatal("fixture lost Tekton's /workspace mount")
	}
	k := s5TestKey()
	c, id, o := s5Fixture(t, k)
	cs, _, _ := unstructured.NestedSlice(o[4].Object, "spec", "containers")
	co := cs[0].(map[string]any)
	for _, m := range co["volumeMounts"].([]any) {
		if mount := m.(map[string]any); mount["mountPath"] == "/workspace" {
			mount["name"] = "ws-s5tree"
		}
	}
	setField(t, o[4], cs, "spec", "containers")
	if got := s5ReadFixture(t, k, c, id, o); got.Status == "Succeeded" || got.Reason != "S5_CI_IDENTITY" {
		t.Fatal("tree volume over /workspace accepted", got)
	}
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
