package progress

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/lineage"
)

// Fixture text is invented. Do not paste TECH_DEBT.md or WORK.md in here.
const debtDoc = `
## 待你签收

- **TD-7** — made up, waiting

## 条目

### TD-7

**P2 · sample · A made-up sign-off row**

- **状态**：待你签收
- **验收结果**：PASS 2026-10-01 fixture

### TD-8

**P3 · sample · Still being written**

- **状态**：在做
- **验收**：echo fixture
`

const workDoc = `
## 条目

### W-2

**LANE-C · Made-up console lane**

- **类别**：道
- **匹配**：LANE-C
- **状态**：在做
- **验收结果**：none yet

### W-3

**阶段 9 · Made-up plan**

- **类别**：计划
- **状态**：未开始
`

func TestAssembleJoinsFixtureCommits(t *testing.T) {
	now := time.Date(2026, 10, 7, 15, 0, 0, 0, time.UTC) // Wednesday; week starts Monday 10-05
	prodAt := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	old := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	commits := []lineage.Commit{
		{
			SHA: "aaa", Subject: "LANE-C land the fixture", Work: "LANE-C, TD-7",
			Session: "local_alpha", At: prodAt,
			Reached: []lineage.Reach{{Env: "prod", At: prodAt}, {Env: "stg", At: prodAt}},
		},
		{
			SHA: "bbb", Subject: "TD-8 still open", Work: "TD-8",
			Session: "local_beta", At: old,
		},
		{SHA: "ccc", Subject: "typo", Work: "unassigned", At: prodAt},
		{SHA: "ddd", Subject: "older note TD-8", At: old}, // missing Work trailer, still links by subject
		{SHA: "aaa", Subject: "dup", Work: "LANE-C", At: prodAt},
	}
	resp := Assemble(now, 3, debtDoc, workDoc, commits)

	if resp.Summary.AwaitingSignoff != 1 {
		t.Fatalf("awaiting = %d", resp.Summary.AwaitingSignoff)
	}
	if resp.Summary.InFlight != 3 { // TD-8 在做, W-2 在做, W-3 未开始
		t.Fatalf("in flight = %d want 3", resp.Summary.InFlight)
	}
	if resp.Summary.Stuck != 1 {
		t.Fatalf("stuck = %d want 1 (TD-8)", resp.Summary.Stuck)
	}
	if resp.Summary.ShippedThisWeek != 2 { // the PROD commit names both LANE-C and TD-7
		t.Fatalf("shipped = %d want 2", resp.Summary.ShippedThisWeek)
	}
	if resp.Summary.UnassignedCommits != 2 {
		t.Fatalf("unassigned = %d want 2", resp.Summary.UnassignedCommits)
	}

	byID := map[string]Item{}
	for _, it := range resp.Items {
		byID[it.ID] = it
	}
	lane := byID["W-2"]
	if lane.Category != "lane" || lane.CommitCount != 1 || lane.Stuck {
		t.Fatalf("lane = %+v", lane)
	}
	if len(lane.Environments) != 2 || lane.Environments[0] != "STG" || lane.Environments[1] != "PROD" {
		t.Fatalf("envs = %v", lane.Environments)
	}
	if len(lane.Threads) != 1 || lane.Threads[0].Session != "local_alpha" {
		t.Fatalf("threads = %+v", lane.Threads)
	}
	if !byID["TD-7"].AwaitingSignoff || byID["TD-7"].AcceptResult == "" || byID["TD-7"].Category != "debt" {
		t.Fatalf("td7 = %+v", byID["TD-7"])
	}
	if byID["TD-8"].CommitCount != 2 || !byID["TD-8"].Stuck {
		t.Fatalf("td8 = %+v", byID["TD-8"])
	}
	if byID["W-3"].Category != "plan" || byID["W-3"].CommitCount != 0 {
		t.Fatalf("plan = %+v", byID["W-3"])
	}
	// debt, then plan, then lane; numeric within a category
	if resp.Items[0].ID != "TD-7" || resp.Items[1].ID != "TD-8" {
		t.Fatalf("order = %s %s", resp.Items[0].ID, resp.Items[1].ID)
	}
}

func TestFindIDsDedupes(t *testing.T) {
	got := findIDs("LANE-C fix TD-253 and TD-253 W-4 NOTD-8")
	want := "LANE-C,TD-253,W-4"
	joined := ""
	for i, id := range got {
		if i > 0 {
			joined += ","
		}
		joined += id
	}
	if joined != want {
		t.Fatalf("got %s", joined)
	}
}

type fakeSource struct {
	debt, work string
	commits    []lineage.Commit
}

func (f fakeSource) ReadFile(_ context.Context, repo, ref, path string) (string, error) {
	if repo != infraRepo || ref != gitRef {
		return "", errFake("wrong ref")
	}
	switch path {
	case debtPath:
		return f.debt, nil
	case workPath:
		return f.work, nil
	default:
		return "", errFake("missing")
	}
}

func (f fakeSource) CommitsSince(context.Context, time.Time) ([]lineage.Commit, []string) {
	return f.commits, nil
}

type errFake string

func (e errFake) Error() string { return string(e) }

func TestHandlerShape(t *testing.T) {
	now := time.Date(2026, 10, 7, 15, 0, 0, 0, time.UTC)
	svc := NewService(fakeSource{debt: debtDoc, work: workDoc})
	svc.now = func() time.Time { return now }
	h := NewHandler(svc)
	rec := httptest.NewRecorder()
	h.HandleGet(rec, httptest.NewRequest(http.MethodGet, "/api/v1/progress?stuck_days=3", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.StuckAfterDays != 3 || resp.Summary.AwaitingSignoff != 1 || resp.Summary.InFlight != 3 {
		t.Fatalf("%+v", resp.Summary)
	}
	if len(resp.Items) != 4 {
		t.Fatalf("items %d", len(resp.Items))
	}
	// second call is cached and still the fixture, not a live file
	rec = httptest.NewRecorder()
	h.HandleGet(rec, httptest.NewRequest(http.MethodGet, "/api/v1/progress", nil))
	if rec.Body.Len() == 0 {
		t.Fatal("empty cache hit")
	}
}

func TestClampStuck(t *testing.T) {
	if clampStuck(0) != 3 || clampStuck(99) != 30 || clampStuck(5) != 5 {
		t.Fatalf("clamp %d %d %d", clampStuck(0), clampStuck(99), clampStuck(5))
	}
}
