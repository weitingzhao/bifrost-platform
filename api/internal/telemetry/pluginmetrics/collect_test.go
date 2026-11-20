package pluginmetrics

import (
	"context"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	"reflect"
	"testing"
	"time"
)

func TestConcurrentOrderAndIndependentCancellation(t *testing.T) {
	started := make(chan context.Context, 2)
	release := make(chan struct{})
	result := make(chan []int, 1)
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		result <- collectProbes(parent, 8*time.Second, "test.collect", []func(context.Context) int{
			func(ctx context.Context) int { started <- ctx; <-ctx.Done(); return 1 },
			func(ctx context.Context) int { started <- ctx; <-release; return 2 },
		})
	}()
	first, second := <-started, <-started
	if first == second {
		t.Fatal("probes share a context")
	}
	for _, ctx := range []context.Context{first, second} {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 8*time.Second || time.Until(deadline) < 7*time.Second {
			t.Fatal("lost per-probe timeout")
		}
	}
	close(release)
	cancel()
	select {
	case got := <-result:
		if !reflect.DeepEqual(got, []int{1, 2}) {
			t.Fatalf("order changed: %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled collection stalled")
	}
}

func TestPanicLeavesZeroAndOtherProbeCompletes(t *testing.T) {
	before := safego.Contained()
	got := collectProbes(context.Background(), time.Second, "test.collect", []func(context.Context) int{
		func(context.Context) int { panic("single observation failed") },
		func(context.Context) int { return 7 },
	})
	if !reflect.DeepEqual(got, []int{0, 7}) {
		t.Fatalf("got %v", got)
	}
	if safego.Contained() != before+1 {
		t.Fatal("panic not counted")
	}
}

func TestDeadlineIsDeliveredToObservation(t *testing.T) {
	got := collectProbes(context.Background(), time.Millisecond, "test.collect", []func(context.Context) error{
		func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() },
	})
	if got[0] != context.DeadlineExceeded {
		t.Fatalf("got %v", got)
	}
}
