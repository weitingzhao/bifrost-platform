package approvals

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// Origin: card #557, 2026-11-14 22:11 Chicago. The Owner approved an
// owner_run_command card while the release window was held; the command's
// precheck exited non-zero and the card failed, one approval wasted. Owner
// 22:35: a decided card may run up to 30 minutes late.

func exitCode(n int) *int { return &n }

// Positive control: exit 75 requeues; the card is claimable again after backoff.
func TestExit75RequeuesAndRunsAgain(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	rec, lease := claimed(t, svc)
	out := svc.result(rec.ID, resultInput{LeaseID: lease, ExitCode: exitCode(75)})
	if out.Status != http.StatusOK || out.Body["status"] != StatusApproved {
		t.Fatalf("exit 75 = %d %v", out.Status, out.Body)
	}
	got, _ := svc.get(rec.ID)
	if !strings.HasPrefix(got.Execution.LastRefusal, "等空档") || got.Execution.Attempts != 1 {
		t.Fatalf("waiting note = %q attempts %d", got.Execution.LastRefusal, got.Execution.Attempts)
	}
	pub := decisionBody(got)
	if pub["attempts"] != 1 || pub["last_refusal"] == "" {
		t.Fatalf("Owner view does not show the wait: %v", pub)
	}
	svc.SetClock(func() time.Time { return got.Execution.NextAttemptAt.Add(time.Second) })
	again := svc.claim(context.Background(), actuation.RoleAdmin, claimInput{ExecutorID: "o"})
	if again.Status != http.StatusOK || again.Body["approval"].(Approval).Execution.Attempts != 2 {
		t.Fatalf("reclaim = %d %v", again.Status, again.Body)
	}
}

// Negative control: any other non-zero exit still fails the card.
func TestExit1StillFails(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	rec, lease := claimed(t, svc)
	out := svc.result(rec.ID, resultInput{LeaseID: lease, ExitCode: exitCode(1)})
	if out.Body["status"] != StatusFailed {
		t.Fatalf("exit 1 = %v", out.Body)
	}
}

// Waiting past the limit (default 30 min from approval) fails with the reason.
func TestExit75AfterLimitFailsWithReason(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	rec, lease := claimed(t, svc)
	decided := svc.clock()
	svc.SetClock(func() time.Time { return decided.Add(31 * time.Minute) })
	out := svc.result(rec.ID, resultInput{LeaseID: lease, ExitCode: exitCode(75)})
	got, _ := svc.get(rec.ID)
	if out.Body["status"] != StatusFailed || !strings.Contains(got.Error, "等空档超时") {
		t.Fatalf("timeout = %v error %q", out.Body, got.Error)
	}
}

// The limit is configurable.
func TestExit75LimitConfigurable(t *testing.T) {
	t.Setenv("APPROVAL_TEMPFAIL_WAIT_MINUTES", "5")
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	rec, lease := claimed(t, svc)
	decided := svc.clock()
	svc.SetClock(func() time.Time { return decided.Add(6 * time.Minute) })
	out := svc.result(rec.ID, resultInput{LeaseID: lease, ExitCode: exitCode(75)})
	if out.Body["status"] != StatusFailed {
		t.Fatalf("5 minute limit not honoured: %v", out.Body)
	}
}

// A card whose execution deadline has passed is not rerun even inside 30 min.
func TestExit75NotRerunAfterCardDeadline(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	rec, lease := claimed(t, svc)
	got, _ := svc.get(rec.ID)
	decided := svc.clock()
	err := svc.store.update(func(doc *file) error {
		a, _ := doc.find(rec.ID)
		a.Execution.Deadline = decided.Add(2 * time.Minute)
		doc.put(a)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = got
	svc.SetClock(func() time.Time { return decided.Add(3 * time.Minute) })
	out := svc.result(rec.ID, resultInput{LeaseID: lease, ExitCode: exitCode(75)})
	if out.Body["status"] != StatusFailed {
		t.Fatalf("expired card was requeued: %v", out.Body)
	}
}

// Exit 75 with an executor error text is a real failure, not a wait.
func TestExit75WithErrorFails(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	rec, lease := claimed(t, svc)
	out := svc.result(rec.ID, resultInput{LeaseID: lease, ExitCode: exitCode(75), Error: "wrote half"})
	if out.Body["status"] != StatusFailed {
		t.Fatalf("%v", out.Body)
	}
}
