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
