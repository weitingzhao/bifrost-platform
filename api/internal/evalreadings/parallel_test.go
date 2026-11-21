package evalreadings

import (
	"context"
	"sync"
	"testing"
	"time"
)

// 2026-11-20: the five Prometheus groups ran one after another under a
// 15-second handler timeout; a 7-day window has eight subqueries. This checks
// they now overlap and that the readings keep the same order as before.
func TestPromGroupsOverlapAndKeepOrder(t *testing.T) {
	var mu sync.Mutex
	inFlight, maxInFlight := 0, 0
	prom := func(ctx context.Context, expr string) ([]Sample, error) {
		mu.Lock()
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		return nil, nil
	}
	w := ClosedWindow(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC), 7)
	got := promGroups(context.Background(), prom, w)
	if maxInFlight < 2 {
		t.Fatalf("queries did not overlap: max in flight %d", maxInFlight)
	}
	var want []Reading
	for _, g := range []func(context.Context, Prom, Window) []Reading{promReadings, ciQueueReadings, busEventReadings, busOutageReadings, alertActivationReadings} {
		want = append(want, g(context.Background(), func(context.Context, string) ([]Sample, error) { return nil, nil }, w)...)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d readings, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i].Name {
			t.Fatalf("reading %d: got %s, want %s", i, got[i].Name, want[i].Name)
		}
	}
}
