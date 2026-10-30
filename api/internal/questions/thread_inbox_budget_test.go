package questions

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

func TestThreadInboxBacklogMustNotStarveSweep(t *testing.T) {
	t.Parallel()
	svc := New(filepath.Join(t.TempDir(), "questions"), nil)
	now := time.Now().UTC()
	var due atomic.Bool
	svc.SetClock(func() time.Time {
		if due.Load() {
			return now.Add(2 * time.Hour)
		}
		return now
	})
	id := "q_aaaaaaaaaaaaaaaa"
	if err := svc.store.update(func(doc *document) error {
		doc.Questions = []Question{{ID: id, Number: 1, Status: StatusOpen, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.update(func(doc *document) error {
		for i := 0; i < 160; i++ {
			doc.Questions = append(doc.Questions, Question{ID: fmt.Sprintf("q_%016x", i), Status: StatusAnswered, RequesterThread: "local_test", Answer: &Answer{By: "owner", At: now}})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	svc.SetThreadInbox(func(ctx context.Context, _, _, _ string) error {
		due.Store(true) // becomes due after the loop's initial Sweep
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
	safego.Go("test-question-inbox-budget", func() { defer close(done); svc.RunSweep(ctx, time.Millisecond) })
	t.Cleanup(func() { cancel(); <-done })
	until := time.Now().Add(13 * time.Second)
	for {
		saved, found, err := svc.Lookup(id)
		if err != nil || !found {
			t.Fatal("lost question")
		}
		if saved.Status == StatusExpired {
			break
		}
		if time.Now().After(until) {
			t.Fatal("delivery backlog starved Sweep")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
