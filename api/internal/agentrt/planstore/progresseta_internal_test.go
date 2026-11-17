package planstore

import (
	"strings"
	"testing"
	"time"
)

func TestMissionETAAdapter(t *testing.T) {
	t.Parallel()
	spent := int64(60)
	now := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	m := &progressMission{state: "active", tasks: []*ProgressTask{{TaskID: "t", Role: "implement", class: "go", State: "running", Attempt: &ProgressAttempt{ElapsedS: &spent}}}}
	got := missionETA(m, map[string]usualTime{"implement|go": {d: 300 * time.Second, n: 4}}, now)
	if got["state"] != "estimated" || got["earliest"] != "2026-11-15T00:04:00Z" || got["latest"] != "2026-11-15T00:06:30Z" {
		t.Fatalf("adapter lost observations: %v", got)
	}
}

func TestMissionETAAdapterPreservesStatusAndMain(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	task := &ProgressTask{TaskID: "done", Role: "implement", class: "go", State: "running", OnMain: &ProgressOnMain{}}
	m := &progressMission{state: "active", tasks: []*ProgressTask{task}}
	usual := map[string]usualTime{"implement|go": {d: time.Minute, n: 3}}
	if got := missionETA(m, usual, now); got["state"] != "unknown" {
		t.Fatalf("counted an on-main task: %v", got)
	}
	task.OnMain = nil
	for _, status := range []string{StatusUnknown, StatusMaybeStuck, StatusHostLost, StatusStopped} {
		task.Status = status
		got := missionETA(m, usual, now)
		if got["state"] != "unknown" || !strings.Contains(got["reason"].(string), strings.ReplaceAll(status, "_", " ")) {
			t.Fatalf("lost status %s: %v", status, got)
		}
	}
	task.Status = ""
	task.Attempt = &ProgressAttempt{}
	if got := missionETA(m, usual, now); got["earliest"] != "2026-11-15T00:01:00Z" {
		t.Fatalf("nil elapsed changed: %v", got)
	}
}
