package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

const infra = "bifrost-trade-infra"

// infraPlan is a planner's plan for an infra goal: one task, no checks of
// its own.
const infraPlan = "One change.\n\n```json\n" + `{
  "mission": {"success": ["the probe has a test"]},
  "tasks": [{"ref": "t1", "title": "Test the probe", "repo": "bifrost-trade-infra", "text": "Add a test to scripts/agentrt.", "verify": []}]
}` + "\n```\n"

// The infra repository's spec (CUT-runtime-repo-infra-2026-10-19): its own
// Python tests, run by the Python of a virtual environment the setup makes
// in .git/venv from pinned wheels, first on PATH; no sibling. The commands
// are shown as a shell runs them, the sh -c script as one argument.
func TestInfraImplementSpec(t *testing.T) {
	r, known := store.Repos[infra]
	if !known || len(r.TestDirs) != 0 || len(r.Siblings) != 0 || len(r.Verify) != 1 || len(r.Setup) != 2 {
		t.Fatalf("infra = %+v", r)
	}
	if !slices.Equal(store.RepoNames(), []string{"bifrost-platform", infra}) {
		t.Fatalf("repos = %v", store.RepoNames())
	}
	check := r.Verify[0]
	const script = `PATH="$PWD/.git/venv/bin:$PATH" GOCACHE="${TMPDIR:-$HOME/.cache}/go-build" exec python3 -I -m pytest -q -p no:cacheprovider `
	if len(check) != 3 || check[0] != "sh" || check[1] != "-c" || !strings.HasPrefix(check[2], script) {
		t.Fatalf("check = %q", check)
	}
	tests := strings.Fields(strings.TrimPrefix(check[2], script))
	for _, want := range []string{"scripts/agentrt", "scripts/benchmark", "scripts/k3s", "k8s/data/role-matrix"} {
		if !slices.Contains(tests, want) {
			t.Errorf("the check does not run %s: %v", want, tests)
		}
	}
	// Each left out for a reason the cut gives: time, loopback, the host, or
	// main.
	for _, out := range []string{"scripts/release", "scripts/gitea", "k8s/cicd/gitea-backup", "scripts/cicd", "agent-config/scripts/recorder",
		"scripts/agent-host", "scripts/agent-host/test_probe_gitea_protection.py", "scripts/test_agentrt_infra.py", "scripts/test_access_checks.py"} {
		if slices.Contains(tests, out) {
			t.Errorf("the check runs %s", out)
		}
	}
	for _, p := range tests {
		if strings.HasPrefix(p, "/") || slices.Contains(strings.Split(p, "/"), "..") || strings.ContainsAny(p, `'"$;&|`) {
			t.Errorf("%q is not a plain path of the repository", p)
		}
	}
	if want := []string{"python3", "-I", "-m", "venv", "--clear", ".git/venv"}; !slices.Equal(r.Setup[0], want) {
		t.Fatalf("setup[0] = %q", r.Setup[0])
	}
	pip := r.Setup[1]
	if pip[0] != ".git/venv/bin/python3" || !slices.Contains(pip, "-I") || !slices.Contains(pip, "--isolated") ||
		!slices.Contains(pip, "--no-deps") || !slices.Contains(pip, "--only-binary=:all:") || !slices.Contains(pip, "--no-cache-dir") {
		t.Fatalf("setup[1] = %q", pip)
	}
	var pins int
	for _, a := range pip {
		if strings.Contains(a, "==") {
			pins++
		} else if !strings.HasPrefix(a, "-") && a != pip[0] && a != "pip" && a != "install" {
			t.Errorf("%q is installed unpinned", a)
		}
	}
	if pins != 6 || !slices.Contains(pip, "pytest==9.0.2") || !slices.Contains(pip, "pyyaml==6.0.3") {
		t.Fatalf("pins = %d in %q", pins, pip)
	}

	body, err := store.ImplementSpec("Test the probe", "Add a test.", r, r.Verify)
	ok(t, err)
	var spec struct {
		Text     string
		Verify   [][]string
		Setup    [][]string
		Siblings []any
		TimeoutS int `json:"timeout_s"`
	}
	ok(t, json.Unmarshal(body, &spec))
	if !slices.EqualFunc(spec.Verify, r.Verify, slices.Equal) || !slices.EqualFunc(spec.Setup, r.Setup, slices.Equal) ||
		spec.Siblings != nil || spec.TimeoutS != store.SpecTimeoutS {
		t.Fatalf("spec = %s", body)
	}
	for _, want := range []string{"    sh -c '" + check[2] + "'\n", "    python3 -I -m venv --clear .git/venv\n",
		"--only-binary=:all: --no-deps pytest==9.0.2", "You may have no network"} {
		if !strings.Contains(spec.Text, want) {
			t.Fatalf("the text lacks %q:\n%s", want, spec.Text)
		}
	}
}

