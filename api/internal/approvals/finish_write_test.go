package approvals

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

func TestActionSucceededFinalWriteFailed(t *testing.T) {
	root := t.TempDir()
	b := newCAS()
	statefile.Use(b, root)
	t.Cleanup(func() { statefile.Use(nil, "") })
	auditPath := filepath.Join(root, "audit.json")
	svc := New(filepath.Join(root, "approvals"), actuation.NewAuditLog(auditPath))
	var calls atomic.Int32
	actions.RegisterExecutor("cordon_node", func(_ context.Context, params map[string]any) (any, error) {
		calls.Add(1)
		return map[string]any{"node": params["name"]}, nil
	})
	c := svc.create(context.Background(), "s", "cordon_node", "patch", "", map[string]any{"name": "node-a"})
	if c.Status != 201 {
		t.Fatalf("create = %d %v", c.Status, c.Body)
	}
	b.failUpdate = errors.New("disk full")
	b.allowBeforeFail = 1
	out := svc.approve(context.Background(), c.Approval.ID, "console")
	if out.Status != 500 || calls.Load() != 1 {
		t.Fatalf("approve = %d %v calls=%d", out.Status, out.Body, calls.Load())
	}
	if out.Body["status"] == StatusExecuted {
		t.Fatalf("unstored success was reported executed: %v", out.Body)
	}
	if !bytes.Contains([]byte(out.Body["error"].(string)), []byte("disk full")) {
		t.Fatalf("error = %v", out.Body["error"])
	}
	got, _ := svc.find(c.Approval.ID)
	if got.Status != StatusRunning {
		t.Fatalf("stored = %s, want the lease still running", got.Status)
	}
	if body, err := statefile.ReadFile(auditPath); err == nil && (bytes.Contains(body, []byte("approval.execute")) || bytes.Contains(body, []byte("approval.unknown"))) {
		t.Fatalf("unstored transition was emitted: %s", body)
	}

	b.failUpdate = nil
	svc.retryDue(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("the action ran again: %d", calls.Load())
	}
	got, _ = svc.find(c.Approval.ID)
	if got.Status != StatusExecuted {
		t.Fatalf("retried write = %s %s", got.Status, got.Error)
	}
	body, err := statefile.ReadFile(auditPath)
	if err != nil || !bytes.Contains(body, []byte("approval.execute")) {
		t.Fatalf("stored transition was not emitted: %v %s", err, body)
	}
}

func TestUnstoredFinishBecomesUnknownWhenTheLeaseLapses(t *testing.T) {
	root := t.TempDir()
	b := newCAS()
	statefile.Use(b, root)
	t.Cleanup(func() { statefile.Use(nil, "") })
	svc := New(filepath.Join(root, "approvals"), nil)
	base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return base })
	var calls atomic.Int32
	actions.RegisterExecutor("cordon_node", func(context.Context, map[string]any) (any, error) {
		calls.Add(1)
		return map[string]any{"ok": true}, nil
	})
	c := svc.create(context.Background(), "s", "cordon_node", "patch", "", map[string]any{"name": "node-b"})
	if c.Status != 201 {
		t.Fatalf("create = %d %v", c.Status, c.Body)
	}
	b.failUpdate = errors.New("disk full")
	b.allowBeforeFail = 1
	out := svc.approve(context.Background(), c.Approval.ID, "console")
	if out.Status != 500 || calls.Load() != 1 {
		t.Fatalf("approve = %d %v calls=%d", out.Status, out.Body, calls.Load())
	}
	b.failUpdate = nil
	svc.SetClock(func() time.Time { return base.Add(leaseFor + unknownGrace + time.Second) })
	svc.retryDue(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("the action ran again: %d", calls.Load())
	}
	got, _ := svc.find(c.Approval.ID)
	if got.Status != StatusUnknown {
		t.Fatalf("status = %s, want unknown after the lease lapsed", got.Status)
	}
}
