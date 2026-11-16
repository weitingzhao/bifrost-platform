package cirunview

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"sort"
)

// FailedTask is the minimal failure observation; it conveys no passing evidence.
type FailedTask struct {
	Task       string
	Tests      []string
	NamesTests bool
}

// Failure is what a run's failed tasks say it failed on: their names, the
// tests they name, and the class: test with a test named; build when a task
// that names its failed tests failed and named none; "" when no failed task
// names tests at all (the console's, a check) or none is kept.
func Failure(failed []FailedTask) (tasks, tests []string, class string) {
	names := false
	seen := map[string]bool{}
	for _, f := range failed {
		tasks = append(tasks, f.Task)
		names = names || f.NamesTests
		for _, t := range f.Tests {
			if !seen[t] {
				seen[t] = true
				tests = append(tests, t)
			}
		}
	}
	sort.Strings(tests)
	switch {
	case len(tests) > 0:
		class = evidence.FailClassTest
	case names:
		class = evidence.FailClassBuild
	}
	return tasks, tests, class
}
