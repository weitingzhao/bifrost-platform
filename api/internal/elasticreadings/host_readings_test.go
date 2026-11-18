package elasticreadings

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Origin for every test here: 2026-11-18, PROD /cluster/nodes took 2.5-5 s on
// 655 of 658 calls because each one waited out an SSH connect timeout to the
// powered-off gpu-server.

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newReach() (*HostReadings, *fakeClock) {
	c := &fakeClock{t: time.Unix(1_800_000_000, 0)}
	return &HostReadings{clock: c.now}, c
}

func TestHostReadingsFreshReadingDoesNotProbe(t *testing.T) {
	d, clock := newReach()
	var calls atomic.Int32
	probe := func(context.Context, string) bool { calls.Add(1); return true }

	if !d.Read(t.Context(), "gpu", probe) {
		t.Fatal("first read: want the probe's answer")
	}
	clock.advance(hostReadingFresh - time.Second)
	if !d.Read(t.Context(), "gpu", probe) {
		t.Fatal("fresh read: want the stored answer")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("probes: got %d want 1", got)
	}
}

// A slow probe must not hold up the page, and many polls start only one.
func TestHostReadingsAgingReadingServesAndRefreshesOnce(t *testing.T) {
	d, clock := newReach()
	d.store("gpu", false)
	clock.advance(hostReadingFresh)

	release := make(chan struct{})
	var calls atomic.Int32
	probe := func(context.Context, string) bool {
		calls.Add(1)
		<-release
		return true
	}
	for range 5 {
		if d.Read(t.Context(), "gpu", probe) {
			t.Fatal("aging read: want the stored answer while the probe runs")
		}
	}
	close(release)
	waitFor(t, func() bool { return d.Read(t.Context(), "gpu", probe) })
	if got := calls.Load(); got != 1 {
		t.Fatalf("probes: got %d want 1", got)
	}
}

func TestHostReadingsTooOldReadingWaitsForProbe(t *testing.T) {
	d, clock := newReach()
	d.store("gpu", false)
	clock.advance(hostReadingMaxAge)
	if !d.Read(t.Context(), "gpu", func(context.Context, string) bool { return true }) {
		t.Fatal("too-old read: want the new probe's answer")
	}
}

func TestHostReadingsCancelledProbeIsNotKept(t *testing.T) {
	d, _ := newReach()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if d.Read(ctx, "gpu", func(context.Context, string) bool { return false }) {
		t.Fatal("cancelled read: want the probe's answer")
	}
	if !d.Read(t.Context(), "gpu", func(context.Context, string) bool { return true }) {
		t.Fatal("a cancelled probe's false was kept as the host's reading")
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
