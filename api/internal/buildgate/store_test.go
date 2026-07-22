package buildgate

import (
	"testing"
	"time"
)

func TestStoreGateRoundTrip(t *testing.T) {
	store := NewStore(t.TempDir())

	rec, err := store.LoadGate("P1")
	if err != nil {
		t.Fatalf("LoadGate (missing) error = %v", err)
	}
	if rec != nil {
		t.Fatalf("LoadGate (missing) = %+v, want nil", rec)
	}

	want := GateRecord{
		At:          time.Now().UTC().Truncate(time.Second),
		Phase:       "P1",
		Result:      "pass",
		TriggeredBy: "tester",
		Checks:      []GateCheck{{ID: "p1-1", Label: "Task 1", Status: "pass", Required: true}},
	}
	if saveErr := store.SaveGate(want); saveErr != nil {
		t.Fatalf("SaveGate() error = %v", saveErr)
	}

	got, err := store.LoadGate("P1")
	if err != nil {
		t.Fatalf("LoadGate() error = %v", err)
	}
	if got == nil {
		t.Fatal("LoadGate() = nil after SaveGate")
	}
	if got.Phase != "P1" || got.Result != "pass" || got.TriggeredBy != "tester" {
		t.Fatalf("LoadGate() = %+v", got)
	}
	if len(got.Checks) != 1 || got.Checks[0].ID != "p1-1" {
		t.Fatalf("LoadGate().Checks = %+v", got.Checks)
	}

	// A different phase must not see P1's gate record.
	other, err := store.LoadGate("P2")
	if err != nil {
		t.Fatalf("LoadGate(P2) error = %v", err)
	}
	if other != nil {
		t.Fatalf("LoadGate(P2) = %+v, want nil (isolated per phase)", other)
	}
}

func TestStoreSignoffRoundTrip(t *testing.T) {
	store := NewStore(t.TempDir())

	rec, err := store.LoadSignoff("P2")
	if err != nil {
		t.Fatalf("LoadSignoff (missing) error = %v", err)
	}
	if rec != nil {
		t.Fatalf("LoadSignoff (missing) = %+v, want nil", rec)
	}

	want := SignoffRecord{
		At: time.Now().UTC().Truncate(time.Second), Phase: "P2",
		SignedBy: "owner", Notes: "looks good", Result: "SIGNED",
	}
	if signoffErr := store.SaveSignoff(want); signoffErr != nil {
		t.Fatalf("SaveSignoff() error = %v", signoffErr)
	}

	got, err := store.LoadSignoff("P2")
	if err != nil {
		t.Fatalf("LoadSignoff() error = %v", err)
	}
	if got == nil || got.SignedBy != "owner" || got.Result != "SIGNED" || got.Notes != "looks good" {
		t.Fatalf("LoadSignoff() = %+v", got)
	}
}

func TestStoreGateOverwritesPreviousRecordForSamePhase(t *testing.T) {
	store := NewStore(t.TempDir())

	if err := store.SaveGate(GateRecord{At: time.Now().UTC(), Phase: "P3", Result: "incomplete"}); err != nil {
		t.Fatalf("SaveGate() error = %v", err)
	}
	if err := store.SaveGate(GateRecord{At: time.Now().UTC(), Phase: "P3", Result: "pass"}); err != nil {
		t.Fatalf("SaveGate() error = %v", err)
	}

	got, err := store.LoadGate("P3")
	if err != nil {
		t.Fatalf("LoadGate() error = %v", err)
	}
	if got == nil || got.Result != "pass" {
		t.Fatalf("LoadGate() = %+v, want latest overwrite", got)
	}
}
