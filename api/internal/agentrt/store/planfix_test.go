package store_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The Owner, 2026-11-06: the plan check refuses only a plan that cannot be
// used. Each slip of form below parses, with the fix applied and a note that
// names it; each of the kept refusals still refuses.
func TestPlanFixesSlipsAndRefusesOnlyTheUnusable(t *testing.T) {
	t.Parallel()
	both := []string{"bifrost-platform", infra}
	plan := func(pairs ...string) string {
		r := goodPlan
		for i := 0; i < len(pairs); i += 2 {
			if !strings.Contains(r, pairs[i]) {
				t.Fatalf("goodPlan has no %q", pairs[i])
			}
			r = strings.Replace(r, pairs[i], pairs[i+1], 1)
		}
		return r
	}
	const title, text, verify0 = `"title": "Document MainHead"`, `"text": "Say which ref MainHead reads."`,
		`["go", "-C", "api", "test", "./internal/agentrt/agentrtapi/"]`
	const question = `{"question": "Q?", "options": ["a", "b"], "recommended": 1, "meanwhile": "m", "owner_duty": "goal"}`
	const decision = `{"decision": "d", "why": "w"}`
	list := func(item string, n int) string { return strings.TrimSuffix(strings.Repeat(item+", ", n), ", ") }
	long := strings.Repeat("x", 150)
	for name, c := range map[string]struct {
		report string
		holds  func(store.PlanDoc) bool
		note   string // a part of the note it records; "" for none
	}{
		"a title over 100 characters": {plan(title, `"title": "`+long+`"`),
			func(p store.PlanDoc) bool {
				return utf8.RuneCountInString(p.Tasks[0].Title) == 100 && strings.HasSuffix(p.Tasks[0].Title, "…")
			}, "tasks[0].title cut to 100 characters"},
		"control characters in a title": {plan(title, `"title": "Document\u001b Main\nHead"`),
			func(p store.PlanDoc) bool { return p.Tasks[0].Title == "Document Main Head" }, "tasks[0].title: control characters stripped"},
		"an empty title": {plan(title, `"title": " "`, text, `"text": "Say which ref\nMainHead reads."`),
			func(p store.PlanDoc) bool { return p.Tasks[0].Title == "Say which ref" }, `tasks[0]: the empty title takes "Say which ref"`},
		"a text over its limit": {plan(text, `"text": "`+strings.Repeat("y", 8001)+`"`),
			func(p store.PlanDoc) bool { return utf8.RuneCountInString(p.Tasks[0].Text) == 8000 }, "tasks[0].text cut to 8000 characters"},
		"a bad ref": {plan(`"ref": "t1"`, `"ref": "t 1"`, `"depends_on": ["t1"]`, `"depends_on": ["t 1"]`),
			func(p store.PlanDoc) bool {
				return p.Tasks[0].Ref == "t1" && slices.Equal(p.Tasks[1].DependsOn, []string{"t1"})
			}, `ref "t 1" read as "t1"`},
		"unknown fields": {plan(`"mission": {`, `"budget": 5, "mission": {`, title, title+`, "goal": "x"`),
			func(p store.PlanDoc) bool { return slices.Equal(p.Ignored, []string{"budget", "tasks[0].goal"}) }, ""},
		"a negative budget": {plan(`"max_attempts": 6`, `"max_attempts": -1`),
			func(p store.PlanDoc) bool { return p.Mission.MaxAttempts == 0 }, "mission.max_attempts -1 dropped"},
		"too many criteria": {plan(`"success": [`, `"success": [`+list(`"c"`, 10)+`, `),
			func(p store.PlanDoc) bool { return len(p.Mission.Success) == 10 }, "mission.success after the first 10 dropped"},
		"an empty criterion": {plan(`"out_of_scope": ["the Console"]`, `"out_of_scope": ["the Console", " "]`),
			func(p store.PlanDoc) bool { return slices.Equal(p.Mission.OutOfScope, []string{"the Console"}) }, "mission.out_of_scope: 1 empty dropped"},
		"a check written with -short": {plan(verify0, `["go", "-C", "api", "test", "-short", "./internal/agentrt/agentrtapi/"]`),
			func(p store.PlanDoc) bool { return len(p.Tasks[0].Verify) == 1 && p.Tasks[0].Verify[0][4] == "-run" },
			`tasks[0].verify[0]: a check is go -C <api> test [-run <pattern>] ./<package>...; "-short" is not a package path of the module; dropped`},
		"more checks than 5": {plan(verify0, list(verify0, 6)),
			func(p store.PlanDoc) bool { return len(p.Tasks[0].Verify) == 5 }, "tasks[0].verify: checks after the first 5 dropped"},
		"verify on a repository that takes none": {plan(`"title": "Document MainHead", "repo": "bifrost-platform"`, `"title": "Document MainHead", "repo": "`+infra+`"`),
			func(p store.PlanDoc) bool { return p.Tasks[0].Repo == infra && len(p.Tasks[0].Verify) == 0 },
			"tasks[0].verify dropped: this repository takes no checks of a plan's own"},
		"a text naming a sibling the task lacks": {plan(text, `"text": "Run ../bifrost-trade-infra/scripts/check.sh."`),
			func(p store.PlanDoc) bool {
				return slices.Equal(p.Tasks[0].Siblings, []string{infra}) && len(p.Tasks[1].Siblings) == 0
			}, "tasks[0]: its text names ../bifrost-trade-infra, added beside its checkout at the head of main"},
		// Cleanup wave 1 (2026-11-11): no bound of 3 questions.
		"more questions than 3": {plan(`"questions": [`, `"questions": [`+list(question, 3)+`, `),
			func(p store.PlanDoc) bool { return len(p.Questions) == 4 && p.Questions[0].Question == "Q?" }, ""},
		"more options than 4": {plan(`["Not now", "Yes, a third task"]`, `["Not now", "Yes, a third task", "c", "d", "e", "f"]`),
			func(p store.PlanDoc) bool {
				return slices.Equal(p.Questions[0].Options, []string{"Not now", "Yes, a third task", "c", "d"})
			},
			"questions[0]: 2 options after the first 4 dropped"},
		"one option": {plan(`["Not now", "Yes, a third task"]`, `["Not now"]`),
			func(p store.PlanDoc) bool { return len(p.Questions) == 0 }, "questions[0] dropped: 1 options, a question has 2 to 4"},
		"no question": {plan(`"question": "Document the Console too?"`, `"question": ""`),
			func(p store.PlanDoc) bool { return len(p.Questions) == 0 }, "questions[0] dropped: no question"},
		"a recommended out of range": {plan(`"recommended": 1`, `"recommended": 3`),
			func(p store.PlanDoc) bool { return p.Questions[0].Recommended == 1 }, "questions[0]: recommended 3 read as 1"},
		"no recommended": {plan(`"recommended": 1, `, ``),
			func(p store.PlanDoc) bool { return p.Questions[0].Recommended == 1 }, "questions[0]: recommended 0 read as 1"},
		"an unknown owner_duty": {plan(`"owner_duty": "goal"`, `"owner_duty": "taste"`),
			func(p store.PlanDoc) bool { return p.Questions[0].OwnerDuty == "goal" }, `questions[0]: owner_duty "taste" read as goal`},
		"a body over the store's bytes": {plan(`"body": "It has no goal docs."`, `"body": "`+strings.Repeat("字", 1100)+`"`),
			func(p store.PlanDoc) bool {
				b := p.Questions[0].Body
				return len(b) <= 2998 && strings.HasSuffix(b, "…")
			},
			"questions[0].body cut to 3000 bytes encoded"},
		"a question over the store's bytes": {plan(`"question": "Document the Console too?"`, `"question": "`+strings.Repeat(`\"`, 200)+`"`),
			func(p store.PlanDoc) bool { return strings.HasPrefix(p.Questions[0].Question, `"""`) },
			"questions[0].question cut to 300 bytes encoded"},
		"no meanwhile": {plan(`"meanwhile": "two tasks go on without the Console"`, `"meanwhile": ""`),
			func(p store.PlanDoc) bool { return p.Questions[0].Meanwhile == "the work goes on with Not now" },
			"questions[0]: the empty meanwhile takes the recommended option"},
		"more decisions than 10": {plan(`"decisions": [`, `"decisions": [`+list(decision, 10)+`, `),
			func(p store.PlanDoc) bool { return len(p.Decisions) == 10 }, "decisions after the first 10 dropped"},
		"a decision with no why": {plan(`"why": "comments only"`, `"why": ""`),
			func(p store.PlanDoc) bool { return p.Decisions[1].Why == "not given" }, "decisions[1]: no why given"},
		"a decision with no decision": {plan(`"decision": "no new tests"`, `"decision": " "`),
			func(p store.PlanDoc) bool { return len(p.Decisions) == 1 }, "decisions[1] dropped: no decision"},
	} {
		p, err := store.ParsePlan([]byte(c.report), both)
		switch {
		case err != nil:
			t.Errorf("%s refused: %v", name, err)
		case !c.holds(p):
			t.Errorf("%s: not fixed: %+v", name, p)
		case c.note == "" && len(p.Notes) != 0, c.note != "" && !hasNote(p, c.note):
			t.Errorf("%s: notes %q (want %q)", name, p.Notes, c.note)
		}
	}

	task := func(ref string) string {
		return `{"ref": "` + ref + `", "title": "x", "repo": "bifrost-platform", "text": "x"}`
	}
	tasks := func(n int) string {
		all := make([]string, n)
		for i := range all {
			all[i] = task(fmt.Sprintf("t%d", i+1))
		}
		return "```json\n{\"tasks\": [" + strings.Join(all, ", ") + "]}\n```\n"
	}
	// Cleanup wave 1 (2026-11-11): no bound of 8 tasks.
	if p, err := store.ParsePlan([]byte(tasks(20)), both); err != nil || len(p.Tasks) != 20 || len(p.Notes) != 0 {
		t.Errorf("20 tasks: %v %q", err, p.Notes)
	}
	for name, c := range map[string]struct {
		report, why string
		repos       []string
	}{
		"the JSON does not read":  {"```json\n{\"tasks\": [}\n```\n", "the plan does not read", both},
		"no json block":           {"I could not plan this.", "no ```json block", both},
		"no tasks":                {"```json\n{\"tasks\": []}\n```\n", "a plan has at least 1 task, not 0", both},
		"a ref used twice":        {plan(`"ref": "t2"`, `"ref": "t1"`), `tasks[1]: ref "t1" is used twice`, both},
		"a ref used twice, fixed": {plan(`"ref": "t2"`, `"ref": "t 1"`), `tasks[1]: ref "t1" is used twice`, both},
		"depends_on naming none":  {plan(`"depends_on": ["t1"]`, `"depends_on": ["t9"]`), `tasks[1]: depends_on names "t9", which is not another task`, both},
		"a dependency cycle":      {plan(`"verify": [["go"`, `"depends_on": ["t2"], "verify": [["go"`), "make a cycle through", both},
		"a repository not covered": {plan(`"repo": "bifrost-platform", "text": "Say which`, `"repo": "bifrost-trade-api", "text": "Say which`),
			`tasks[0]: repo is one of bifrost-platform, bifrost-trade-infra, not "bifrost-trade-api"`, both},
		"naming a repository not covered": {plan(text, `"text": "Run ../bifrost-trade-infra/scripts/check.sh."`),
			"tasks[0]: its text names ../bifrost-trade-infra, which is not a repository this goal covers", []string{"bifrost-platform"}},
	} {
		if _, err := store.ParsePlan([]byte(c.report), c.repos); err == nil || !strings.Contains(err.Error(), c.why) {
			t.Errorf("%s: %v (want %q)", name, err, c.why)
		}
	}
}

