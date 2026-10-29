package stgrelease

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// panicWindows replaces one transport call with a panic and counts the
// renewal attempts that reach it.
type panicWindows struct {
	Windows
	endPanic, renewPanic bool
	renews               *atomic.Int64
}

func (w panicWindows) EndSTGWindow(ctx context.Context, old delivery.STGWindow) error {
	if w.endPanic {
		panic("test: EndSTGWindow")
	}
	return w.Windows.EndSTGWindow(ctx, old)
}

func (w panicWindows) RenewSTGWindow(ctx context.Context, old delivery.STGWindow) (delivery.STGWindow, error) {
	w.renews.Add(1)
	if w.renewPanic {
		panic("test: RenewSTGWindow")
	}
	return w.Windows.RenewSTGWindow(ctx, old)
}

func waitDone(t *testing.T, h *WindowHolder) {
	t.Helper()
	select {
	case <-h.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("holder did not finish")
	}
}

func TestReleaseCompletesAfterReleasePanic(t *testing.T) {
	t.Parallel()
	for _, inject := range []bool{false, true} {
		name := "control"
		if inject {
			name = "end-panic"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg, _, _ := leaseFixture()
			cfg.Windows = panicWindows{Windows: cfg.Windows, endPanic: inject, renews: new(atomic.Int64)}
			h, err := HoldWindow(context.Background(), cfg, leaseState(false), leaseBatch, 1)
			if err != nil {
				t.Fatal(err)
			}
			released := make(chan error, 1)
			go func() { released <- h.Release() }()
			select {
			case err := <-released:
				if inject && err == nil {
					t.Fatal("a release panic was reported as a successful release")
				}
				if !inject && err != nil {
					t.Fatalf("control release: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Release is stuck after the finisher panicked")
			}
			waitDone(t, h)
			if _, ok := <-h.Losses(); ok {
				t.Fatal("a release is not a lease loss")
			}
			observed := make(chan error, 1)
			go func() { _, err := h.Observe(context.Background()); observed <- err }()
			select {
			case err := <-observed:
				if !errors.Is(err, ErrWindowWait) {
					t.Fatalf("Observe after release: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Observe blocks after the finisher panicked")
			}
		})
	}
}

func TestRenewPanicLosesTheLeaseOnce(t *testing.T) {
	t.Parallel()
	cfg, f, _ := leaseFixture()
	pw := panicWindows{Windows: cfg.Windows, renewPanic: true, renews: new(atomic.Int64)}
	cfg.Windows = pw
	h, err := HoldWindow(context.Background(), cfg, leaseState(false), leaseBatch, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.clock.Step(time.Minute)
	waitDone(t, h)
	if h.Generation() != 0 {
		t.Fatal("a renewer panic left a live generation")
	}
	loss, ok := <-h.Losses()
	if !ok || loss.Generation != 1 || loss.Err == nil {
		t.Fatalf("renewer panic: want one lease_lost notification, got %+v ok=%v", loss, ok)
	}
	if _, ok := <-h.Losses(); ok {
		t.Fatal("more than one loss notification")
	}
	if err := h.Release(); err != nil {
		t.Fatalf("Release after a lost lease writes nothing and returns nil, got %v", err)
	}
	f.clock.Step(time.Minute)
	f.clock.Step(time.Minute)
	if n := pw.renews.Load(); n != 1 {
		t.Fatalf("renewals after the panic: want 1 attempt in all, got %d", n)
	}
	if _, err := h.Observe(context.Background()); !errors.Is(err, ErrWindowWait) {
		t.Fatalf("Observe after a lost lease: %v", err)
	}
}
