package briefing

import (
	"path/filepath"
	"testing"
)

func newTestSessionResultStore(t *testing.T) *SessionResultStore {
	t.Helper()
	t.Setenv("PLATFORM_BRIEFING_SESSION_RESULTS_PATH", filepath.Join(t.TempDir(), "results.json"))
	return NewSessionResultStore()
}

func TestSessionResultStoreAppendAndList(t *testing.T) {
	store := newTestSessionResultStore(t)

	if list := store.List(50); len(list) != 0 {
		t.Fatalf("List() on empty store = %+v, want empty", list)
	}

	if err := store.Append(SessionResult{ID: "r1", Outcome: "success", Summary: "first"}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := store.Append(SessionResult{ID: "r2", Outcome: "success", Summary: "second"}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	list := store.List(50)
	if len(list) != 2 {
		t.Fatalf("List() len = %d, want 2", len(list))
	}
	// Most recent append is prepended to the front.
	if list[0].ID != "r2" || list[1].ID != "r1" {
		t.Fatalf("List() order = [%s %s], want [r2 r1]", list[0].ID, list[1].ID)
	}
}

func TestSessionResultStoreListRespectsLimit(t *testing.T) {
	store := newTestSessionResultStore(t)
	for i := 0; i < 5; i++ {
		if err := store.Append(SessionResult{ID: string(rune('a' + i)), Outcome: "success"}); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	list := store.List(2)
	if len(list) != 2 {
		t.Fatalf("List(2) len = %d, want 2", len(list))
	}
}

func TestSessionResultStoreCapsAt100Entries(t *testing.T) {
	store := newTestSessionResultStore(t)
	for i := 0; i < 105; i++ {
		if err := store.Append(SessionResult{ID: "r", Outcome: "success"}); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	list := store.List(0)
	if len(list) != 100 {
		t.Fatalf("List(0) len = %d, want capped at 100", len(list))
	}
}

func TestNewSessionResultID(t *testing.T) {
	a := newSessionResultID()
	b := newSessionResultID()
	if a == "" || b == "" {
		t.Fatal("newSessionResultID() returned empty string")
	}
}
