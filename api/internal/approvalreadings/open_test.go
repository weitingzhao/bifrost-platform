package approvalreadings

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

// Origin: the Owner, 2026-11-21 ("看得见"): the Approvals line listed what the
// system cannot see; whose move a card is and for how long, a card waiting out
// "not now", and approvals that timed out waiting for a slot were stored but
// shown nowhere. These readings come from the records as they are.
func TestOpenCardsSayWhoseMoveAndSinceWhen(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	ago := func(m int) time.Time { return now.Add(-time.Duration(m) * time.Minute) }
	items := []approvals.Approval{
		{Number: 1, Action: "owner_run_command", Status: approvals.StatusPending, CreatedAt: ago(30)},
		{Number: 2, Action: "apply_manifest", Status: approvals.StatusApproved, CreatedAt: ago(20), DecidedAt: ago(10)},
		{Number: 3, Action: "owner_run_command", Status: approvals.StatusApproved, CreatedAt: ago(40), DecidedAt: ago(15),
			Execution: &approvals.Execution{LastRefusal: "等空档（命令退出 75）", NextAttemptAt: now.Add(time.Minute)}},
		{Number: 4, Action: "start_pipeline_run", Status: approvals.StatusRunning, CreatedAt: ago(9), DecidedAt: ago(8),
			Execution: &approvals.Execution{ExecutorID: "platform:x", StartedAt: ago(5)}},
		{Number: 5, Action: "owner_run_command", Status: approvals.StatusFailed, CreatedAt: ago(90), Error: "等空档超时：批准后等了 30 分钟"},
		{Number: 6, Action: "cordon_node", Status: approvals.StatusExecuted, CreatedAt: ago(60)},
		{Number: 7, Action: "cordon_node", Status: approvals.StatusPending, CreatedAt: ago(1), Drill: true},
	}
	r := Compute(items, time.UTC, now)
	want := map[int]struct {
		holder  string
		minutes int
	}{1: {"owner", 30}, 2: {"executor", 10}, 3: {"waiting", 15}, 4: {"running", 5}}
	if len(r.Open) != len(want) {
		t.Fatalf("open = %+v", r.Open)
	}
	for _, c := range r.Open {
		w, ok := want[c.Number]
		if !ok || c.Holder != w.holder || c.Minutes != w.minutes {
			t.Fatalf("card #%d: %+v, want %+v", c.Number, c, w)
		}
	}
	if r.Open[0].Number != 1 { // oldest first: #1 since 30 minutes ago
		t.Fatalf("order = %+v", r.Open)
	}
	if r.Open[1].Refusal == "" && r.Open[1].Number == 3 {
		t.Fatal("the waiting card does not say why")
	}
	if len(r.WaitTimeouts) != 1 || r.WaitTimeouts[0] != 5 {
		t.Fatalf("wait timeouts = %v", r.WaitTimeouts)
	}
}
