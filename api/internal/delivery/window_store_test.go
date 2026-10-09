package delivery

import (
	"context"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
)

func TestReleaseWindowOneHolderWins(t *testing.T) {
	cs := fake.NewSimpleClientset()
	now := time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	held := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		who := "holder-" + string(rune('a'+i))
		go func() {
			defer wg.Done()
			_, err := putReleaseWindow(context.Background(), cs, "cicd", windowInput{
				What: "bifrost-trade-core", Who: who, Reason: "hold", TTLMinutes: 5,
			}, now)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
				return
			}
			if _, ok := err.(*WindowHeld); ok {
				held++
			} else {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("wins=%d held=%d, want one winner", wins, held)
	}
	if held != 7 {
		t.Fatalf("held=%d, want the other seven refused", held)
	}
}

func TestReleaseWindowRenewAndForeignDelete(t *testing.T) {
	cs := fake.NewSimpleClientset()
	now := time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)
	first, err := putReleaseWindow(context.Background(), cs, "cicd", windowInput{
		What: "bifrost-trade-core", Who: "ada@host", Reason: "hold", TTLMinutes: 5,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := putReleaseWindow(context.Background(), cs, "cicd", windowInput{
		What: "bifrost-trade-core", Who: "ada@host", Reason: "hold", TTLMinutes: 5,
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if renewed.StartedAt != first.StartedAt {
		t.Fatalf("renewal reset started_at %s -> %s", first.StartedAt, renewed.StartedAt)
	}
	if renewed.ExpiresAt == first.ExpiresAt {
		t.Fatal("renewal did not move expires_at")
	}
	if err := deleteReleaseWindow(context.Background(), cs, "cicd", "bob@host", false, now.Add(time.Minute)); err == nil {
		t.Fatal("a non-holder deleted the window")
	}
	got, open, err := getReleaseWindow(context.Background(), cs, "cicd", now.Add(time.Minute))
	if err != nil || !open || got.Who != "ada@host" {
		t.Fatalf("window after refused delete: open=%v who=%s err=%v", open, got.Who, err)
	}
	if err := deleteReleaseWindow(context.Background(), cs, "cicd", "ada@host", false, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, open, err := getReleaseWindow(context.Background(), cs, "cicd", now.Add(time.Minute)); err != nil || open {
		t.Fatalf("window still open after holder delete: open=%v err=%v", open, err)
	}
}

func TestExpiredWindowIsEmptyAndReplaceable(t *testing.T) {
	cs := fake.NewSimpleClientset()
	start := time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC)
	if _, err := putReleaseWindow(context.Background(), cs, "cicd", windowInput{
		What: "bifrost-trade-core", Who: "ada@host", Reason: "stg", TTLMinutes: 5,
	}, start); err != nil {
		t.Fatal(err)
	}
	later := start.Add(6 * time.Minute)
	if _, open, err := getReleaseWindow(context.Background(), cs, "cicd", later); err != nil || open {
		t.Fatalf("expired window still open: open=%v err=%v", open, err)
	}
	if _, err := putReleaseWindow(context.Background(), cs, "cicd", windowInput{
		What: "bifrost-research", Who: "bob@host", Reason: "hold", TTLMinutes: 5,
	}, later); err != nil {
		t.Fatal(err)
	}
	got, open, err := getReleaseWindow(context.Background(), cs, "cicd", later)
	if err != nil || !open || got.Who != "bob@host" {
		t.Fatalf("replacement: open=%v who=%s err=%v", open, got.Who, err)
	}
	if err := deleteReleaseWindow(context.Background(), cs, "cicd", "ada@host", true, later); err != nil {
		t.Fatal(err)
	}
	if _, open, err := getReleaseWindow(context.Background(), cs, "cicd", later); err != nil || open {
		t.Fatalf("force delete left the window open: open=%v err=%v", open, err)
	}
}
