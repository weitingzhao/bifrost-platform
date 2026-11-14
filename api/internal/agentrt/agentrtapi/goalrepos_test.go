package agentrtapi_test

import (
	"os"
	"regexp"
	"slices"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

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
