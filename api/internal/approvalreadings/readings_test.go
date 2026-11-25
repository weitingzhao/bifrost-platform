package approvalreadings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

// The approval records, counted. Origin: 2026-11-14, the Owner: what the Ops
// page shows is what the system measured; that day the coordinator had told
// him by word how many cards he was asked for and how many bought nothing,
// and the page showed none of it. The cases are the kinds of request that
// day had: approved in seconds and done (#535), approved and it changed
// nothing that mattered (a restart that did not fix the fault: the record
// says executed, so it counts as executed here: whether it should have been
// asked is not in the records), approved and failed, rejected, expired
// unapproved, still pending, a drill. A day is the Owner's day: a request at
// 04:30Z on the 15th was made on the 14th in Chicago.
func TestApprovalReadings(t *testing.T) {
	t.Parallel()
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	rec := func(n int, requester, status, created string, decideAfter time.Duration, more func(*approvals.Approval)) approvals.Approval {
		a := approvals.Approval{Number: n, Requester: requester, Status: status, CreatedAt: at(created)}
		if decideAfter > 0 {
			a.DecidedAt = a.CreatedAt.Add(decideAfter)
			a.ApprovedLine = "line"
		}
		if more != nil {
			more(&a)
		}
		return a
	}
	items := []approvals.Approval{
		rec(1, "line-a", approvals.StatusExecuted, "2026-11-14T16:25:59Z", 50*time.Second, func(a *approvals.Approval) { a.RequesterThread, a.WorkID = "line a, earlier", "LANE-a" }),
		rec(2, "line-a", approvals.StatusFailed, "2026-11-14T17:00:00Z", 10*time.Second, func(a *approvals.Approval) { a.RequesterThread, a.WorkID = "line a", "LANE-a" }),
		rec(3, "line-a", approvals.StatusUnknown, "2026-11-14T18:00:00Z", 100*time.Second, func(a *approvals.Approval) { a.WorkID = "TD-9" }),
		rec(4, "line-a", approvals.StatusRejected, "2026-11-14T19:00:00Z", 0, func(a *approvals.Approval) { a.DecidedAt = at("2026-11-14T19:01:00Z") }),
		rec(5, "line-a", approvals.StatusPending, "2026-11-14T20:00:00Z", 0, nil),
		rec(6, "line-a", approvals.StatusExpired, "2026-11-14T21:00:00Z", 0, nil),
		rec(7, "line-a", approvals.StatusRunning, "2026-11-14T22:00:00Z", 30*time.Second, nil),
		rec(8, "line-a", approvals.StatusPending, "2026-11-14T22:30:00Z", 0, func(a *approvals.Approval) { a.Drill = true }),
		// Approved, then never run before its deadline: an expiry and still an approval.
		rec(9, "line-b", approvals.StatusExpired, "2026-11-14T15:00:00Z", 20*time.Second, nil),
		// 04:30Z on the 15th is the 14th in Chicago.
		rec(10, "line-b", approvals.StatusExecuted, "2026-11-15T04:30:00Z", 40*time.Second, nil),
		rec(11, "line-b", approvals.StatusExecuted, "2026-11-13T23:00:00Z", 5*time.Second, nil),
	}
	got := Compute(items, loc, at("2026-11-15T05:00:00Z"))
	if got.Retained != 11 || got.Oldest != "2026-11-13T23:00:00Z" || got.Zone != "America/Chicago" || len(got.Days) != 2 || got.Days[0].Day != "2026-11-14" || got.Days[1].Day != "2026-11-13" {
		t.Fatalf("days: %+v", got)
	}
	a, b := got.Days[0].Groups[0], got.Days[0].Groups[1]
	if a.Requester != "line-a" || b.Requester != "line-b" {
		t.Fatalf("the requester with more requests comes first: %s %s", a.Requester, b.Requester)
	}
	ints := func(v []int) string {
		s := ""
		for _, n := range v {
			s += string(rune('0'+n%10)) + ","
		}
		return s
	}
	if ints(a.Requests) != "1,2,3,4,5,6,7," || ints(a.Approved) != "1,2,3,7," || ints(a.Executed) != "1," || ints(a.Failed) != "2," || ints(a.Unknown) != "3," ||
		ints(a.Rejected) != "4," || ints(a.Pending) != "5," || ints(a.Expired) != "6," || ints(a.Waiting) != "7," || ints(a.Drills) != "8," {
		t.Errorf("line-a: %+v", a)
	}
	// 10, 30, 50, 100 seconds: the median of an even count is the mean of the middle two.
	if a.ApproveSecondsMedian == nil || *a.ApproveSecondsMedian != 40 || a.Thread != "line a" || len(a.WorkIDs) != 2 || a.WorkIDs[0] != "LANE-a" {
		t.Errorf("line-a's median, thread and work ids: %v %q %v", a.ApproveSecondsMedian, a.Thread, a.WorkIDs)
	}
	if ints(b.Requests) != "9,0," || ints(b.Approved) != "9,0," || ints(b.Expired) != "9," || ints(b.Executed) != "0," || *b.ApproveSecondsMedian != 30 {
		t.Errorf("line-b: %+v", b)
	}
	readingsContract(t, "approvals.json", got)
	// No record: no day, and nothing is called zero that was not counted.
	none := Compute(nil, loc, at("2026-11-15T05:00:00Z"))
	if none.Retained != 0 || none.Oldest != "" || len(none.Days) != 0 || none.Days == nil {
		t.Errorf("no records: %+v", none)
	}
	readingsContract(t, "approvals-none.json", none)
}

