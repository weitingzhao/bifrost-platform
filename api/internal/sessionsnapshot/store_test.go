package sessionsnapshot

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func newTestSnapshotStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("PLATFORM_SESSION_SNAPSHOT_PATH", filepath.Join(t.TempDir(), "latest.json"))
	return NewStore()
}

func TestStoreLatestMissingReturnsFalse(t *testing.T) {
	store := newTestSnapshotStore(t)
	if _, ok := store.Latest(); ok {
		t.Fatal("Latest() = true, want false before any Save")
	}
}

func TestStoreSaveAndLatestRoundTrip(t *testing.T) {
	store := newTestSnapshotStore(t)
	payload, err := json.Marshal(map[string]any{"foo": "bar"})
	if err != nil {
		t.Fatal(err)
	}
	env := Envelope{SavedAt: "2026-07-01T00:00:00Z", SavedBy: "tester", Payload: payload}
	if err := store.Save(env); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, ok := store.Latest()
	if !ok {
		t.Fatal("Latest() = false after Save")
	}
	if got.SavedAt != "2026-07-01T00:00:00Z" || got.SavedBy != "tester" {
		t.Fatalf("Latest() = %+v", got)
	}
	var decoded map[string]any
	if err := json.Unmarshal(got.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if decoded["foo"] != "bar" {
		t.Fatalf("decoded payload = %+v", decoded)
	}
}

func TestStoreSaveOverwritesPreviousSnapshot(t *testing.T) {
	store := newTestSnapshotStore(t)
	_ = store.Save(Envelope{SavedAt: "t1", SavedBy: "a", Payload: json.RawMessage(`{}`)})
	_ = store.Save(Envelope{SavedAt: "t2", SavedBy: "b", Payload: json.RawMessage(`{}`)})

	got, ok := store.Latest()
	if !ok || got.SavedAt != "t2" || got.SavedBy != "b" {
		t.Fatalf("Latest() = %+v, want the second save to win", got)
	}
}
