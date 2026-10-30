package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/infracheck"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

const infra = "bifrost-trade-infra"

func TestInfraUsesSharedGenerator(t *testing.T) {
	t.Parallel()
	r := store.Repos[infra]
	if !slices.Equal(r.Verify[0], []string{"sh", "-c", infracheck.Check()}) ||
		!slices.EqualFunc(r.Setup, infracheck.Setup(""), slices.Equal) {
		t.Fatal("agentd must use the same infra check and setup as CI")
	}
}

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
	t.Parallel()
	r, known := store.Repos[infra]
	if !known || len(r.TestDirs) != 0 || len(r.Siblings) != 0 || len(r.Verify) != 1 || len(r.Setup) != 2 {
		t.Fatalf("infra = %+v", r)
	}
	if !slices.Equal(store.RepoNames(), []string{"bifrost-platform", infra}) {
		t.Fatalf("repos = %v", store.RepoNames())
	}
	check := r.Verify[0]
	// First a tree whose tests sit in a Python package is refused (review
	// 331 A1), then pytest reads no configuration and no conftest.py, and
	// loads no plugin it finds (review 330 A1).
	const refuse = `for f in __init__.py agent-config/__init__.py agent-config/scripts/__init__.py k8s/__init__.py k8s/data/__init__.py ` +
		`scripts/__init__.py; do if [ -e "$f" ] || [ -L "$f" ]; then echo "$f: `
	const find = `f=$(find -L k8s/data/role-matrix scripts/agentrt scripts/benchmark scripts/k3s scripts/landing -name __init__.py -print 2>/dev/null | head -n 1); `
	const script = `PATH="$PWD/.git/venv/bin:$PATH" GOCACHE="${TMPDIR:-$HOME/.cache}/go-build" PYTEST_DISABLE_PLUGIN_AUTOLOAD=1 ` +
		`exec python3 -I -m pytest -c /dev/null --rootdir="$PWD" --noconftest -q -p no:cacheprovider `
	before, after, found := strings.Cut(check[2], script)
	if len(check) != 3 || check[0] != "sh" || check[1] != "-c" || !found || !strings.Contains(before, refuse) || !strings.Contains(before, find) {
		t.Fatalf("check = %q", check)
	}
	tests := strings.Fields(after)
	for _, want := range []string{"scripts/agentrt", "scripts/benchmark", "scripts/k3s", "scripts/landing", "k8s/data/role-matrix", "scripts/test_access_checks.py"} {
		if !slices.Contains(tests, want) {
			t.Errorf("the check does not run %s: %v", want, tests)
		}
	}
	// Each left out for a reason the cut gives: time, loopback or the host.
	for _, out := range []string{"scripts/release", "scripts/gitea", "k8s/cicd/gitea-backup", "scripts/cicd", "agent-config/scripts/recorder",
		"scripts/agent-host", "scripts/agent-host/test_probe_gitea_protection.py", "scripts/test_agentrt_infra.py"} {
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
	// pip reads no configuration file (review 330 B5).
	if !slices.Equal(r.Setup[1][:3], []string{"env", "PIP_CONFIG_FILE=/dev/null", ".git/venv/bin/python3"}) {
		t.Fatalf("setup[1] = %q", r.Setup[1])
	}
	pip := r.Setup[1][2:]
	if !slices.Contains(pip, "-I") || !slices.Contains(pip, "--isolated") ||
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
		"    env PIP_CONFIG_FILE=/dev/null .git/venv/bin/python3 -I -m pip --isolated install", "--only-binary=:all: --no-deps pytest==9.0.2",
		"You may have no network"} {
		if !strings.Contains(spec.Text, want) {
			t.Fatalf("the text lacks %q:\n%s", want, spec.Text)
		}
	}
}

