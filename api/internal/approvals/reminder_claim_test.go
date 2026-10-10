package approvals

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTwoWorkersSendOneReminder(t *testing.T) {
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
	at = base.Add(4 * time.Hour)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		a.remindDue(context.Background())
	}()
	go func() {
		defer wg.Done()
		<-start
		b.remindDue(context.Background())
	}()
	close(start)
	wg.Wait()
	if n := len(f.got()); n != 1 {
		t.Fatalf("pushes = %d, want 1: %+v", n, f.got())
	}
	ds := deliveriesOf(t, a, c.Approval.ID)
	if len(ds) != 1 || ds[0].Kind != "reminder_waiting" || ds[0].Result != "accepted" || ds[0].Result == deliveryClaimed {
		t.Fatalf("deliveries = %+v", ds)
	}
}

func TestFailedReminderIsRetried(t *testing.T) {
	f := relay(t)
	f.fail = true
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	base := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	at := base
	svc.SetClock(func() time.Time { return at })
	c := svc.create(context.Background(), "s", "cordon_node", "patch", "", map[string]any{"name": "node-a"})
	if c.Status != http.StatusCreated {
		t.Fatalf("create = %d %v", c.Status, c.Body)
	}
	at = base.Add(4 * time.Hour)
	svc.remindDue(context.Background())
	if len(f.got()) != 0 {
		t.Fatalf("failed relay recorded a push: %+v", f.got())
	}
	if ds := deliveriesOf(t, svc, c.Approval.ID); len(ds) != 0 {
		t.Fatalf("failed send left a claim: %+v", ds)
	}
	f.fail = false
	svc.remindDue(context.Background())
	if n := len(f.got()); n != 1 {
		t.Fatalf("retry pushes = %d: %+v", n, f.got())
	}
	ds := deliveriesOf(t, svc, c.Approval.ID)
	if len(ds) != 1 || ds[0].Kind != "reminder_waiting" || ds[0].Result != "accepted" {
		t.Fatalf("deliveries = %+v", ds)
	}
}

func TestExpiredReminderClaimCanBeTakenAgain(t *testing.T) {
	f := relay(t)
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	base := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	at := base
	svc.SetClock(func() time.Time { return at })
	c := svc.create(context.Background(), "s", "cordon_node", "patch", "", map[string]any{"name": "node-a"})
	if c.Status != http.StatusCreated {
		t.Fatalf("create = %d %v", c.Status, c.Body)
	}
	at = base.Add(4 * time.Hour)
	if err := svc.store.update(func(doc *file) error {
		a, ok := doc.find(c.Approval.ID)
		if !ok {
			return errNoChange
		}
		a.Deliveries = []Delivery{{
			Kind: "reminder_waiting", Channel: "ntfy", Target: relayClaimTarget,
			Result: deliveryClaimed, ClaimID: "claim_old", At: at.Add(-time.Hour),
			ClaimExpiresAt: at.Add(-time.Minute),
		}}
		doc.put(a)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	svc.remindDue(context.Background())
	if n := len(f.got()); n != 1 {
		t.Fatalf("expired claim pushes = %d, want 1", n)
	}
	ds := deliveriesOf(t, svc, c.Approval.ID)
	if len(ds) != 1 || ds[0].Result != "accepted" || ds[0].ClaimID == "claim_old" {
		t.Fatalf("deliveries = %+v", ds)
	}
}

func TestReminderFinishAndReleaseCheckClaimID(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	base := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	at := base
	svc.SetClock(func() time.Time { return at })
	c := svc.create(context.Background(), "s", "cordon_node", "patch", "", map[string]any{"name": "node-a"})
	at = base.Add(4 * time.Hour)
	if err := svc.store.update(func(doc *file) error {
		a, _ := doc.find(c.Approval.ID)
		a.Deliveries = []Delivery{{
			Kind: "reminder_waiting", Channel: "ntfy", Target: relayClaimTarget,
			Result: deliveryClaimed, ClaimID: "claim_live", At: at,
			ClaimExpiresAt: at.Add(reminderClaimFor),
		}}
		doc.put(a)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.releaseReminder(c.Approval.ID, "reminder_waiting", "claim_other"); err != nil {
		t.Fatal(err)
	}
	if err := svc.finishReminder(c.Approval.ID, "reminder_waiting", "claim_other", []Delivery{{
		Kind: "reminder_waiting", Channel: "ntfy", Target: "ntfy:x", Result: "accepted", At: at,
	}}); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("finish with the wrong claim = %v", err)
	}
	got, _ := svc.find(c.Approval.ID)
	if len(got.Deliveries) != 1 || got.Deliveries[0].ClaimID != "claim_live" || got.Deliveries[0].Result != deliveryClaimed {
		t.Fatalf("wrong claim id changed the live claim: %+v", got.Deliveries)
	}
	if err := svc.finishReminder(c.Approval.ID, "reminder_waiting", "claim_live", []Delivery{{
		Kind: "reminder_waiting", Channel: "ntfy", Target: "ntfy:ok", Result: "accepted", At: at,
	}}); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.find(c.Approval.ID)
	if len(got.Deliveries) != 1 || got.Deliveries[0].ClaimID != "" || got.Deliveries[0].Target != "ntfy:ok" {
		t.Fatalf("finish = %+v", got.Deliveries)
	}
}

func TestDeliveryCapKeepsTheFinalResult(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	c := svc.create(context.Background(), "s", "cordon_node", "patch", "", map[string]any{"name": "node-a"})
	if c.Status != http.StatusCreated {
		t.Fatalf("create = %d %v", c.Status, c.Body)
	}
	batch := []Delivery{{
		Kind: "failed", Channel: "ntfy", Target: "ntfy:final", Result: "accepted", At: time.Unix(1, 0).UTC(),
	}}
	for i := 0; i < 25; i++ {
		batch = append(batch, Delivery{
			Kind: "reminder_waiting", Channel: "ntfy", Target: fmt.Sprintf("t%d", i),
			Result: "accepted", At: time.Unix(int64(i+2), 0).UTC(),
		})
	}
	if err := svc.RecordDeliveries(c.Approval.ID, batch); err != nil {
		t.Fatal(err)
	}
	got, ok := svc.find(c.Approval.ID)
	if !ok || len(got.Deliveries) != maxDeliveries {
		t.Fatalf("kept %d", len(got.Deliveries))
	}
	if got.Deliveries[0].Target != "ntfy:final" {
		t.Fatalf("oldest final result was dropped: %+v", got.Deliveries[0])
	}
	if got.Deliveries[len(got.Deliveries)-1].Target != "t24" {
		t.Fatalf("newest = %+v", got.Deliveries[len(got.Deliveries)-1])
	}
}
