package store_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// A plan is refused only for what its planner got wrong (R5:5.7, PROD M-342):
// a key the reader does not use, a fence with no language and a base the goal
// did not keep are not the planner's mistakes, and cost no round.

// unknownKeysPlan is goodPlan with keys a plan does not have at the top
// level, in mission and in a task.
var unknownKeysPlan = strings.NewReplacer(
	`"mission": {`, `"goal": "document the goal route", "mission": {"budget": {"hours": 2}, `,
	`{"ref": "t1", `, `{"ref": "t1", "goal": "say which ref", `,
).Replace(goodPlan)

func TestPlanGateUnknownKeys(t *testing.T) {
	t.Parallel()
	repos := []string{"bifrost-platform"}
	p, err := store.ParsePlan([]byte(unknownKeysPlan), repos)
	if err != nil || len(p.Tasks) != 2 || p.Mission.MaxAttempts != 6 {
		t.Fatalf("a plan with unknown keys: %+v %v", p, err)
	}
	if want := []string{"goal", "mission.budget", "tasks[0].goal"}; !slices.Equal(p.Ignored, want) {
		t.Fatalf("ignored %v, want %v", p.Ignored, want)
	}
	if err = store.PlanBlock([]byte(unknownKeysPlan)); err != nil {
		t.Fatalf("PlanBlock: %v", err)
	}
	// A key matches a field whatever its case, so it is not ignored.
	if p, err = store.ParsePlan([]byte(strings.Replace(goodPlan, `"mission":`, `"Mission":`, 1)), repos); err != nil || len(p.Ignored) != 0 {
		t.Fatalf("a key in another case: %v %v", p.Ignored, err)
	}
	// A key named twice is still refused, a known one or not.
	for name, report := range map[string]string{
		"known":   strings.Replace(goodPlan, `"mission": {`, `"tasks": [], "mission": {`, 1),
		"unknown": strings.Replace(unknownKeysPlan, `"mission": {`, `"Goal": "again", "mission": {`, 1),
	} {
		if _, err := store.ParsePlan([]byte(report), repos); err == nil || !strings.Contains(err.Error(), "twice in one object") {
			t.Errorf("%s twice: %v", name, err)
		}
	}
}

func TestPlanGateFences(t *testing.T) {
	t.Parallel()
	repos := []string{"bifrost-platform"}
	plain := strings.Replace(goodPlan, "```json\n", "```\n", 1)
	upper := strings.Replace(goodPlan, "```json\n", "```JSON\n", 1)
	oneTask := "```\n" + `{"tasks": [{"ref": "t1", "title": "One", "repo": "bifrost-platform", "text": "One."}]}` + "\n```\n"
	for name, c := range map[string]struct {
		report string
		tasks  int
	}{
		"no language":           {plain, 2},
		"JSON":                  {upper, 2},
		"after a go block":      {"```go\nfunc x() {}\n```\n\n" + plain, 2},
		"the last plain one":    {plain + "\n" + oneTask, 1},
		"json before plain":     {goodPlan + "\n" + oneTask, 2},
		"json after plain":      {oneTask + "\n" + goodPlan, 2},
		"a plain block of text": {plain + "\n```\nnot a plan\n```\n", 2},
	} {
		p, err := store.ParsePlan([]byte(c.report), repos)
		if err != nil || len(p.Tasks) != c.tasks {
			t.Errorf("%s: %d tasks, %v (want %d)", name, len(p.Tasks), err, c.tasks)
		}
	}
	for name, report := range map[string]string{
		"no block":       "# Plan\n\nNo block.\n",
		"a go block":     strings.Replace(goodPlan, "```json\n", "```go\n", 1),
		"plain, no {":    "```\n[1, 2]\n```\n",
		"never closed":   "```\n{\"tasks\": []}\n",
		"lower-case one": strings.Replace(goodPlan, "```json\n", "```Json\n", 1),
	} {
		err := store.PlanBlock([]byte(report))
		_, perr := store.ParsePlan([]byte(report), repos)
		if err == nil || err.Error() != "the report has no ```json block" || perr == nil || perr.Error() != err.Error() {
			t.Errorf("%s: %v, ParsePlan %v", name, err, perr)
		}
	}
}