// A used plan's fixes reach what it makes and its accepted event names them:
// a raise of the budget is dropped and the mission keeps its own; the card
// takes the fixed default; a check not in the form is dropped from the spec;
// a repository of the goal the text names beside the checkout is in the
// spec's siblings, at main.
func TestAcceptPlanNotesItsFixes(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: "M-3", Title: "goal", Goal: "a goal",
			Deadline: time.Now().Add(24 * time.Hour), Repos: []string{"bifrost-platform", infra}, Success: []string{store.GoalSuccess},
			MaxAttempts: 10, MaxWallHours: 24}); err != nil {
			return err
		}
		return store.MoveMission(ctx, tx, "controller", "M-3", "active", "test")
	}))
	report := goodPlan
	for _, r := range [][2]string{{`"max_attempts": 6`, `"max_attempts": 40`}, {`"recommended": 1`, `"recommended": 3`},
		{`["go", "-C", "api", "test", "./internal/agentrt/agentrtapi/"]`, `["go", "-C", "api", "run", "./payload.go"]`},
		{`"text": "Say which ref MainHead reads."`, `"text": "Say which ref MainHead reads, as ../bifrost-trade-infra/scripts/main.sh does."`}} {
		report = strings.Replace(report, r[0], r[1], 1)
	}
	plan := w.handIn(t, report, "done")
	var s cards
	main := func(context.Context, string) (string, error) { return commitB, nil }
	if did, err := store.AcceptPlan(ctx, w.rt, plan, s.ask, s.withdraw, main); err != nil || did != "accepted" {
		t.Fatalf("accept: %q %v", did, err)
	}
	var reason, attempts string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'reason' FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_state_changed'
	  AND body_json->>'to' = 'accepted'`, plan).Scan(&reason))
	for _, want := range []string{"plan used: 2 tasks", ", fixed: ", "tasks[0].verify[0]: a check is go -C <api> test", "questions[0]: recommended 3 read as 1",
		"tasks[0]: its text names ../bifrost-trade-infra, added beside its checkout at the head of main",
		"mission.max_attempts 40 dropped: a plan may only lower the mission's 10"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the accepted reason lacks %q: %q", want, reason)
		}
	}
	ok(t, w.rt.QueryRow(ctx, `SELECT budget_json->>'max_attempts' FROM agentrt.mission WHERE mission_id = 'M-3'`).Scan(&attempts))
	if attempts != "10" || len(s.asked) != 1 || s.asked[0].Recommended != 1 {
		t.Fatalf("max_attempts %s, cards %+v", attempts, s.asked)
	}
	var body []byte
	ok(t, w.rt.QueryRow(ctx, `SELECT b.body FROM agentrt.task t JOIN agentrt.artifact_blob b ON b.artifact_id = t.spec_artifact_id
	  WHERE t.mission_id = 'M-3' AND t.role = 'implement' AND t.title = 'Document MainHead'`).Scan(&body))
	spec, err := agentd.ParseSpec(body)
	ok(t, err)
	want := append(slices.Clone(store.Repos["bifrost-platform"].Verify), []string{"go", "-C", "api", "test", "-run", "TestGoal", "./internal/agentrt/agentrtapi/"})
	sibs := []agentd.Sibling{{Repo: "bifrost-ui", Ref: "main"}, {Repo: infra, Ref: "main"}}
	if !slices.EqualFunc(spec.Verify, want, slices.Equal[[]string]) || !slices.Equal(spec.Siblings, sibs) {
		t.Fatalf("the spec: verify %q, siblings %+v", spec.Verify, spec.Siblings)
	}
}
