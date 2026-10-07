package delivery

import "testing"

func paramMap(params []map[string]any) map[string]any {
	out := map[string]any{}
	for _, p := range params {
		out[p["name"].(string)] = p["value"]
	}
	return out
}

func TestPipelineRunParamsUIRevision(t *testing.T) {
	for _, name := range []string{"bifrost-deliver-platform", "bifrost-deliver-platform-prod", "bifrost-deliver-stg", "bifrost-deliver-prod"} {
		got := paramMap(pipelineRunParams(name, "feature-x", ""))
		if got["revision"] != "feature-x" || got["uiRevision"] != "feature-x" {
			t.Fatalf("%s params = %v, want revision and uiRevision both feature-x", name, got)
		}
	}
	got := paramMap(pipelineRunParams("bifrost-deliver-research", "main", "1.2.3"))
	if _, ok := got["uiRevision"]; ok || got["tag"] != "1.2.3" {
		t.Fatalf("research params = %v, want a tag and no uiRevision", got)
	}
}