// A command's words are quoted where a shell would read them otherwise; the
// platform's commands, plain words, read as they did.
func TestImplementSpecQuotesCommands(t *testing.T) {
	body, err := store.ImplementSpec("t", "x", store.Repo{}, [][]string{{"echo", "it's", "a b", "", "./...", "--x=:all:"}, {"go", "-C", "api", "vet", "./..."}})
	ok(t, err)
	var spec struct{ Text string }
	ok(t, json.Unmarshal(body, &spec))
	for _, want := range []string{`    echo 'it'\''s' 'a b' '' ./... --x=:all:` + "\n", "    go -C api vet ./...\n"} {
		if !strings.Contains(spec.Text, want) {
			t.Fatalf("the text lacks %q:\n%s", want, spec.Text)
		}
	}
}

// An infra goal: its planning spec names infra's checks and takes no checks
// of a plan's own; the plan's task is an infra implementation whose spec is
// infra's; a plan that adds a check to an infra task is refused, as is a
// platform goal's plan naming infra; a repository the runtime does not know
// is still refused.
func TestInfraGoalToImplementation(t *testing.T) {
	w := newKernelWorld(t)
	owner := store.Writer{Actor: "owner", Token: "admin"}
	main := func(context.Context) (string, error) { return commitA, nil }
	if _, err := store.ProposeGoal(ctx, w.rt, owner, "g0", store.Goal{Title: "x", Text: "x", Repo: "bifrost-trade-api", Main: main}); !errors.Is(err, store.ErrInvalid) ||
		!strings.Contains(err.Error(), "may name bifrost-platform, bifrost-trade-infra, not \"bifrost-trade-api\"") {
		t.Fatalf("an unknown repository: %v", err)
	}
	goal := func(key, repo string) (mission, plan string) {
		t.Helper()
		out, err := store.ProposeGoal(ctx, w.rt, owner, key, store.Goal{Title: "Test the probe", Text: "Test the probe", Repo: repo, Main: main})
		ok(t, err)
		mission, plan = out["mission_id"].(string), out["task_id"].(string)
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", mission, "active", "test") }))
		_, err = store.Dispatch(ctx, w.rt)
		ok(t, err)
		return mission, plan
	}
	text := func(id string) string {
		var body []byte
		ok(t, w.rt.QueryRow(ctx, `SELECT b.body FROM agentrt.task t JOIN agentrt.artifact_blob b ON b.artifact_id = t.spec_artifact_id
		  WHERE t.task_id = $1`, id).Scan(&body))
		return string(body)
	}

	mission, plan := goal("g1", infra)
	var spec struct{ Text string }
	ok(t, json.Unmarshal([]byte(text(plan)), &spec))
	for _, want := range []string{"- " + infra + ": `sh -c '", "Every task is in " + infra + ", the goal's repository",
		"takes no\n  checks of a plan's own: leave verify empty", `"repo": "` + infra + `"`, `"verify": [], "depends_on"`} {
		if !strings.Contains(spec.Text, want) {
			t.Fatalf("the planning spec lacks %q:\n%s", want, spec.Text)
		}
	}
	if strings.Contains(spec.Text, "go -C api test [-run") {
		t.Fatalf("an infra plan is told to add go tests:\n%s", spec.Text)
	}
	w.work(t, plan, infraPlan, "done")
	var c2 cards
	if did, err := store.AcceptPlan(ctx, w.rt, plan, c2.ask, c2.withdraw); err != nil || did != "accepted" {
		t.Fatalf("accept: %q %v", did, err)
	}
	var impl, repo, base, class string
	ok(t, w.rt.QueryRow(ctx, `SELECT task_id, repo, base_commit, task_class FROM agentrt.task WHERE mission_id = $1 AND role = 'implement'`,
		mission).Scan(&impl, &repo, &base, &class))
	r := store.Repos[infra]
	want, err := store.ImplementSpec("Test the probe", "Add a test to scripts/agentrt.", r, r.Verify)
	ok(t, err)
	if repo != infra || base != commitA || class != "goal" || text(impl) != string(want) {
		t.Fatalf("the implementation: %s %s %s\n%s", repo, base, class, text(impl))
	}

	// A check of the plan's own on infra is not taken: there is no module a
	// plan may test there. Nor may a platform goal's plan name infra. (The
	// implementation above is cancelled, and each refused plan after it, so
	// the planner claims the next plan.)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveTask(ctx, tx, "controller", impl, "cancelled", "test") }))
	for key, c := range map[string]struct{ repo, report, why string }{
		"g2": {infra, strings.Replace(infraPlan, `"verify": []`, `"verify": [["python3", "-m", "pytest", "scripts/agentrt"]]`, 1),
			"a check is go -C <> test"},
		"g3": {"bifrost-platform", infraPlan, `repo is one of bifrost-platform, not "bifrost-trade-infra"`},
	} {
		_, plan = goal(key, c.repo)
		w.work(t, plan, c.report, "done")
		if did, err := store.AcceptPlan(ctx, w.rt, plan, c2.ask, c2.withdraw); err != nil || did != "rework" ||
			!strings.Contains(lastReason(t, w, plan), c.why) {
			t.Fatalf("%s: %q %v %q", key, did, err, lastReason(t, w, plan))
		}
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveTask(ctx, tx, "controller", plan, "cancelled", "test") }))
	}
}
