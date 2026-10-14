package approvals

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
)

func waitHeldLoop(t *testing.T, condition func() bool) {
	t.Helper()
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(time.Millisecond) {
		if condition() {
			return
		}
	}
	t.Fatal("result loop did not reach the expected state")
}

func heldCount(s *Service) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.held)
}

func startHeldTestLoop(t *testing.T, s *Service, workers bool) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if workers {
			s.RunRetries(ctx, 2*time.Millisecond)
		} else {
			s.RunHeldFinishes(ctx, 2*time.Millisecond)
		}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("result loop did not stop on cancellation")
		}
	})
	return cancel
}

// API recovery and grace cleanup were red before TD-334 (there was no API
// result loop). Workers recovery was green through the existing RunRetries.
func TestHeldLoopPersistsSynchronousFinish(t *testing.T) {
	for _, workers := range []bool{false, true} {
		name := "api"
		if workers {
			name = "workers"
		}
		t.Run(name, func(t *testing.T) {
			s, b := lifetimeService(t)
			base := time.Now().UTC()
			s.SetClock(func() time.Time { return base })
			a := lifetimeApproval(t, s)
			var calls atomic.Int32
			actions.RegisterExecutor(a.Action, func(context.Context, map[string]any) (any, error) {
				calls.Add(1)
				return map[string]any{"finished": true}, nil
			})
			t.Cleanup(func() { actions.RegisterExecutor(a.Action, nil) })
			b.mu.Lock()
			b.failUpdate = errors.New("final write unavailable")
			b.allowBeforeFail = 1
			b.mu.Unlock()
			// Run the loop before the synchronous execution ends, covering a
			// tick racing the first failed write and rememberFinish.
			startHeldTestLoop(t, s, workers)
			out := s.approve(context.Background(), a.ID, "console")
			if out.Status != 500 || calls.Load() != 1 || heldCount(s) != 1 {
				t.Fatalf("approve=%d calls=%d held=%d", out.Status, calls.Load(), heldCount(s))
			}
			s.SetClock(func() time.Time { return base.Add(15 * time.Second) })
			b.mu.Lock()
			b.failUpdate = nil
			b.mu.Unlock()
			waitHeldLoop(t, func() bool {
				got, ok := s.find(a.ID)
				return ok && got.Status == StatusExecuted && heldCount(s) == 0
			})
			got, _ := s.find(a.ID)
			result, _ := got.Result.(map[string]any)
			if calls.Load() != 1 || got.Execution.Attempts != 1 || result["finished"] != true {
				t.Fatalf("stored=%+v calls=%d", got, calls.Load())
			}
		})
	}
}

func TestHeldLoopDropsExpiredResult(t *testing.T) {
	s, b := lifetimeService(t)
	base := time.Now().UTC()
	s.SetClock(func() time.Time { return base })
	a := lifetimeApproval(t, s)
	var calls atomic.Int32
	actions.RegisterExecutor(a.Action, func(context.Context, map[string]any) (any, error) {
		calls.Add(1)
		return "completed", nil
	})
	t.Cleanup(func() { actions.RegisterExecutor(a.Action, nil) })
	b.mu.Lock()
	b.failUpdate = errors.New("storage remains unavailable")
	b.allowBeforeFail = 1
	b.mu.Unlock()
	if out := s.approve(context.Background(), a.ID, "console"); out.Status != 500 {
		t.Fatalf("approve=%d", out.Status)
	}
	startHeldTestLoop(t, s, false)
	s.SetClock(func() time.Time { return base.Add(leaseFor + unknownGrace + time.Second) })
	waitHeldLoop(t, func() bool { return heldCount(s) == 0 })
	// The flush-only loop must not sweep. Once storage recovers, the normal
	// expiry rule (as used by a read or workers) persists unknown.
	b.mu.Lock()
	b.failUpdate = nil
	b.mu.Unlock()
	if _, err := s.sweptDoc(); err != nil {
		t.Fatal(err)
	}
	got, _ := s.find(a.ID)
	if got.Status != StatusUnknown || got.Result != nil || calls.Load() != 1 || got.Execution.Attempts != 1 {
		t.Fatalf("stored=%+v calls=%d", got, calls.Load())
	}
}

func TestHeldLoopCancellation(t *testing.T) {
	s, _ := lifetimeService(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.RunHeldFinishes(ctx, time.Hour)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled loop waited for its ticker")
	}
}

// Red before the TD-334 review fix: stopping the retry loop cancelled the
// action it was running (recorded failed although it may have finished), and
// the loop went on to claim the next due approval and run it on the
// cancelled context, recording failed an approval that never ran.
func TestRetryLoopStopNeitherCutsTheRunningActionNorClaimsAnother(t *testing.T) {
	s, _ := lifetimeService(t)
	base := time.Now().UTC()
	var now atomic.Int64
	now.Store(base.UnixNano())
	s.SetClock(func() time.Time { return time.Unix(0, now.Load()).UTC() })
	first := lifetimeApproval(t, s)
	second := lifetimeApproval(t, s)
	var refuse, sawCancel atomic.Bool
	var calls atomic.Int32
	refuse.Store(true)
	running := make(chan struct{}, 4)
	release := make(chan struct{})
	actions.RegisterExecutor(first.Action, func(ctx context.Context, _ map[string]any) (any, error) {
		calls.Add(1)
		if refuse.Load() {
			return nil, actions.Transient("release window held by another session")
		}
		running <- struct{}{}
		<-release
		if ctx.Err() != nil {
			sawCancel.Store(true)
			return nil, ctx.Err()
		}
		return map[string]any{"finished": true}, nil
	})
	t.Cleanup(func() { actions.RegisterExecutor(first.Action, nil) })
	for _, a := range []Approval{first, second} {
		s.approve(context.Background(), a.ID, "console")
		if got, _ := s.find(a.ID); got.Status != StatusApproved {
			t.Fatalf("%s after a transient refusal = %s, want approved", a.ID, got.Status)
		}
	}
	refuse.Store(false)
	now.Store(base.Add(10 * time.Minute).UnixNano()) // both retries are due, well before the hour's deadline
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.RunRetries(ctx, 2*time.Millisecond)
	}()
	select {
	case <-running:
	case <-time.After(2 * time.Second):
		t.Fatal("no retry started")
	}
	cancel() // shutdown arrives while one action is running
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("retry loop did not stop")
	}
	if sawCancel.Load() {
		t.Fatal("the running action saw the loop's cancellation")
	}
	var executed, approved int
	for _, a := range []Approval{first, second} {
		got, _ := s.find(a.ID)
		switch got.Status {
		case StatusExecuted:
			executed++
		case StatusApproved:
			approved++
		default:
			t.Fatalf("%s = %s", a.ID, got.Status)
		}
	}
	if executed != 1 || approved != 1 || calls.Load() != 3 {
		t.Fatalf("executed=%d approved=%d calls=%d, want 1, 1 and 3 (two refusals, one run)", executed, approved, calls.Load())
	}
}
