package approvals

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
)

// Origin: #607 (2026-11-17): an apply_manifest card said executed while its
// apply run failed with "commit is not reachable from main"; #604 and #608
// succeeded and said the same, which only meant "started". The card now ends
// when the run ends.

func applyCard(t *testing.T, svc *Service, outcome ApplyOutcome) Approval {
	t.Helper()
	actions.RegisterExecutor("apply_manifest", func(context.Context, map[string]any) (any, error) {
		return map[string]any{"apply_run": "apply-p1-1", "plan_id": "p1"}, nil
	})
	svc.FollowUp = func(ctx context.Context, rec Approval, result any) (any, error) {
		return FollowApplyRun(ctx, rec, result, outcome, time.Millisecond)
	}
	c := svc.create(context.Background(), "s", "apply_manifest", "apply the plan", "", map[string]any{"plan_id": "p1"})
	if c.Status != 201 {
		t.Fatalf("create = %d %v", c.Status, c.Body)
	}
	svc.approve(context.Background(), c.Approval.ID, "console")
	got, _ := svc.get(c.Approval.ID)
	return got
}

// Positive control: the run succeeds after it was seen running; the card is executed.
func TestAnApplyCardIsExecutedWhenItsRunSucceeds(t *testing.T) {
	svc := New(t.TempDir()+"/approvals", nil)
	polls := 0
	got := applyCard(t, svc, func(context.Context, string) (bool, bool, string, error) {
		polls++
		return polls >= 3, true, "", nil
	})
	if got.Status != StatusExecuted || polls < 3 {
		t.Fatalf("status %s after %d polls", got.Status, polls)
	}
}

// The case #607 was: the run failed; the card must say failed, with the run's words.
func TestAnApplyCardFailsWhenItsRunFails(t *testing.T) {
	svc := New(t.TempDir()+"/approvals", nil)
	got := applyCard(t, svc, func(context.Context, string) (bool, bool, string, error) {
		return true, false, "Failed: commit is not reachable from main", nil
	})
	if got.Status != StatusFailed || !strings.Contains(got.Error, "not reachable from main") {
		t.Fatalf("status %s error %q", got.Status, got.Error)
	}
}

func TestFollowApplyRunEdges(t *testing.T) {
	apply := Approval{Action: "apply_manifest"}
	started := map[string]any{"apply_run": "r1"}
	never := func(context.Context, string) (bool, bool, string, error) {
		t.Fatal("the run was read")
		return false, false, "", nil
	}
	// Another action, or no run named: the answer passes through unread.
	if got, err := FollowApplyRun(context.Background(), Approval{Action: "cordon_node"}, started, never, time.Millisecond); err != nil || got == nil {
		t.Fatalf("another action: %v %v", got, err)
	}
	if got, err := FollowApplyRun(context.Background(), apply, map[string]any{"plan_id": "p"}, never, time.Millisecond); err != nil || got == nil {
		t.Fatalf("no run named: %v %v", got, err)
	}
	// A read that fails once is tried again; the run's answer then counts.
	calls := 0
	got, err := FollowApplyRun(context.Background(), apply, started, func(context.Context, string) (bool, bool, string, error) {
		calls++
		if calls == 1 {
			return false, false, "", errors.New("api timeout")
		}
		return true, true, "", nil
	}, time.Millisecond)
	if err != nil || got.(map[string]any)["apply_run_status"] != "Succeeded" || got.(map[string]any)["apply_run"] != "r1" {
		t.Fatalf("a retried read: %v %v", got, err)
	}
	// Still running when the wait ends: the card is executed and says the run
	// was only started; it is not told as a success of the run.
	got, err = followApplyRun(context.Background(), apply, started, func(context.Context, string) (bool, bool, string, error) { return false, false, "", nil }, time.Millisecond, 30*time.Millisecond)
	if status, _ := got.(map[string]any)["apply_run_status"].(string); err != nil || !strings.HasPrefix(status, "started") {
		t.Fatalf("still running at the bound: %v %v", got, err)
	}
	// Unreadable to the end: an error that says it could not be read.
	_, err = followApplyRun(context.Background(), apply, started, func(context.Context, string) (bool, bool, string, error) {
		return false, false, "", errors.New("forbidden")
	}, time.Millisecond, 30*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Fatalf("unreadable: %v", err)
	}
}
