package unitexec

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func part(index, total, attempt int, class, list string, failed ...string) store.UnitRun {
	r := store.UnitRun{ShardIndex: index, ShardTotal: total, Attempt: attempt, Class: class, FailedTests: failed,
		RunName: "unit-0123abcd-" + string(rune('a'+index)) + strings.Repeat("r", attempt-1)}
	if list != "" {
		r.TestsListSHA = &list
	}
	return r
}

func TestSettle(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	p, tr, bd, in := store.UnitRunPassed, store.UnitRunTest, store.UnitRunBuild, store.UnitRunInfra
	for name, tc := range map[string]struct {
		runs    []store.UnitRun
		verdict string
		class   string
		failed  string
		retry   int
		lost    int
	}{
		"nothing ran":                         {nil, "", "", "", 0, 0},
		"one part of one passed":              {[]store.UnitRun{part(0, 1, 1, p, a)}, p, "", "", 0, 0},
		"a task with no test list passed":     {[]store.UnitRun{part(0, 1, 1, p, "")}, p, "", "", 0, 0},
		"two of three parts passed":           {[]store.UnitRun{part(0, 3, 1, p, a), part(2, 3, 1, p, a)}, "", "", "", 0, 0},
		"all three passed":                    {[]store.UnitRun{part(0, 3, 1, p, a), part(1, 3, 1, p, a), part(2, 3, 1, p, a)}, p, "", "", 0, 0},
		"the parts cut different lists":       {[]store.UnitRun{part(0, 2, 1, p, a), part(1, 2, 1, p, b)}, "", "", "", 0, 0},
		"a red part among missing ones":       {[]store.UnitRun{part(1, 3, 1, tr, a, "x.TestB", "x.TestA")}, SettledFailed, tr, "x.TestA,x.TestB", 0, 0},
		"a build failure and a red test":      {[]store.UnitRun{part(0, 2, 1, bd, ""), part(1, 2, 1, tr, a, "x.TestA")}, SettledFailed, tr, "x.TestA", 0, 0},
		"a build failure alone":               {[]store.UnitRun{part(0, 1, 1, bd, "")}, SettledFailed, bd, "", 0, 0},
		"a part lost once":                    {[]store.UnitRun{part(0, 2, 1, p, a), part(1, 2, 1, in, "")}, "", "", "", 1, 0},
		"the lost part passed on the retry":   {[]store.UnitRun{part(0, 2, 1, p, a), part(1, 2, 1, in, ""), part(1, 2, 2, p, a)}, p, "", "", 0, 0},
		"a part lost twice":                   {[]store.UnitRun{part(0, 2, 1, p, a), part(1, 2, 1, in, ""), part(1, 2, 2, in, "")}, "", "", "", 0, 1},
		"runs of another cut are not counted": {[]store.UnitRun{part(0, 1, 1, p, a), part(0, 2, 1, p, a)}, "", "", "", 0, 0},
	} {
		got := Settle(tc.runs)
		if got.Verdict != tc.verdict || got.FailClass != tc.class || strings.Join(got.FailedTests, ",") != tc.failed || len(got.Retry) != tc.retry || len(got.Lost) != tc.lost {
			t.Errorf("%s: %+v", name, got)
		}
	}
	red := Settle([]store.UnitRun{part(0, 2, 1, p, a), part(1, 2, 1, tr, a, "x.TestA")})
	if red.Head.ShardIndex != 1 || len(red.Runs) != 2 {
		t.Errorf("a failed task is written under its red part and names every run: %+v", red)
	}
	if green := Settle([]store.UnitRun{part(1, 2, 1, p, a), part(0, 2, 1, p, a)}); green.Head.ShardIndex != 0 {
		t.Errorf("a passed task is written under its first part: %+v", green.Head)
	}
}
