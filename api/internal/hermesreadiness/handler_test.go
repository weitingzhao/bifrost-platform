package hermesreadiness

import (
	"strings"
	"testing"
)

func TestFirstTask_L0ReadOnly(t *testing.T) {
	task := FirstTask()
	if task.Autonomy != "L0" {
		t.Fatalf("autonomy %s", task.Autonomy)
	}
	if task.ID != "hermes-mission-health-l0" {
		t.Fatalf("id %s", task.ID)
	}
	if !strings.Contains(task.Prompt, "verify_mission_snapshot") {
		t.Fatal("prompt missing verify_mission_snapshot")
	}
	if !strings.Contains(task.Prompt, "L0") {
		t.Fatal("prompt missing L0")
	}
	if len(task.RequiredMcpTools) < 3 {
		t.Fatalf("tools %v", task.RequiredMcpTools)
	}
}

func TestBuild_LlmKeyMissingBlockerDetail(t *testing.T) {
	t.Setenv("NOUS_HERMES_URL", "")
	t.Setenv("HERMES_LLM_KEY_CONFIGURED", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	resp := Build(t.Context(), NewHandler().httpClient)
	if resp.Ready {
		t.Fatal("expected not ready without nous + llm")
	}
	if len(resp.BlockerDetails) == 0 {
		t.Fatal("expected blocker_details")
	}
	found := false
	for _, d := range resp.BlockerDetails {
		if d.Code == "NOUS_HERMES_URL_MISSING" {
			found = true
			if d.Remediation == "" {
				t.Fatal("missing remediation")
			}
		}
	}
	if !found {
		t.Fatalf("codes %v", resp.BlockerDetails)
	}
}
