package approvalrules

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

func grantable() approvals.Approval {
	return approvals.Approval{
		ID: "appr_1", Number: 7, ConfirmedNumber: 7, Action: "act", Tier: string(actions.TierD),
		Status: approvals.StatusRunning, ParamsHash: "h", Channel: "console",
		DecidedAt: time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC),
		Params:    map[string]any{"env": "prod"},
	}
}

// Origin: 2026-11-18, decoupling item 5: the read-back moved out of
// api/internal/server and no test named its refusals, so a slip in the move
// would not have shown. Each case breaks one condition of a grantable record
// and pins the refusal's text; the first case is the positive control.
func TestCheckGrantRecordRefusesEachConditionByName(t *testing.T) {
	if err := checkGrantRecord(grantable(), "act", "h", "h", true); err != nil {
		t.Fatalf("positive control: %v", err)
	}
	for _, c := range []struct {
		name       string
		edit       func(*approvals.Approval)
		hash, ctxH string
		prod       bool
		want       string
	}{
		{"action", func(a *approvals.Approval) { a.Action = "other" }, "h", "h", true, "approval appr_1 is for other, not act"},
		{"tier", func(a *approvals.Approval) { a.Tier = "C" }, "h", "h", true, "approval appr_1 is tier C, not D"},
		{"status", func(a *approvals.Approval) { a.Status = approvals.StatusPending }, "h", "h", true, "approval appr_1 is " + approvals.StatusPending + ", not running"},
		{"no hash", func(*approvals.Approval) {}, "", "", true, "approval appr_1's params_hash does not match the parameters executed"},
		{"stored hash", func(a *approvals.Approval) { a.ParamsHash = "x" }, "h", "h", true, "approval appr_1's params_hash does not match the parameters executed"},
		{"context hash", func(*approvals.Approval) {}, "h", "x", true, "approval appr_1's params_hash does not match the parameters executed"},
		{"drill", func(a *approvals.Approval) { a.Drill = true }, "h", "h", true, "approval appr_1 is a drill"},
		{"channel", func(a *approvals.Approval) { a.Channel = "mcp" }, "h", "h", true, `approval appr_1 was approved on "mcp"; a landing grant needs the Console`},
		{"number", func(a *approvals.Approval) { a.ConfirmedNumber = 8 }, "h", "h", true, "approval appr_1 was approved without its number confirmed"},
		{"decided", func(a *approvals.Approval) { a.DecidedAt = time.Time{} }, "h", "h", true, "approval appr_1 has no decision time"},
		{"not prod", func(*approvals.Approval) {}, "h", "h", false, "a landing grant's approval is PROD's: "},
		{"env", func(a *approvals.Approval) { a.Params = map[string]any{"env": "stg"} }, "h", "h", true, "a landing grant's approval is PROD's: "},
	} {
		rec := grantable()
		c.edit(&rec)
		err := checkGrantRecord(rec, "act", c.hash, c.ctxH, c.prod)
		if err == nil || !strings.HasPrefix(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
}

// Origin: the same move. The context is looked at before the service: a call
// outside an approval's execution is refused as that, service or no service.
func TestGrantApprovalNeedsAnExecutionThenAService(t *testing.T) {
	_, err := GrantApproval(context.Background(), nil, "act", nil)
	if err == nil || err.Error() != "act runs only as an approval's execution" {
		t.Fatalf("outside an execution: %v", err)
	}
	ctx := actions.WithCreateAttempt(context.Background(), "appr_1", 1)
	_, err = GrantApproval(ctx, nil, "act", nil)
	if err == nil || err.Error() != "no approval service to read the approval back from" {
		t.Fatalf("no service: %v", err)
	}
}