// A command's words are quoted where a shell would read them otherwise; the
// platform's commands, plain words, read as they did. A first word a shell
// would take for an assignment or a reserved word is quoted too, and the line
// runs as the command it shows (review 330 B1).
func TestImplementSpecQuotesCommands(t *testing.T) {
	t.Parallel()
	body, err := store.ImplementSpec("t", "x", store.Repo{}, [][]string{{"echo", "it's", "a b", "", "./...", "--x=:all:", "if", "A=B"},
		{"go", "-C", "api", "vet", "./..."}, {"A=B", "x"}, {"if", "x"}})
	ok(t, err)
	var spec struct{ Text string }
	ok(t, json.Unmarshal(body, &spec))
	for _, want := range []string{`    echo 'it'\''s' 'a b' '' ./... --x=:all: if A=B` + "\n", "    go -C api vet ./...\n", "    'A=B' x\n", "    'if' x\n"} {
		if !strings.Contains(spec.Text, want) {
			t.Fatalf("the text lacks %q:\n%s", want, spec.Text)
		}
	}
	// Read back by a shell: commands named A=B and if, on PATH, print their
	// arguments.
	bin := t.TempDir()
	for _, name := range []string{"A=B", "if"} {
		ok(t, os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\necho \"$0 $*\"\n"), 0o755))
	}
	for _, line := range []string{"    'A=B' x\n", "    'if' x\n"} {
		cmd := exec.Command("/bin/sh", "-c", strings.TrimSpace(line))
		cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin"}
		out, err := cmd.CombinedOutput()
		name, _, _ := strings.Cut(strings.Trim(strings.TrimSpace(line), "'"), "'")
		if err != nil || strings.TrimSpace(string(out)) != filepath.Join(bin, name)+" x" {
			t.Fatalf("%q ran as %q (%v)", line, out, err)
		}
	}
}

// An infra goal: its planning spec names infra's checks and takes no checks
// of a plan's own; the plan's task is an infra implementation whose spec is
// infra's; a plan that adds a check to an infra task is refused, as is a
// platform goal's plan naming infra; a repository the runtime does not know
// is still refused.
func TestInfraGoalToImplementation(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	owner := store.Writer{Actor: "owner", Token: "admin"}
	main := func(context.Context, string) (string, error) { return commitA, nil }
	if _, err := store.ProposeGoal(ctx, w.rt, owner, "g0", store.Goal{Title: "x", Text: "x", Repos: []string{"bifrost-trade-api"}, Main: main}); !errors.Is(err, store.ErrInvalid) ||
		!strings.Contains(err.Error(), "may name bifrost-platform, bifrost-trade-infra, not \"bifrost-trade-api\"") {
		t.Fatalf("an unknown repository: %v", err)
	}
	goal := func(key, repo string) (mission, plan string) {
		t.Helper()
		out, err := store.ProposeGoal(ctx, w.rt, owner, key, store.Goal{Title: "Test the probe", Text: "Test the probe", Repos: []string{repo}, Main: main})
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
			"this repository takes no checks of a plan's own; leave verify empty"},
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

// TD-404 (PROD M-23): codex's sandbox has no network, so the platform's setup
// downloads the API module's dependencies before the executor starts, first;
// its spec tells the executor so. Without it codex's go build and go vet of
// the packages it changed failed on an empty module cache.
func TestPlatformSetupDownloadsGoModules(t *testing.T) {
	t.Parallel()
	r := store.Repos["bifrost-platform"]
	if len(r.Setup) == 0 || !slices.Equal(r.Setup[0], []string{"go", "-C", "api", "mod", "download"}) {
		t.Fatalf("the platform's setup does not download Go's modules first: %q", r.Setup)
	}
	body, err := store.ImplementSpec("t", "x", r, r.Verify)
	ok(t, err)
	var spec struct {
		Text  string
		Setup [][]string
	}
	ok(t, json.Unmarshal(body, &spec))
	if !slices.EqualFunc(spec.Setup, r.Setup, slices.Equal) ||
		!strings.Contains(spec.Text, "Dependencies were installed before you started, by:\n\n    go -C api mod download\n") {
		t.Fatalf("spec = %s", body)
	}
}

// TD-403: the platform's tests need a database, so agentd gives its
// attempts one of their own; infra's need none.
func TestOnlyThePlatformGetsATestDatabase(t *testing.T) {
	t.Parallel()
	for repo, r := range store.Repos {
		if r.TestDB != (repo == "bifrost-platform") {
			t.Errorf("%s: TestDB %v", repo, r.TestDB)
		}
	}
}
