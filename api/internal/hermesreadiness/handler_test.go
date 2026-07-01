package hermesreadiness

import "testing"

func TestFirstTask_L0ReadOnly(t *testing.T) {
	task := FirstTask()
	if task.Autonomy != "L0" {
		t.Fatalf("autonomy %s", task.Autonomy)
	}
	if task.ID != "hermes-mission-health-l0" {
		t.Fatalf("id %s", task.ID)
	}
	if !stringsContains(task.Prompt, "verify_mission_snapshot") {
		t.Fatal("prompt missing verify_mission_snapshot")
	}
	if !stringsContains(task.Prompt, "L0") {
		t.Fatal("prompt missing L0")
	}
	if len(task.RequiredMcpTools) < 3 {
		t.Fatalf("tools %v", task.RequiredMcpTools)
	}
}

func stringsContains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
