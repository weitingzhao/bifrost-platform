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
