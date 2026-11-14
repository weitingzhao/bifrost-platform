package store_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// twoRepoPlan puts a task in each repository of a two-repository goal, the
// platform's with a check of its own, after infra's.
const twoRepoPlan = "Two changes.\n\n```json\n" + `{
  "mission": {"success": ["both land"]},
  "tasks": [{"ref": "t1", "title": "Test the probe", "repo": "bifrost-trade-infra", "text": "Add a test to scripts/agentrt.", "verify": []},
    {"ref": "t2", "title": "Name the test", "repo": "bifrost-platform", "text": "Name it in repos.go.",
     "verify": [["go", "-C", "api", "test", "./internal/agentrt/store/"]], "depends_on": ["t1"]}]
}` + "\n```\n"

// A mission of two repositories that kept no base for the second (one a
// route proposed, before goals kept bases): a plan with a task there is sent
// back, never started from the other repository's commit.
func TestPlanWithoutABaseIsSentBack(t *testing.T) {
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
	plan := w.ready(t, store.Task{MissionID: "M-3", Role: "plan", Effect: "none", OffPlanReason: "a goal", BaseCommit: commitA})
	w.work(t, plan, twoRepoPlan, "done")
	var c cards
	if did, err := store.AcceptPlan(ctx, w.rt, plan, c.ask, c.withdraw, nil); err != nil || did != "rework" ||
		!strings.Contains(lastReason(t, w, plan), "tasks[0]: the goal recorded no commit of bifrost-trade-infra to start from") {
		t.Fatalf("a plan without a base: %q %v %q", did, err, lastReason(t, w, plan))
	}
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.task WHERE mission_id = 'M-3' AND role = 'implement'`).Scan(&n))
	if n != 0 {
		t.Fatalf("a refused plan made %d tasks", n)
	}
}

// T26's first run (PROD M-9): a planner that read the goal's repositories
// side by side gave a platform task work only an infra checkout could do. A
// task's text that names a repository of the goal beside its checkout that
// the checkout does not have gets it beside the checkout, with a note (the
// Owner, 2026-11-06); one the goal does not cover is refused, with what the
// task has; the siblings it has, its own repository and paths that name no
// repository pass as they are.
func TestPlanTasksWorkInTheirCheckout(t *testing.T) {
	t.Parallel()
	text := func(repo, s string) string {
		return strings.Replace(twoRepoPlan, `"repo": "`+repo+`", "text": "`, `"repo": "`+repo+`", "text": "`+s+` `, 1)
	}
	both := []string{"bifrost-platform", infra}
	p, err := store.ParsePlan([]byte(text("bifrost-platform", "Then run ../bifrost-trade-infra/scripts/agentrt/check.sh, not ../bifrost-trade-infra/x.")), both)
	if err != nil || !slices.Equal(p.Tasks[1].Siblings, []string{infra}) || len(p.Tasks[0].Siblings) != 0 ||
		!slices.Equal(p.Notes, []string{"tasks[1]: its text names ../bifrost-trade-infra, added beside its checkout at the head of main"}) {
		t.Errorf("platform runs infra: %v %+v %q", err, p.Tasks, p.Notes)
	}
	for name, c := range map[string]struct{ report, why string }{
		"infra reads the ui": {text(infra, "Compare with ../../bifrost-ui/src."), "tasks[0]: its text names ../bifrost-ui, which is not a repository this goal covers " +
			"(a task in bifrost-trade-infra has bifrost-trade-infra alone)"},
		"another repository": {text("bifrost-platform", "See ../bifrost-research."), "names ../bifrost-research, which is not a repository this goal covers"},
		"a longer name":      {text("bifrost-platform", "Not ../bifrost-platform-plugin."), "names ../bifrost-platform-plugin"},
	} {
		if _, err := store.ParsePlan([]byte(c.report), both); err == nil || !strings.Contains(err.Error(), c.why) {
			t.Errorf("%s: %v (want %q)", name, err, c.why)
		}
	}
	for name, report := range map[string]string{
		"its sibling":     text("bifrost-platform", "The UI is at ../bifrost-ui (file:../../bifrost-ui)."),
		"its own":         text("bifrost-platform", "Not ../bifrost-platform."),
		"a relative path": text("bifrost-platform", "Import it from ../lib/x and run go -C api test ../internal."),
		"a name, no path": text("bifrost-platform", "After t1 lands in bifrost-trade-infra."),
	} {
		if p, err := store.ParsePlan([]byte(report), both); err != nil || len(p.Notes) != 0 {
			t.Errorf("%s: %v %q", name, err, p.Notes)
		}
	}
	if got := store.CheckoutOf(infra); got != "bifrost-trade-infra alone" {
		t.Errorf("CheckoutOf(infra) = %q", got)
	}
}
