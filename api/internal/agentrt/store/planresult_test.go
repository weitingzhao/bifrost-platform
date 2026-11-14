package store_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// A plan as a planner hands it in: two tasks (the second after the first, the
// first with two checks of its own), one question, two decisions, a mission
// with new criteria, what is out of scope and a lower budget.
const goodPlan = "Two changes, the second after the first.\n\n```json\n" + `{
  "mission": {"success": ["the comment says what MainHead reads", "go vet passes"], "out_of_scope": ["the Console"], "max_attempts": 6},
  "tasks": [
    {"ref": "t1", "title": "Document MainHead", "repo": "bifrost-platform", "text": "Say which ref MainHead reads.",
     "verify": [["go", "-C", "api", "test", "./internal/agentrt/agentrtapi/"], ["go", "-C", "api", "test", "-run", "TestGoal", "./internal/agentrt/agentrtapi/"]]},
    {"ref": "t2", "title": "Document GoalRepos", "repo": "bifrost-platform", "text": "Say why only the platform.", "depends_on": ["t1"]}
  ],
  "questions": [{"question": "Document the Console too?", "body": "It has no goal docs.", "options": ["Not now", "Yes, a third task"],
    "recommended": 1, "meanwhile": "two tasks go on without the Console", "owner_duty": "goal"}],
  "decisions": [{"decision": "two tasks, not one", "why": "each is one review", "dropped": "one task", "reverse": "merge them"},
    {"decision": "no new tests", "why": "comments only"}]
}` + "\n```\n"

func TestParsePlan(t *testing.T) {
	t.Parallel()
	repos := []string{"bifrost-platform"}
	p, err := store.ParsePlan([]byte(goodPlan), repos)
	if err != nil || len(p.Tasks) != 2 || len(p.Questions) != 1 || len(p.Decisions) != 2 || p.Mission.MaxAttempts != 6 {
		t.Fatalf("%+v %v", p, err)
	}
	// The last block is the plan.
	two := "```json\n{\"tasks\": []}\n```\n" + goodPlan
	if p, err = store.ParsePlan([]byte(two), repos); err != nil || len(p.Tasks) != 2 {
		t.Fatalf("the last block: %+v %v", p, err)
	}
	check := func(verify string) string {
		return strings.Replace(goodPlan, `["go", "-C", "api", "test", "./internal/agentrt/agentrtapi/"]`, verify, 1)
	}
	label := func(s string) string { return strings.Replace(goodPlan, `"Not now"`, `"`+s+`"`, 1) }
	// Only a plan that cannot be used is refused (the Owner, 2026-11-06).
	for name, c := range map[string]struct{ report, why string }{
		"two values": {"```json\n{\"tasks\": []} {}\n```", "more than one value"},
		"no tasks":   {"```json\n{\"tasks\": []}\n```", "at least 1 task"},
		"self dep":   {strings.Replace(goodPlan, `"depends_on": ["t1"]`, `"depends_on": ["t2"]`, 1), "not another task"},
	} {
		if _, err := store.ParsePlan([]byte(c.report), repos); err == nil || !strings.Contains(err.Error(), c.why) {
			t.Errorf("%s: %v (want %q)", name, err, c.why)
		}
	}
	// A check not in the accepted form is dropped, the task and its other
	// check kept.
	for name, c := range map[string]struct{ report, why string }{
		"go run":         {check(`["go", "-C", "api", "run", "./payload.go"]`), "a check is"},
		"go generate":    {check(`["go", "-C", "api", "generate", "./..."]`), "a check is"},
		"exec":           {check(`["go", "-C", "api", "test", "-exec=/bin/sh", "./..."]`), "not a package path"},
		"toolexec":       {check(`["go", "-C", "api", "build", "-toolexec=/bin/sh", "./..."]`), "a check is"},
		"other dir":      {check(`["go", "-C", "/tmp", "test", "./..."]`), "a check is"},
		"no -C":          {check(`["go", "test", "./..."]`), "a check is"},
		"up and out":     {check(`["go", "-C", "api", "test", "./../../x"]`), "not a package path"},
		"run then flag":  {check(`["go", "-C", "api", "test", "-run", "-exec", "./..."]`), "-run takes"},
		"run no package": {check(`["go", "-C", "api", "test", "-run", "TestX"]`), "-run takes"},
		"sh":             {check(`["sh", "-c", "true"]`), "a check is"},
	} {
		p, err := store.ParsePlan([]byte(c.report), repos)
		if err != nil || len(p.Tasks[0].Verify) != 1 || p.Tasks[0].Verify[0][4] != "-run" || !hasNote(p, "tasks[0].verify[0]", c.why, "dropped") {
			t.Errorf("%s: %v %v %q", name, err, p.Tasks, p.Notes)
		}
	}
	// The question store counts a label's JSON encoding, quotes included: a
	// label over its 160 bytes is cut to fit.
	for name, s := range map[string]string{"label bytes": strings.Repeat("字", 54), "label 159": strings.Repeat("a", 159)} {
		p, err := store.ParsePlan([]byte(label(s)), repos)
		if o := p.Questions[0].Options[0]; err != nil || !strings.HasSuffix(o, "…") || len(o) > 158 || !hasNote(p, "options[0] cut to 160 bytes") {
			t.Errorf("%s: %v %q %q", name, err, o, p.Notes)
		}
	}
	for name, report := range map[string]string{
		"label 158":     label(strings.Repeat("a", 158)),
		"label 52 字":    label(strings.Repeat("字", 52)),
		"whole module":  check(`["go", "-C", "api", "test", "./..."]`),
		"two packages":  check(`["go", "-C", "api", "test", "./internal/a/", "./internal/b/..."]`),
		"a run pattern": check(`["go", "-C", "api", "test", "-run", "^TestGoal(Route|Title)$", "./internal/agentrt/agentrtapi/"]`),
	} {
		if p, err := store.ParsePlan([]byte(report), repos); err != nil || len(p.Notes) != 0 {
			t.Errorf("%s: %v %q", name, err, p.Notes)
		}
	}
}

