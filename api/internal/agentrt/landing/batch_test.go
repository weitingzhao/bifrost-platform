package landing

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// batchCand is a candidate whose facts are eligibleFacts for task n.
func batchCand(n int) BatchCandidate {
	f := eligibleFacts()
	f.Task = fmt.Sprintf("t_%026d", n)
	return BatchCandidate{Task: f.Task, Facts: f}
}

func batchTasks(cs []BatchCandidate) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Task)
	}
	return out
}

// R5:5.7 goal 2: a batch is the platform's waiting roots in candidate
// order, at most the rule's max_batch (none set: no cap) and what the day's
// quota leaves; never an S5 root
// or another repository's; the first root that may wait for a grant leads.
func TestBatchFormation(t *testing.T) {
	t.Parallel()
	now := epoch.Add(time.Hour)
	var cands []BatchCandidate
	for i := range 10 {
		c := batchCand(i)
		c.Facts.Rule.Landing.Limits.MaxLandingsPerDay = 20
		cands = append(cands, c)
	}
	// No max_batch: all ten, past the old fixed 8 (2026-11-06 Owner: the
	// batch size is the rule's, not the code's).
	if got := FormBatch(cands, now, false); !slices.Equal(batchTasks(got), batchTasks(cands)) {
		t.Fatal("not every root in order", batchTasks(got))
	}
	capped := slices.Clone(cands)
	for i := range capped {
		f := capped[i].Facts
		r := *f.Rule
		l := *r.Landing
		l.Limits.MaxBatch, r.Landing, f.Rule = 3, &l, &r
		capped[i].Facts = f
	}
	if got := FormBatch(capped, now, false); !slices.Equal(batchTasks(got), batchTasks(cands[:3])) {
		t.Fatal("not the rule's max_batch first in order", batchTasks(got))
	}
	// The day's quota: five a day, two reserved already, leaves three.
	quota := slices.Clone(cands[:6])
	for i := range quota {
		f := quota[i].Facts
		r := *f.Rule
		l := *r.Landing
		l.Limits.MaxLandingsPerDay, r.Landing, f.Rule, f.ReservedToday = 5, &l, &r, 2
		quota[i].Facts = f
	}
	if got := FormBatch(quota, now, false); len(got) != 3 {
		t.Fatal("past the day's quota", batchTasks(got))
	}
	// S5 and another repository stay out; the others keep their order.
	mixed := slices.Clone(cands[:5])
	mixed[1].Facts.Operation.S5.Required = true
	mixed[3].Facts.Repo = "bifrost-trade-infra"
	if got := batchTasks(FormBatch(mixed, now, false)); !slices.Equal(got, []string{mixed[0].Task, mixed[2].Task, mixed[4].Task}) {
		t.Fatal("S5 or infra in a batch", got)
	}
	// Two roots that may wait for a grant (a core path, grants enabled): the
	// first leads, the second stays in its place.
	granted := slices.Clone(cands[:4])
	for _, i := range []int{2, 3} {
		f := granted[i].Facts
		f.GrantEnabled = true
		f.Changes = slices.Clone(f.Changes)
		f.Changes[0].OldPath, f.Changes[0].NewPath = "api/internal/agentrt/store/example.go", "api/internal/agentrt/store/example.go"
		if reason, soft := PolicyChecks(f, now); reason != "" || len(soft) == 0 || !MayAwaitGrant(f, soft) {
			t.Fatal("fixture: not a root that may wait for a grant", reason, soft)
		}
		granted[i].Facts = f
	}
	if got := batchTasks(FormBatch(granted, now, false)); !slices.Equal(got, []string{granted[2].Task, granted[0].Task, granted[1].Task, granted[3].Task}) {
		t.Fatal("the first grant root does not lead", got)
	}
	// Another tenant waits; one candidate is a batch of one.
	other := slices.Clone(cands[:3])
	other[1].Facts.Tenant = "other"
	if got := batchTasks(FormBatch(other, now, false)); !slices.Equal(got, []string{other[0].Task, other[2].Task}) {
		t.Fatal("tenants mixed", got)
	}
	if got := FormBatch(cands[:1], now, false); len(got) != 1 {
		t.Fatal("one candidate", len(got))
	}
}

// Split halves a batch, the first half the larger.
func TestBatchSplit(t *testing.T) {
	t.Parallel()
	for n, want := range map[int][2]int{2: {1, 1}, 3: {2, 1}, 8: {4, 4}, 5: {3, 2}} {
		var m []string
		for i := range n {
			m = append(m, fmt.Sprint(i))
		}
		a, b := Split(m)
		if len(a) != want[0] || len(b) != want[1] || !slices.Equal(append(a, b...), m) {
			t.Fatal(n, a, b)
		}
	}
}

