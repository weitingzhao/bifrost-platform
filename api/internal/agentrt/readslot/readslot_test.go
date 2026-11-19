package readslot

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// A store read that does not take a context still ends with it, and reads
// that never return hold at most the store's slots (review 161 B1).
func TestSourceReadsAreBounded(t *testing.T) {
	reads := New(2)
	release := make(chan struct{})
	var started atomic.Int32
	stuck := func() (int, bool, error) { started.Add(1); <-release; return 1, true, nil }
	watchdog := time.AfterFunc(10*time.Second, func() { panic("a source read did not return") })
	defer watchdog.Stop()
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		start := time.Now()
		_, ok, err := Within(ctx, reads, stuck)
		cancel()
		if ok || !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
			t.Fatalf("read %d = %v %v after %s", i, ok, err, time.Since(start))
		}
	}
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		_, ok, err := Within(ctx, reads, stuck)
		cancel()
		if ok || !errors.Is(err, ErrBusy) {
			t.Fatalf("a read past the slots = %v %v", ok, err)
		}
	}
	if n := started.Load(); n != 2 {
		t.Fatalf("%d reads started; two slots", n)
	}
	close(release)
	// both slots come back once the stuck reads return
	for deadline := time.Now().Add(5 * time.Second); len(reads.slots) != 0; {
		if time.Now().After(deadline) {
			t.Fatalf("%d slots still held after the reads returned", len(reads.slots))
		}
		time.Sleep(10 * time.Millisecond)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if v, ok, err := Within(context.Background(), reads, func() (int, bool, error) { return 7, true, nil }); err == nil {
			if !ok || v != 7 {
				t.Fatalf("after the stuck reads returned = %v %v", v, ok)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the slots were not freed when the reads returned")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
