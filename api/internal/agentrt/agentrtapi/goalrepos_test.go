package agentrtapi_test

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// A goal naming several repositories over HTTP (the Owner 10-19, T26): repos
// is taken, sorted, main read for each; repo of one and repos of one are the
// same request under one key; repo and repos together, an empty or repeated
// repos, or a repository the runtime does not know are refused and write
// nothing; a main that cannot be read names the repositories.
func TestGoalRepos(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	mains = nil
	code, out := call(t, s, "admin", "POST", "goals", map[string]any{"idempotency_key": "r1", "goal": "do this",
		"repos": []string{"bifrost-trade-infra", "bifrost-platform"}})
	if code != http.StatusOK || out["repo"] != "bifrost-platform" || fmt.Sprint(out["repos"]) != "[bifrost-platform bifrost-trade-infra]" ||
		fmt.Sprint(out["bases"]) != fmt.Sprintf("map[bifrost-platform:%s bifrost-trade-infra:%s]", commitA, commitA) {
		t.Fatalf("a goal of two repositories: %d %v", code, out)
	}
	if !slices.Equal(mains, []string{"bifrost-platform", "bifrost-trade-infra"}) {
		t.Fatalf("main read for %v", mains)
	}
	var scope string
	must(t, pool.QueryRow(ctx, `SELECT scope_json->>'repos' FROM agentrt.mission WHERE mission_id = $1`, out["mission_id"]).Scan(&scope))
	if scope != `["bifrost-platform", "bifrost-trade-infra"]` {
		t.Fatalf("the mission's repos: %s", scope)
	}

	code, one := call(t, s, "admin", "POST", "goals", map[string]any{"idempotency_key": "r2", "goal": "do that", "repo": "bifrost-platform"})
	if code != http.StatusOK {
		t.Fatalf("a goal by repo: %d %v", code, one)
	}
	if code, again := call(t, s, "admin", "POST", "goals", map[string]any{"idempotency_key": "r2", "goal": "do that",
		"repos": []string{"bifrost-platform"}}); code != http.StatusOK || again["task_id"] != one["task_id"] {
		t.Fatalf("the same goal by repos: %d %v", code, again)
	}

	count := func() (m, k int) {
		must(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.mission), (SELECT count(*) FROM agentrt.task)`).Scan(&m, &k))
		return m, k
	}
	missions, tasks := count()
	for name, c := range map[string]struct {
		body map[string]any
		has  string
	}{
		"both":    {map[string]any{"idempotency_key": "x1", "goal": "do this", "repo": "bifrost-platform", "repos": []string{"bifrost-platform"}}, "repo or repos, not both"},
		"empty":   {map[string]any{"idempotency_key": "x2", "goal": "do this", "repos": []string{}}, "not none"},
		"twice":   {map[string]any{"idempotency_key": "x3", "goal": "do this", "repos": []string{"bifrost-platform", "bifrost-platform"}}, "names bifrost-platform twice"},
		"unknown": {map[string]any{"idempotency_key": "x4", "goal": "do this", "repos": []string{"bifrost-platform", "bifrost-trade-api"}}, `not "bifrost-trade-api"`},
		// Both given, whatever one holds (review 332 B1).
		"empty repo and repos": {map[string]any{"idempotency_key": "x6", "goal": "do this", "repo": "", "repos": []string{"bifrost-platform"}}, "not both"},
		"repo and null repos":  {map[string]any{"idempotency_key": "x7", "goal": "do this", "repo": "bifrost-platform", "repos": nil}, "not both"},
		"null repos":           {map[string]any{"idempotency_key": "x8", "goal": "do this", "repos": nil}, "not none"},
		"null repo":            {map[string]any{"idempotency_key": "x9", "goal": "do this", "repo": nil}, `not ""`},
		"empty repo":           {map[string]any{"idempotency_key": "x10", "goal": "do this", "repo": ""}, `not ""`},
		"a number in repos":    {map[string]any{"idempotency_key": "x11", "goal": "do this", "repos": []any{"bifrost-platform", 7}}, "list of repositories' names"},
		"repo a list":          {map[string]any{"idempotency_key": "x12", "goal": "do this", "repo": []string{"bifrost-platform"}}, "a repository's name"},
	} {
		if code, out := call(t, s, "admin", "POST", "goals", c.body); code != http.StatusUnprocessableEntity || out["error"] != "invalid" ||
			!strings.Contains(out["message"].(string), c.has) {
			t.Errorf("%s: %d %v", name, code, out)
		}
	}
	mainFails = true
	t.Cleanup(func() { mainFails = false })
	if code, out := call(t, s, "admin", "POST", "goals", map[string]any{"idempotency_key": "x5", "goal": "do this",
		"repos": []string{"bifrost-platform", "bifrost-trade-infra"}}); code != http.StatusServiceUnavailable || out["error"] != "no_main" ||
		!strings.Contains(out["message"].(string), "bifrost-platform or bifrost-trade-infra") {
		t.Errorf("main unreadable: %d %v", code, out)
	}
	if m, k := count(); m != missions || k != tasks {
		t.Fatalf("refused goals wrote: missions %d -> %d, tasks %d -> %d", missions, m, tasks, k)
	}
}

// The Console offers the repositories the runtime takes: its GOAL_REPOS are
// store.Repos, in the order RepoNames gives.
func TestConsoleGoalReposAreTheRuntimes(t *testing.T) {
	src, err := os.ReadFile("../../../../console/src/api/agentRuntime.ts")
	must(t, err)
	m := regexp.MustCompile(`export const GOAL_REPOS = \[([^\]]*)\] as const`).FindSubmatch(src)
	if m == nil {
		t.Fatal("console/src/api/agentRuntime.ts has no GOAL_REPOS")
	}
	var console []string
	for _, q := range regexp.MustCompile(`'([^']+)'`).FindAllSubmatch(m[1], -1) {
		console = append(console, string(q[1]))
	}
	if !slices.Equal(console, store.RepoNames()) {
		t.Fatalf("the Console offers %v; the runtime takes %v", console, store.RepoNames())
	}
}