// hasNote says whether one of a plan's notes holds every one of parts.
func hasNote(p store.PlanDoc, parts ...string) bool {
	return slices.ContainsFunc(p.Notes, func(n string) bool {
		for _, s := range parts {
			if !strings.Contains(n, s) {
				return false
			}
		}
		return true
	})
}

// PlanBlock says whether a report's last block reads as a plan, in
// ParsePlan's own words, and leaves its content to ParsePlan's checks.
func TestPlanBlock(t *testing.T) {
	t.Parallel()
	repos := []string{"bifrost-platform"}
	if err := store.PlanBlock([]byte(goodPlan)); err != nil {
		t.Fatalf("a good plan: %v", err)
	}
	if err := store.PlanBlock([]byte("```json\n{\"tasks\": []}\n```")); err != nil {
		t.Fatalf("a block that reads, with no task: %v", err)
	}
	for name, c := range map[string]struct{ report, why string }{
		"no block":   {"# Plan\n\nNo block.\n", "the report has no ```json block"},
		"bad json":   {"```json\n{\"tasks\": [}\n```", "the plan does not read: "},
		"key twice":  {"```json\n{\"tasks\": [], \"Tasks\": []}\n```", `the plan names "Tasks" twice in one object`},
		"two values": {"```json\n{\"tasks\": []} {}\n```", "the plan block holds more than one value"},
	} {
		err := store.PlanBlock([]byte(c.report))
		_, perr := store.ParsePlan([]byte(c.report), repos)
		if err == nil || !strings.HasPrefix(err.Error(), c.why) || perr == nil || perr.Error() != err.Error() {
			t.Errorf("%s: %v, ParsePlan %v", name, err, perr)
		}
	}
}
