package store

import (
	"strings"
	"testing"
	"time"
)

// When a mission should be done (2.5 G8): from the usual times of its
// unfinished tasks, one after another, less what is spent; unknown when
// something has no usual time.
func TestMissionETA(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	usual := map[string]usualTime{"implement|go": {d: 300 * time.Second, n: 4}, "review|go": {d: 100 * time.Second, n: 3}}
	spent := func(s int64) *ProgressAttempt { return &ProgressAttempt{ElapsedS: &s} }
	task := func(id, role, class, state string, a *ProgressAttempt) *ProgressTask {
		return &ProgressTask{TaskID: id, Role: role, class: class, State: state, Attempt: a}
	}
	m := &progressMission{state: "active", tasks: []*ProgressTask{
		task("t1", "implement", "go", "running", spent(60)),
		task("t2", "review", "go", "proposed", nil),
		task("t3", "implement", "go", "submitted", nil), // counted by its acceptance and review
	}}
	eta := missionETA(m, usual, now)
	if eta["state"] != "estimated" || eta["earliest"] != now.Add(340*time.Second).Format(time.RFC3339) ||
		eta["latest"] != now.Add(540*time.Second).Format(time.RFC3339) {
		t.Fatalf("eta = %v; want 340 s to 540 s from now", eta)
	}
	basis := eta["basis"].([]any)
	if len(basis) != 2 || basis[0].(map[string]any)["role"] != "implement" || basis[1].(map[string]any)["samples"] != 3 {
		t.Fatalf("basis = %v", basis)
	}
	if _, late := eta["past_usual"]; late {
		t.Fatalf("a task named past its usual time: %v", eta)
	}

	// Past 1.5 times its usual time, a task adds nothing and is named.
	m.tasks[0] = task("t1", "implement", "go", "running", spent(500))
	eta = missionETA(m, usual, now)
	if eta["earliest"] != now.Add(100*time.Second).Format(time.RFC3339) || eta["latest"] != now.Add(150*time.Second).Format(time.RFC3339) ||
		len(eta["past_usual"].([]string)) != 1 {
		t.Fatalf("eta with a late task = %v", eta)
	}

	for name, c := range map[string]struct {
		m    *progressMission
		want string
	}{
		"no usual time": {&progressMission{state: "active", tasks: []*ProgressTask{task("t4", "implement", "python", "ready", nil)}},
			"no usual time for implement python"},
		"paused":       {&progressMission{state: "paused", tasks: m.tasks}, "the mission is paused"},
		"a task waits": {&progressMission{state: "active", tasks: []*ProgressTask{task("t5", "implement", "go", "blocked", nil)}}, "is blocked"},
		"nothing open": {&progressMission{state: "active"}, "no unfinished task"},
	} {
		eta := missionETA(c.m, usual, now)
		if eta["state"] != "unknown" || !strings.Contains(eta["reason"].(string), c.want) {
			t.Errorf("%s: eta = %v; want unknown, %q", name, eta, c.want)
		}
	}
}