// The same JSON contract is consumed by Console tests. No fixture is regenerated here.
func readingsContract(t *testing.T, name string, value any) {
	t.Helper()
	got, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("..", "server", "testdata", "contract", "readings", name))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(got, '\n'), want) {
		t.Fatalf("readings contract %s changed: %s", name, got)
	}
}

// Origin: the Owner, 2026-11-24 (approvals line, administration): only a
// change of law and the six classes should reach him (laws 31, 46); the
// rest is the gap, and the gap must come from the system, not a hand count
// (LAW-8). The 11-24 hand count was 101 cards in 5 days, about 95 daily.
func TestToOwnerSplitsLawSixAndDaily(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	at := now.Add(-time.Hour)
	items := []approvals.Approval{
		{Number: 1, Action: "law_amend", Status: approvals.StatusExecuted, CreatedAt: at},
		{Number: 2, Action: "owner_run_command", OwnerClass: "H5", Status: approvals.StatusExecuted, CreatedAt: at},
		{Number: 3, Action: "apply_manifest", Status: approvals.StatusExecuted, CreatedAt: at},
		{Number: 4, Action: "owner_run_command", Status: approvals.StatusPending, CreatedAt: at},
		{Number: 5, Action: "ib_mode", Status: approvals.StatusPending, CreatedAt: at, Drill: true},
	}
	r := Compute(items, time.UTC, now)
	if len(r.Days) != 1 {
		t.Fatalf("days = %+v", r.Days)
	}
	got := r.Days[0].ToOwner
	if fmt.Sprint(got.Law) != "[1]" || fmt.Sprint(got.Six) != "[2]" || fmt.Sprint(got.Daily) != "[3 4]" {
		t.Fatalf("to_owner = %+v", got)
	}
	// 2026-11-24, the UI line: a page counts each of the six classes and
	// reads their definition from the readings, not from a copy.
	if fmt.Sprint(got.SixByClass["H5"]) != "[2]" || len(got.SixByClass) != 6 || got.SixByClass["H1"] == nil {
		t.Fatalf("six_by_class = %+v", got.SixByClass)
	}
	if len(r.SixClasses) != 6 || r.SixClasses["H4"] == "" {
		t.Fatalf("six_classes = %+v", r.SixClasses)
	}
}
