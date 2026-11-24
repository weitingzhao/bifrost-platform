package approvals

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// #697 reached the Owner as a second prompt while the Console confirmation was
// still in flight. Give the one GPU confirmation a short window to settle.
func TestInlineGPUShutdownDefersOnlyThePendingNotice(t *testing.T) {
	f := relay(t)
	svc, _, _ := twoPods(t)
	base := time.Date(2026, 11, 22, 20, 0, 0, 0, time.UTC)
	at := base
	svc.SetClock(func() time.Time { return at })
	create := func(role actuation.Role, name string) int {
		body := `{"inline_gpu_shutdown":true,"action":"poweroff_compute_node","reason":"Confirm shutdown","params":{"name":"` + name + `"}}`
		req := httptest.NewRequest(http.MethodPost, "/approvals", strings.NewReader(body))
		w := httptest.NewRecorder()
		svc.HandleCreate(w, req.WithContext(actuation.WithPrincipal(req.Context(), actuation.Principal{Role: role})))
		return w.Code
	}
	if got := create(actuation.RoleOperator, "gpu-server"); got != http.StatusForbidden {
		t.Fatalf("operator inline create = %d", got)
	}
	if got := create(actuation.RoleAdmin, "another-node"); got != http.StatusForbidden {
		t.Fatalf("other node inline create = %d", got)
	}
	if got := create(actuation.RoleAdmin, "gpu-server"); got != http.StatusCreated {
		t.Fatalf("GPU inline create = %d", got)
	}
	svc.NotifyCreated(context.Background())
	if n := len(f.got()); n != 0 {
		t.Fatalf("the confirmation still in flight notified %d time(s)", n)
	}
	at = base.Add(createdFallbackAfter)
	svc.notifyCreated(context.Background(), createdFallbackAfter)
	if n := len(f.got()); n != 1 {
		t.Fatalf("the unfinished approval should still notify, got %d", n)
	}
}

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
	c := a.create(context.Background(), "s", "ib_mode", "patch", "", map[string]any{"mode": "node-a"})
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
	c := a.create(context.Background(), "s", "ib_mode", "patch", "", map[string]any{"mode": "node-a"})
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
	c := a.create(context.Background(), "s", "ib_mode", "patch", "", map[string]any{"mode": "node-a"})
	a.NotifyCreated(context.Background())
	got := f.got()
	if len(got) != 1 || !strings.HasPrefix(got[0].Title, "Decide · #") || !strings.Contains(got[0].Title, "patch") {
		t.Fatalf("pushes = %+v", got)
	}
	f.mu.Lock()
	f.fail = true
	f.mu.Unlock()
	d := a.create(context.Background(), "s", "ib_mode", "patch", "", map[string]any{"mode": "node-b"})
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

// 2026-11-24: ntfy answered 429 to six first notices in an hour and each was
// tried once. A failed first notice is tried again after two minutes, three
// times in all, and each failure stays on the card.
func TestAFailedFirstNoticeIsTriedAgainSpaced(t *testing.T) {
	f := relay(t)
	f.mu.Lock()
	f.fail = true
	f.mu.Unlock()
	a, _, _ := twoPods(t)
	base := time.Now().UTC()
	at := base
	a.SetClock(func() time.Time { return at })
	c := a.create(context.Background(), "s", "ib_mode", "patch", "", map[string]any{"mode": "node-r"})
	failedOf := func() int {
		n := 0
		for _, d := range deliveriesOf(t, a, c.Approval.ID) {
			if d.Kind == "created" && d.Result == "failed" {
				n++
			}
		}
		return n
	}
	a.NotifyCreated(context.Background())
	a.NotifyCreated(context.Background()) // too soon: not again
	if n := failedOf(); n != 1 {
		t.Fatalf("after two passes at once: %d failures", n)
	}
	at = base.Add(createdRetryAfter + time.Second)
	a.NotifyCreated(context.Background())
	at = base.Add(2*createdRetryAfter + 2*time.Second)
	a.NotifyCreated(context.Background())
	at = base.Add(3*createdRetryAfter + 3*time.Second)
	a.NotifyCreated(context.Background()) // tries used up
	if n := failedOf(); n != createdTries {
		t.Fatalf("failures = %d, want %d", n, createdTries)
	}
	f.mu.Lock()
	f.fail = false
	f.mu.Unlock()
	d := a.create(context.Background(), "s", "ib_mode", "patch", "", map[string]any{"mode": "node-s"})
	f.mu.Lock()
	f.fail = true
	f.mu.Unlock()
	a.NotifyCreated(context.Background())
	f.mu.Lock()
	f.fail = false
	f.mu.Unlock()
	at = at.Add(createdRetryAfter + time.Second)
	a.NotifyCreated(context.Background())
	ds := deliveriesOf(t, a, d.Approval.ID)
	if len(ds) != 2 || ds[1].Result != "accepted" {
		t.Fatalf("a retry after the relay recovered: %+v", ds)
	}
}
