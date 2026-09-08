package delivery

import "testing"

func TestPipelineRunWorkspacesDeliverProd(t *testing.T) {
	t.Parallel()
	ws := pipelineRunWorkspaces("bifrost-deliver-prod")
	if len(ws) != 1 {
		t.Fatalf("workspaces = %d, want 1", len(ws))
	}
	if ws[0]["name"] != "build-context" {
		t.Fatalf("workspace name = %v, want build-context", ws[0]["name"])
	}
	if ws[0]["volumeClaimTemplate"] == nil {
		t.Fatal("expected volumeClaimTemplate for deliver-prod build-context")
	}
}

func TestIsKanikoPipelineDeliverProd(t *testing.T) {
	t.Parallel()
	if !isKanikoPipeline("bifrost-deliver-prod") {
		t.Fatal("bifrost-deliver-prod should be a Kaniko pipeline")
	}
}

func TestResearchDagsterBuildGetsItsWorkspace(t *testing.T) {
	// It failed with InvalidWorkspaceBindings before this: the pipeline declares
	// build-context and platform-api bound nothing, so the only way to build the
	// Dagster image was kubectl create -f on a hand-edited run template.
	ws := pipelineRunWorkspaces("bifrost-build-research-dagster")
	if len(ws) != 1 || ws[0]["name"] != "build-context" {
		t.Fatalf("expected one build-context workspace, got %v", ws)
	}
	vct, _ := ws[0]["volumeClaimTemplate"].(map[string]any)
	spec, _ := vct["spec"].(map[string]any)
	res, _ := spec["resources"].(map[string]any)
	req, _ := res["requests"].(map[string]any)
	if req["storage"] != "8Gi" {
		t.Fatalf("the Dagster context carries the dbt project; want 8Gi, got %v", req["storage"])
	}
}

func TestTheDagsterSuffixIsEnforcedNotTrusted(t *testing.T) {
	// Both research image lines share one repository and differ only by this
	// suffix, so a bare semver would build a Dagster image over the image
	// research-api runs.
	const repo = "registry.cicd.svc.cluster.local:5000/bifrost-research"
	for _, tc := range []struct{ in, want string }{
		{"0.94.1", repo + ":0.94.1-dagster"},
		{"0.94.1-dagster", repo + ":0.94.1-dagster"},
		{"  0.94.1  ", repo + ":0.94.1-dagster"},
		{"", ""},
	} {
		if got := researchDagsterImage(tc.in); got != tc.want {
			t.Fatalf("researchDagsterImage(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTheDagsterBuildSchedulesOnAmd64(t *testing.T) {
	// It runs kaniko, so it carries the same placement rule as every other
	// build pipeline — an arm64 node gives "exec format error".
	if !isKanikoPipeline("bifrost-build-research-dagster") {
		t.Fatal("the Dagster build runs kaniko and must be placed like one")
	}
}
