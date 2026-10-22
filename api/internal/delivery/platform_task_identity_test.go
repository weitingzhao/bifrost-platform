package delivery

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"os"
	"strings"
	"testing"
)

func TestPlatformSourceGateIdentity(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pipeline string
		tasks    []any
		task     string
		want     bool
	}{
		{"new prod", "bifrost-deliver-platform-prod", []any{map[string]any{"name": "check-source"}}, "check-source", true},
		{"old prod", "bifrost-deliver-platform-prod", []any{map[string]any{"name": "rollout"}}, "check-source", false},
		{"stg", "bifrost-deliver-platform", []any{map[string]any{"name": "check-source"}}, "check-source", false},
		// TD-411: STG's migration Job is created as tekton-deliver, the only
		// subject of its Role; PROD's pipeline has no such task.
		{"stg migrate", "bifrost-deliver-platform", []any{map[string]any{"name": "agentrt-migrate"}}, "agentrt-migrate", true},
		{"old stg", "bifrost-deliver-platform", []any{map[string]any{"name": "rollout"}}, "agentrt-migrate", false},
		{"prod migrate", "bifrost-deliver-platform-prod", []any{map[string]any{"name": "agentrt-migrate"}}, "agentrt-migrate", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pipe := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{"tasks": tc.tasks}}}
			specs := platformTaskRunSpecs(tc.pipeline, pipe)
			found := false
			for _, spec := range specs {
				if spec["serviceAccountName"] != "tekton-deliver" {
					t.Fatal(spec)
				}
				if spec["pipelineTaskName"] == tc.task {
					found = true
				}
			}
			if found != tc.want {
				t.Fatalf("%s override = %v, want %v", tc.task, found, tc.want)
			}
			if len(specs) != 2+boolInt(tc.want) {
				t.Fatal(specs)
			}
		})
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func TestPlatformSourceGateIdentityWiredIntoRun(t *testing.T) {
	source, err := os.ReadFile("run_spec.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), `spec["taskRunSpecs"] = platformTaskRunSpecs(pipelineName, pipe)`) {
		t.Fatal("PipelineRun must use conditional source identity overrides")
	}
}
