package approvals

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// Origin: 2026-11-18, the first notice of a card moves to the bus: the Owner's
// phone rang twice for each card while both paths ran. Whoever gets there, the
// bus's event or the loop, in either worker, a card is pushed once, and the
// loop gives the bus its time first.
func TestACardsFirstNoticeGoesOutOnce(t *testing.T) {
	f := relay(t)
	a, b, _ := twoPods(t)
	base := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	at := base
	a.SetClock(func() time.Time { return at })
	b.SetClock(func() time.Time { return at })
	c := a.create(context.Background(), "s", "cordon_node", "patch", "", map[string]any{"name": "node-a"})
	if c.Status != http.StatusCreated {
		t.Fatalf("create = %d %v", c.Status, c.Body)
	}
	if n := len(f.got()); n != 0 {
		t.Fatalf("creating the card pushed %d", n)
	}
	a.notifyCreated(context.Background(), createdFallbackAfter)
	if n := len(f.got()); n != 0 {
		t.Fatalf("the loop did not wait for the bus: %d", n)
	}
	var wg sync.WaitGroup
	for _, s := range []*Service{a, b} {
		wg.Add(1)
		go func() { defer wg.Done(); s.NotifyCreated(context.Background()) }()
	}
	wg.Wait()
	at = base.Add(time.Minute)
	a.notifyCreated(context.Background(), createdFallbackAfter)
	if n := len(f.got()); n != 1 {
		t.Fatalf("pushes = %d, want 1", n)
	}
	if ds := deliveriesOf(t, a, c.Approval.ID); len(ds) != 1 || ds[0].Kind != "created" || ds[0].Result != "accepted" {
		t.Fatalf("deliveries = %+v", ds)
	}
}

// The positive control of the wait: with no event, the loop sends it.
func TestTheLoopSendsAFirstNoticeTheBusMissed(t *testing.T) {
	f := relay(t)
	a, _, _ := twoPods(t)
	base := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	at := base
	a.SetClock(func() time.Time { return at })
	c := a.create(context.Background(), "s", "cordon_node", "patch", "", map[string]any{"name": "node-a"})
	if c.Status != http.StatusCreated {
		t.Fatalf("create = %d %v", c.Status, c.Body)
	}
	at = base.Add(createdFallbackAfter)
	a.notifyCreated(context.Background(), createdFallbackAfter)
	if n := len(f.got()); n != 1 {
		t.Fatalf("pushes = %d, want 1", n)
	}
}

// TD-286, kept when the notice moved: the notice says what the card asks, and
// a push that failed stays on the card with its reason and is not repeated.
func TestAFirstNoticeSaysTheCardAndKeepsItsFailure(t *testing.T) {
	f := relay(t)
	a, _, _ := twoPods(t)
	c := a.create(context.Background(), "s", "cordon_node", "patch", "", map[string]any{"name": "node-a"})
	a.NotifyCreated(context.Background())
	got := f.got()
	if len(got) != 1 || !strings.HasPrefix(got[0].Title, "Decide · #") || !strings.Contains(got[0].Title, "patch") {
		t.Fatalf("pushes = %+v", got)
	}
	f.mu.Lock()
	f.fail = true
	f.mu.Unlock()
	d := a.create(context.Background(), "s", "cordon_node", "patch", "", map[string]any{"name": "node-b"})
	a.NotifyCreated(context.Background())
	a.NotifyCreated(context.Background())
	ds := deliveriesOf(t, a, d.Approval.ID)
	if len(ds) != 1 || ds[0].Kind != "created" || ds[0].Result != "failed" || ds[0].Error == "" {
		t.Fatalf("deliveries = %+v", ds)
	}
	if n := len(deliveriesOf(t, a, c.Approval.ID)); n != 1 {
		t.Fatalf("the first card has %d deliveries", n)
	}
}
