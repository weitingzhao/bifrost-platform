package unitexec

import (
	"sort"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Settled is what the runs of one unit task on one content add up to.
type Settled struct {
	// Verdict is store.UnitRunPassed, "failed", or "" while the content is
	// not judged yet: a part is missing, still running, or was lost.
	Verdict string
	// Head is the run the ledger row is written under: the first part's.
	Head store.UnitRun
	// FailClass is test when a part had a red test, else build.
	FailClass   string
	FailedTests []string
	Runs        []string
	// Retry lists the parts whose only run was lost (infra, attempt 1).
	Retry []int
	// Lost lists the parts lost twice: nothing more is tried.
	Lost []int
}

// SettledFailed is the verdict of a unit task with a red part.
const SettledFailed = "failed"

// Settle reads the runs of one unit task on one content (one repo, unit,
// task and key). For each part the latest attempt counts. A red part makes
// the task failed whatever the others did. It passed when every part did
// and the parts agree on the list of tests they cut their share from.
func Settle(runs []store.UnitRun) Settled {
	var out Settled
	latest := map[int]store.UnitRun{}
	total := 0
	for _, r := range runs {
		if r.ShardTotal > total {
			total = r.ShardTotal
		}
	}
	for _, r := range runs {
		if r.ShardTotal != total {
			continue // a plan that cut the unit otherwise: not this cut's part
		}
		if cur, ok := latest[r.ShardIndex]; !ok || r.Attempt > cur.Attempt {
			latest[r.ShardIndex] = r
		}
	}
	if total == 0 {
		return out
	}
	parts := make([]int, 0, len(latest))
	for i := range latest {
		parts = append(parts, i)
	}
	sort.Ints(parts)
	red, passed := false, 0
	listSHA, agree := "", true
	seen := map[string]bool{}
	for n, i := range parts {
		r := latest[i]
		if n == 0 {
			out.Head = r
		}
		out.Runs = append(out.Runs, r.RunName)
		switch r.Class {
		case store.UnitRunPassed:
			passed++
			sha := ""
			if r.TestsListSHA != nil {
				sha = *r.TestsListSHA
			}
			if passed == 1 {
				listSHA = sha
			} else if sha != listSHA {
				agree = false
			}
		case store.UnitRunTest, store.UnitRunBuild:
			if !red || r.Class == store.UnitRunTest && out.FailClass != store.UnitRunTest {
				out.FailClass = r.Class
			}
			if !red {
				out.Head = r
			}
			red = true
			for _, name := range r.FailedTests {
				if !seen[name] {
					seen[name] = true
					out.FailedTests = append(out.FailedTests, name)
				}
			}
		default:
			if r.Attempt <= 1 {
				out.Retry = append(out.Retry, i)
			} else {
				out.Lost = append(out.Lost, i)
			}
		}
	}
	sort.Strings(out.FailedTests)
	switch {
	case red:
		out.Verdict = SettledFailed
	case passed == total && len(latest) == total && agree:
		out.Verdict = store.UnitRunPassed
	}
	return out
}
