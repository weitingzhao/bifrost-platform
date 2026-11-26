package approvals

import (
	"context"
	"net/http"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
)

// Origin: Crew (2026-11-22) and the workshop (2026-11-24, ledger C9) count
// the Owner's steps from the approval records, and the records did not say
// who decided: only channel=console, absent on rejections.
func TestDecidedBySaysWhoDecided(t *testing.T) {
	s, _ := lifetimeService(t)
	actions.RegisterExecutor("ib_maintenance", func(context.Context, map[string]any) (any, error) { return nil, nil })
	t.Cleanup(func() { actions.RegisterExecutor("ib_maintenance", nil) })
	mk := func() Approval {
		c := s.create(context.Background(), "sess-d", "ib_maintenance", "backup", "", nil)
		if c.Status != http.StatusCreated {
			t.Fatalf("create = %d %v", c.Status, c.Body)
		}
		return c.Approval
	}
	a := mk()
	in := approveInput{Channel: "console", ApprovalLine: CanonicalApprovalLine(a), ParamsHash: a.ParamsHash, By: "owner"}
	if out := s.approveWith(context.Background(), a.ID, in); out.Status != http.StatusOK {
		t.Fatalf("approve = %d %v", out.Status, out.Body)
	}
	b := mk()
	if out := s.rejectBy(b.ID, "no", "owner"); out.Status != http.StatusOK {
		t.Fatalf("reject = %d %v", out.Status, out.Body)
	}
	c := mk()
	if out := s.withdraw(c.ID, "sess-d", "not needed"); out.Status != http.StatusOK {
		t.Fatalf("withdraw = %d %v", out.Status, out.Body)
	}
	for id, want := range map[string]string{a.ID: "owner", b.ID: "owner", c.ID: "sess-d"} {
		got, _ := s.find(id)
		if got.DecidedBy != want {
			t.Errorf("%s decided_by = %q, want %q", id, got.DecidedBy, want)
		}
	}
}
