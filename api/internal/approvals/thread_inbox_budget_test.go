package approvals

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

func TestThreadInboxBacklogMustNotStarveHeldFinish(t *testing.T) {
	t.Parallel()
	s := New(filepath.Join(t.TempDir(), "approvals"), nil)
	now := time.Now().UTC()
	a := Approval{ID: "appr_held", Number: 1, Status: StatusRunning, Action: "review_fixture",
		CreatedAt: now, ExpiresAt: now.Add(time.Hour), Execution: &Execution{LeaseID: "fixture",
			LeaseExpiresAt: now.Add(-unknownGrace + 12*time.Second)}}
	if err := s.store.update(func(doc *file) error { doc.Approvals = []Approval{a}; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.store.update(func(doc *file) error {
		for i := 0; i < 160; i++ {
			doc.Approvals = append(doc.Approvals, Approval{ID: fmt.Sprintf("appr_old_%03d", i), Status: StatusFailed, RequesterThread: "local_review"})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	var calls atomic.Int32
	started := make(chan struct{})
	s.SetThreadInbox(func(ctx context.Context, _, _, _ string) error {
		once.Do(func() {
			// A computed finish appears after this loop's initial flush. The
			// state store is now healthy; only notification delivery delays it.
			s.rememberFinish(a.ID, "fixture", now, map[string]any{"ok": true}, nil)
			close(started)
		})
		calls.Add(1)
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	safego.Go("test-held-starvation", func() { defer close(done); s.RunHeldFinishes(ctx, time.Millisecond) })
	t.Cleanup(func() { cancel(); <-done })
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("loop did not start")
	}
	limit := time.NewTimer(21 * time.Second)
	defer limit.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for heldCount(s) > 0 {
		select {
		case <-limit.C:
			t.Fatal("held finish still not visited")
		case <-tick.C:
		}
	}
	cancel()
	<-done
	if _, err := s.sweptDoc(); err != nil {
		t.Fatal(err)
	}
	saved, found, err := s.Lookup(a.ID)
	t.Logf("elapsed=%s sends=%d found=%t status=%s result=%v held=%d error=%v", time.Since(now), calls.Load(), found, saved.Status, saved.Result, heldCount(s), err)
	if err != nil || !found || saved.Status != StatusExecuted || saved.Result == nil {
		t.Fatal("notification backlog caused the already computed finish to expire instead of being persisted")
	}
	if result, ok := saved.Result.(map[string]any); !ok || result["ok"] != true {
		t.Fatalf("computed result changed: %v", saved.Result)
	}
}
