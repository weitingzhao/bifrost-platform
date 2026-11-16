package planpace

import (
	"reflect"
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
	usual := map[string]UsualTime{"implement|go": {Duration: 300 * time.Second, Samples: 4}, "review|go": {Duration: 100 * time.Second, Samples: 3}}
	spent := func(s int64) *ETAAttempt { return &ETAAttempt{ElapsedS: &s} }
	task := func(id, role, class, state string, a *ETAAttempt) *ETATask {
		return &ETATask{TaskID: id, Role: role, Class: class, State: state, Attempt: a}
	}
	m := &ETAMission{State: "active", Tasks: []*ETATask{
		task("t1", "implement", "go", "running", spent(60)),
		task("t2", "review", "go", "proposed", nil),
		task("t3", "implement", "go", "submitted", nil), // counted by its acceptance and review
	}}
	eta := MissionETA(m, usual, now, 3)
	if eta["state"] != "estimated" || eta["earliest"] != now.Add(340*time.Second).Format(time.RFC3339) ||
		eta["latest"] != now.Add(540*time.Second).Format(time.RFC3339) {
		t.Fatalf("eta = %v; want 340 s to 540 s from now", eta)
	}
	wantBasis := []any{map[string]any{"role": "implement", "task_class": "go", "usual_s": int64(300), "samples": 4},
		map[string]any{"role": "review", "task_class": "go", "usual_s": int64(100), "samples": 3}}
	if !reflect.DeepEqual(eta["basis"], wantBasis) {
		t.Fatalf("basis = %v; want %v", eta["basis"], wantBasis)
	}
	if _, late := eta["past_usual"]; late {
		t.Fatalf("a task named past its usual time: %v", eta)
	}

	// Past 1.5 times its usual time, a task adds nothing and is named.
	m.Tasks[0] = task("t1", "implement", "go", "running", spent(500))
	eta = MissionETA(m, usual, now, 3)
	if eta["earliest"] != now.Add(100*time.Second).Format(time.RFC3339) || eta["latest"] != now.Add(150*time.Second).Format(time.RFC3339) ||
		len(eta["past_usual"].([]string)) != 1 {
		t.Fatalf("eta with a late task = %v", eta)
	}

	// Two of the same role and Class: each counted, one basis row.
	two := &ETAMission{State: "active", Tasks: []*ETATask{task("t6", "implement", "go", "ready", nil), task("t7", "implement", "go", "proposed", nil)}}
	eta = MissionETA(two, usual, now, 3)
	if eta["earliest"] != now.Add(600*time.Second).Format(time.RFC3339) || eta["latest"] != now.Add(900*time.Second).Format(time.RFC3339) ||
		len(eta["basis"].([]any)) != 1 {
		t.Fatalf("two implementations = %v; want 600 s to 900 s on one basis", eta)
	}
	// At exactly 1.5 times its usual time a task is not past it.
	m.Tasks[0] = task("t1", "implement", "go", "running", spent(450))
	if eta = MissionETA(m, usual, now, 3); eta["past_usual"] != nil || eta["earliest"] != now.Add(100*time.Second).Format(time.RFC3339) {
		t.Fatalf("eta at 1.5 times = %v", eta)
	}

	for name, c := range map[string]struct {
		m    *ETAMission
		want string
	}{
		"no usual time": {&ETAMission{State: "active", Tasks: []*ETATask{task("t4", "implement", "python", "ready", nil)}},
			"no usual time for implement python"},
		"paused":       {&ETAMission{State: "paused", Tasks: m.Tasks}, "the mission is paused"},
		"a task waits": {&ETAMission{State: "active", Tasks: []*ETATask{task("t5", "implement", "go", "blocked", nil)}}, "is blocked"},
		"nothing open": {&ETAMission{State: "active"}, "no unfinished task"},
		// Review 350 A3: submitted work waits for acceptance tasks not proposed yet.
		"only submitted": {&ETAMission{State: "active", Tasks: []*ETATask{task("t8", "implement", "go", "submitted", nil)}},
			"every unfinished task is submitted or verifying"},
		"only verifying": {&ETAMission{State: "active", Tasks: []*ETATask{task("t9", "implement", "go", "verifying", nil)}},
			"every unfinished task is submitted or verifying"},
		"on the Owner": {&ETAMission{State: "active", Tasks: []*ETATask{task("t10", "implement", "go", "awaiting_owner", nil)}},
			"is awaiting_owner"},
		"proposed, waiting on the Owner": {&ETAMission{State: "active", Tasks: []*ETATask{
			{TaskID: "t11", Role: "implement", Class: "go", State: "proposed", Status: "waiting_owner"}}}, "t11 is proposed"},
		"awaiting the Owner": {&ETAMission{State: "awaiting_owner", Tasks: m.Tasks}, "the mission is awaiting_owner"},
		// v4 §20.3 rule 7: a task maybe stuck, on a lost host or of unknown status makes the time unknown, whatever
		// the others' usual times; it says which (T24, §20.5: suspend a task).
		// A task before it with no usual time does not hide it (review 471).
		"maybe stuck": {&ETAMission{State: "active", Tasks: []*ETATask{task("t12", "implement", "python", "ready", nil),
			{TaskID: "t13", Role: "implement", Class: "go", State: "running", Status: "maybe_stuck", Attempt: spent(60)}}},
			"task t13 is maybe stuck: no time can be told until it is heard from"},
		"host lost": {&ETAMission{State: "active", Tasks: []*ETATask{
			{TaskID: "t14", Role: "implement", Class: "python", State: "running", Status: "host_lost", Attempt: spent(60)}}}, "task t14 is host lost"},
		"status unknown": {&ETAMission{State: "active", Tasks: []*ETATask{
			{TaskID: "t15", Role: "implement", Class: "go", State: "running", Status: "unknown"}}}, "task t15 is unknown: no time can be told until its state is known"},
	} {
		eta := MissionETA(c.m, usual, now, 3)
		if eta["state"] != "unknown" || !strings.Contains(eta["reason"].(string), c.want) {
			t.Errorf("%s: eta = %v; want unknown, %q", name, eta, c.want)
		}
	}
}
