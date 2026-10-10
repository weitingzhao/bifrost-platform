package approvals

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

func TestFinishPlatformDoesNotRequeuePastGrace(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return base })
	rec, lease := claimed(t, svc)

	inside := base.Add(leaseFor + time.Minute)
	svc.SetClock(func() time.Time { return inside })
	out, _, err := svc.finishPlatform(rec.ID, lease, base, nil, actions.Transient("kube API unreachable"))
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != StatusApproved {
		t.Fatalf("transient inside grace = %s (%s), want requeue", out.Status, out.Error)
	}

	svc.SetClock(func() time.Time { return inside.Add(backoff(2) + time.Second) })
	again := svc.claim(context.Background(), actuation.RoleAdmin, claimInput{ExecutorID: "owner-mac", ID: rec.ID})
	if again.Status != 200 {
		t.Fatalf("reclaim = %d %v", again.Status, again.Body)
	}
	lease2 := again.Body["lease_id"].(string)
	stored, _ := svc.find(rec.ID)
	past := stored.Execution.LeaseExpiresAt.Add(unknownGrace + time.Second)
	svc.SetClock(func() time.Time { return past })
	out, evs, err := svc.finishPlatform(rec.ID, lease2, past, nil, actions.Transient("kube API unreachable"))
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != StatusUnknown {
		t.Fatalf("transient past grace = %s, want unknown", out.Status)
	}
	got, _ := svc.find(rec.ID)
	if got.Status != StatusUnknown || !got.Execution.NextAttemptAt.IsZero() || got.Execution.LateResult {
		t.Fatalf("stored = %s next=%s late=%v", got.Status, got.Execution.NextAttemptAt, got.Execution.LateResult)
	}
	for _, e := range evs {
		if e.action == "approval.queue" {
			t.Fatalf("past-grace refusal requeued: %+v", evs)
		}
	}
}

func TestBackgroundRenewRecordsStoreErrorAndLostLease(t *testing.T) {
	root := t.TempDir()
	b := newCAS()
	statefile.Use(b, root)
	t.Cleanup(func() { statefile.Use(nil, "") })
	auditPath := filepath.Join(root, "audit.json")
	audit := actuation.NewAuditLog(auditPath)
	svc := New(filepath.Join(root, "approvals"), audit)
	base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return base })
	rec, lease := claimed(t, svc)

	b.failUpdate = errors.New("disk full token=SUPERSECRETVALUE")
	if !svc.backgroundRenew(rec.ID, lease) {
		t.Fatal("a store error should leave the heartbeat running")
	}
	body, err := statefile.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("approval.renew")) || bytes.Contains(body, []byte("SUPERSECRETVALUE")) || !bytes.Contains(body, []byte("[redacted]")) {
		t.Fatalf("store error audit = %s", body)
	}
	still, _ := svc.find(rec.ID)
	if still.Status != StatusRunning {
		t.Fatalf("failed renew changed status to %s", still.Status)
	}

	b.failUpdate = nil
	svc.SetClock(func() time.Time { return base.Add(leaseFor + unknownGrace + time.Second) })
	if svc.backgroundRenew(rec.ID, lease) {
		t.Fatal("a lost lease should stop the heartbeat")
	}
	got, _ := svc.find(rec.ID)
	if got.Status != StatusUnknown {
		t.Fatalf("status = %s, want unknown", got.Status)
	}
	body, _ = statefile.ReadFile(auditPath)
	if !bytes.Contains(body, []byte("approval.unknown")) {
		t.Fatalf("lost lease was not recorded: %s", body)
	}
}
