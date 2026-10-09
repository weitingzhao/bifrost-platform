package delivery

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestMergeParamsRejectsUndeclaredAndBadValues(t *testing.T) {
	declared := map[string]bool{"revision": true, "coreRevision": true}
	legacy := []map[string]any{{"name": "revision", "value": "main"}}
	if _, err := mergePipelineParams(declared, legacy, map[string]string{"nope": "main"}); err == nil {
		t.Fatal("undeclared name was accepted")
	}
	if _, err := mergePipelineParams(declared, legacy, map[string]string{"coreRevision": "has space"}); err == nil {
		t.Fatal("illegal character was accepted")
	}
	long := strings.Repeat("a", 257)
	if _, err := mergePipelineParams(declared, legacy, map[string]string{"coreRevision": long}); err == nil {
		t.Fatal("over-long value was accepted")
	}
}

func TestMergeParamsCallerWinsAndLegacyUndeclaredDrops(t *testing.T) {
	declared := map[string]bool{"revision": true, "coreRevision": true}
	legacy := []map[string]any{
		{"name": "revision", "value": "main"},
		{"name": "coreRevision", "value": "main"},
		{"name": "not-yet", "value": "main"},
	}
	got, err := mergePipelineParams(declared, legacy, map[string]string{"coreRevision": "abc"})
	if err != nil {
		t.Fatal(err)
	}
	m := paramMap(got)
	if m["revision"] != "main" || m["coreRevision"] != "abc" {
		t.Fatalf("merged = %v", m)
	}
	if _, ok := m["not-yet"]; ok {
		t.Fatal("undeclared legacy param was kept")
	}
}

func TestMergeParamsEmptyCallerKeepsLegacy(t *testing.T) {
	got, err := mergePipelineParams(nil, pipelineRunParams("bifrost-deliver-prod", "main", ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	m := paramMap(got)
	for _, name := range []string{"revision", "uiRevision", "coreRevision", "workerRevision", "apiRevision", "frontendRevision", "infraRevision"} {
		if m[name] != "main" {
			t.Fatalf("%s = %v, want main", name, m[name])
		}
	}
}

func TestDeclaredParamsAndRunExtras(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"params": []any{
				map[string]any{"name": "revision"},
				map[string]any{"name": "coreRevision"},
			},
		},
	}}
	declared := declaredPipelineParams(obj)
	if !declared["revision"] || !declared["coreRevision"] || len(declared) != 2 {
		t.Fatalf("declared = %v", declared)
	}
	spec := map[string]any{}
	if err := applyDeclaredRunExtras(spec, map[string]string{
		"bifrost.io/run-timeout": "1h0m0s",
		"bifrost.io/run-arch":    "amd64",
	}); err != nil {
		t.Fatal(err)
	}
	timeouts, _ := spec["timeouts"].(map[string]any)
	if timeouts["pipeline"] != "1h0m0s" {
		t.Fatalf("timeout = %v", spec["timeouts"])
	}
	if spec["taskRunTemplate"] == nil {
		t.Fatal("amd64 template was not applied")
	}
	if err := applyDeclaredRunExtras(map[string]any{}, map[string]string{"bifrost.io/run-timeout": "not a duration"}); err == nil {
		t.Fatal("bad timeout was accepted")
	}
}
