package driftproposal

import (
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("PLATFORM_DRIFT_PROPOSALS_DIR", t.TempDir())
	return NewStore()
}

func TestStorePutGetRoundTrip(t *testing.T) {
	store := newTestStore(t)

	if _, ok := store.Get("missing"); ok {
		t.Fatal("Get(missing) = true, want false")
	}

	now := time.Now().UTC().Truncate(time.Second)
	p := Proposal{
		ID: "drift-1", Status: StatusPendingApproval, Host: "mac-mini-1",
		LayersFailed: []string{"catalog", "docs"}, FindingsCount: 3,
		Summary: "catalog drift detected", CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Put(p); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	got, ok := store.Get("drift-1")
	if !ok {
		t.Fatal("Get(drift-1) = false, want true")
	}
	if got.Status != StatusPendingApproval || got.Summary != "catalog drift detected" || len(got.LayersFailed) != 2 {
		t.Fatalf("Get(drift-1) = %+v", got)
	}
}

func TestStorePutOverwritesExistingProposal(t *testing.T) {
	store := newTestStore(t)
	store.mustPut(t, Proposal{ID: "drift-1", Status: StatusPendingApproval, Summary: "v1"})
	store.mustPut(t, Proposal{ID: "drift-1", Status: StatusApproved, Summary: "v2"})

	got, ok := store.Get("drift-1")
	if !ok || got.Status != StatusApproved || got.Summary != "v2" {
		t.Fatalf("Get(drift-1) = %+v, want latest overwrite", got)
	}
}

func TestStoreListOrdersByCreatedAtDesc(t *testing.T) {
	store := newTestStore(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store.mustPut(t, Proposal{ID: "old", CreatedAt: base})
	store.mustPut(t, Proposal{ID: "new", CreatedAt: base.Add(time.Hour)})
	store.mustPut(t, Proposal{ID: "newest", CreatedAt: base.Add(2 * time.Hour)})

	list := store.List()
	if len(list) != 3 {
		t.Fatalf("List() len = %d, want 3", len(list))
	}
	if list[0].ID != "newest" || list[1].ID != "new" || list[2].ID != "old" {
		t.Fatalf("List() order = [%s %s %s], want [newest new old]", list[0].ID, list[1].ID, list[2].ID)
	}
}

func TestStoreListEmptyDirReturnsEmpty(t *testing.T) {
	store := newTestStore(t)
	if list := store.List(); len(list) != 0 {
		t.Fatalf("List() = %+v, want empty", list)
	}
}

func TestNewProposalIDIsNonEmptyAndUnique(t *testing.T) {
	a := newProposalID()
	time.Sleep(time.Millisecond)
	b := newProposalID()
	if a == "" || b == "" || a == b {
		t.Fatalf("newProposalID() a=%q b=%q, want distinct non-empty ids", a, b)
	}
}

// mustPut is a small helper to keep table-style test setup terse.
func (s *Store) mustPut(t *testing.T, p Proposal) {
	t.Helper()
	if err := s.Put(p); err != nil {
		t.Fatalf("Put(%q) error = %v", p.ID, err)
	}
}