// BlameRed on what a red landing CI's TaskRuns really say: go test's FAIL
// lines, a build error, a panic. Nothing that maps blames no one.
func TestBatchBlameRed(t *testing.T) {
	t.Parallel()
	members := []BatchMember{
		{Op: "land:a:1", Task: "a", Paths: []string{"api/internal/agentrt/store/landing_batch.go"}},
		{Op: "land:b:1", Task: "b", Paths: []string{"api/internal/agentrt/landing/batch.go", "api/internal/agentrt/landing/batch_test.go"}},
		{Op: "land:c:1", Task: "c", Paths: []string{"console/src/components/A.tsx"}},
		{Op: "land:d:1", Task: "d", Paths: []string{"api/internal/agentrt/store/sub/x.go"}},
	}
	blamed := func(tr ...CIFailedTask) []string {
		var out []string
		for _, m := range BlameRed(CIFailure{Pipeline: "p", Run: "r", TaskRuns: tr}, members) {
			out = append(out, m.Task)
		}
		return out
	}
	goTest := `=== RUN   TestBatchFormation
    batch_test.go:41: not the first BatchMax in order [t_1]
--- FAIL: TestBatchFormation (0.00s)
FAIL
FAIL	github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing	0.412s
ok  	github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store	12.003s
FAIL
`
	if got := blamed(CIFailedTask{Name: "go-test", LogTail: goTest}); !slices.Equal(got, []string{"b"}) {
		t.Fatal("go test FAIL line", got)
	}
	build := `# github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store
internal/agentrt/store/landing_batch.go:12:3: undefined: batchFields2
FAIL	github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store [build failed]
`
	if got := blamed(CIFailedTask{Name: "go-build", Message: "\"step-test\" exited with code 1", LogTail: build}); !slices.Equal(got, []string{"a"}) {
		t.Fatal("build error", got)
	}
	// The relative path alone, without the package header, maps too.
	if got := blamed(CIFailedTask{LogTail: "api/internal/agentrt/store/landing_batch.go:7:2: syntax error\n"}); !slices.Equal(got, []string{"a"}) {
		t.Fatal("build position", got)
	}
	panicked := `panic: runtime error: index out of range [3] with length 3 [recovered]

goroutine 7 [running]:
testing.tRunner.func1.2({0x8a1b20, 0xc0000a4018})
	/usr/local/go/src/testing/testing.go:1632 +0x230
github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store/sub.Read(...)
	/workspace/source/api/internal/agentrt/store/sub/x.go:88 +0x1d
`
	if got := blamed(CIFailedTask{Name: "go-test", LogTail: panicked}); !slices.Equal(got, []string{"d"}) {
		t.Fatal("panic", got)
	}
	// Two TaskRuns: both packages blamed, in member order.
	if got := blamed(CIFailedTask{LogTail: build}, CIFailedTask{LogTail: goTest}); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatal("two TaskRuns", got)
	}
	// A console failure, a timeout, a read error: nothing maps.
	for _, tr := range []CIFailedTask{
		{Name: "console", Message: "npm ERR! Lifecycle script `test` failed", LogTail: "FAIL  src/pages/x.test.tsx > renders\nAssertionError"},
		{Name: "go-test", Message: "TaskRun \"go-test\" failed to finish within \"1h0m0s\""},
		{},
	} {
		if got := blamed(tr); len(got) != 0 {
			t.Fatal("blamed on nothing that maps", tr.Name, got)
		}
	}
	if got := BlameRed(CIFailure{ReadError: "forbidden"}, members); len(got) != 0 {
		t.Fatal("blamed with no TaskRuns", got)
	}
}

func TestWaveBatchFormation(t *testing.T) {
	t.Parallel()
	now := epoch.Add(time.Hour)
	members := []BatchCandidate{batchCand(1), batchCand(2), batchCand(3)}
	for i := range members {
		members[i].Facts.Rule.Landing.Limits.MaxBatch = 1
		members[i].Facts.Rule.Landing.Limits.MaxLandingsPerDay = 3
		members[i].Facts.ReservedToday = 2
	}
	// A later member may need a grant, but append order still determines
	// the lead: its existing batch material covers every member.
	f := &members[2].Facts
	f.GrantEnabled = true
	f.Changes = slices.Clone(f.Changes)
	f.Changes[0].OldPath = "api/internal/agentrt/store/example.go"
	f.Changes[0].NewPath = f.Changes[0].OldPath
	if reason, soft := PolicyChecks(*f, now); reason != "" || len(soft) == 0 || !MayAwaitGrant(*f, soft) {
		t.Fatal("fixture must allow waiting for a grant", reason, soft)
	}
	if got := FormBatch(members, now, true); !slices.Equal(batchTasks(got), batchTasks(members)) {
		t.Fatal("wave must consume one slot and retain append order", batchTasks(got))
	}
	for _, tc := range []struct {
		name   string
		refuse func(*Facts)
	}{
		{"quota", func(f *Facts) { f.ReservedToday = 3 }},
		{"tenant", func(f *Facts) { f.Tenant = "other" }},
		{"repo", func(f *Facts) { f.Repo = "bifrost-trade-infra" }},
		{"s5", func(f *Facts) { f.Operation.S5.Required = true }},
		{"policy", func(f *Facts) { f.GrantEnabled = false; f.Changes = members[2].Facts.Changes }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cands := slices.Clone(members)
			tc.refuse(&cands[1].Facts)
			if got := FormBatch(cands, now, true); len(got) != 0 {
				t.Fatal("refused member must refuse entire wave", batchTasks(got))
			}
		})
	}
	tooMany := make([]BatchCandidate, BatchMax+1)
	if got := FormBatch(tooMany, now, true); len(got) != 0 {
		t.Fatal("wave exceeds record member bound")
	}
}
