package approvalfacts

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

func TestExternalApprovalFact(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	a := approvals.Approval{Number: 12, Status: approvals.StatusExpired, Action: "deploy", Tier: "T2", CreatedAt: at,
		Params: map[string]any{"repo": "bifrost-platform", "task_id": "invalid", "task": "t_01M59KGZ56VX9YCCCN9KCR9264", "op": "release:deploy", "reason": "private"}}
	f, ok := Fact(a)
	if !ok || !f.Expired || f.Repo != "bifrost-platform" || f.Task != a.Params["task"] || f.Op != "release:deploy" {
		t.Fatalf("fact = %+v, %v", f, ok)
	}
	if _, found := f.Body()["reason"]; found {
		t.Fatal("copied private params")
	}
	a.Params = map[string]any{"repo": "bad/repo", "task": "t_invalid", "op": "two words"}
	f, ok = Fact(a)
	if !ok || f.Repo != "" || f.Task != "" || f.Op != "" {
		t.Fatalf("invalid shapes retained: %+v", f)
	}
	a.Number = 0
	if _, ok := Fact(a); ok {
		t.Fatal("accepted missing number")
	}
	a.Number, a.Status = 12, ""
	if _, ok := Fact(a); ok {
		t.Fatal("accepted missing status")
	}
}

func TestExternalRecordApprovalObservationNeverReopensExpired(t *testing.T) {
	t.Parallel()
	at := time.Now().Add(-time.Hour)
	a := approvals.Approval{Number: 1, Status: approvals.StatusExpired, ExpiresAt: at}
	if _, ok := Observation(a, time.Now()); !ok {
		t.Fatal("terminal card lost")
	}
	a.Status = approvals.StatusPending
	if _, ok := Observation(a, time.Now()); ok {
		t.Fatal("expired pending card recorded")
	}
}
