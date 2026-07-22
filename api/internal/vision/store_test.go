package vision

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreGateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	rec, err := store.LoadGate()
	if err != nil {
		t.Fatalf("LoadGate (missing file): %v", err)
	}
	if rec != nil {
		t.Fatalf("LoadGate (missing file) = %+v, want nil", rec)
	}

	want := GateRecord{
		At:          time.Now().UTC().Truncate(time.Second),
		Result:      "pass",
		TriggeredBy: "tester",
		Summary:     "all checks green",
		Checks: []GateCheck{
			{ID: "c1", Label: "Check 1", Required: true, Reachability: "ok"},
		},
	}
	if saveErr := store.SaveGate(want); saveErr != nil {
		t.Fatalf("SaveGate: %v", saveErr)
	}

	got, err := store.LoadGate()
	if err != nil {
		t.Fatalf("LoadGate: %v", err)
	}
	if got == nil {
		t.Fatal("LoadGate returned nil after SaveGate")
	}
	if got.Result != want.Result || got.TriggeredBy != want.TriggeredBy || got.Summary != want.Summary {
		t.Fatalf("LoadGate = %+v, want %+v", got, want)
	}
	if !got.At.Equal(want.At) {
		t.Fatalf("LoadGate.At = %v, want %v", got.At, want.At)
	}
	if len(got.Checks) != 1 || got.Checks[0].ID != "c1" {
		t.Fatalf("LoadGate.Checks = %+v", got.Checks)
	}
}

func TestStoreSignoffRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	rec, err := store.LoadSignoff()
	if err != nil {
		t.Fatalf("LoadSignoff (missing file): %v", err)
	}
	if rec != nil {
		t.Fatalf("LoadSignoff (missing file) = %+v, want nil", rec)
	}

	want := V1SignoffRecord{
		At: time.Now().UTC().Truncate(time.Second), SignedBy: "owner",
		Notes: "looks good", GateAt: time.Now().UTC().Truncate(time.Second), Result: "SIGNED",
	}
	if signoffErr := store.SaveSignoff(want); signoffErr != nil {
		t.Fatalf("SaveSignoff: %v", signoffErr)
	}

	got, err := store.LoadSignoff()
	if err != nil {
		t.Fatalf("LoadSignoff: %v", err)
	}
	if got == nil || got.SignedBy != "owner" || got.Result != "SIGNED" || got.Notes != "looks good" {
		t.Fatalf("LoadSignoff = %+v", got)
	}
}

func TestStorePathsDeriveFromConfigDir(t *testing.T) {
	store := NewStore("/some/config/dir")
	if store.gatePath != filepath.Join("/some/config/dir", "vision_v1_gate.json") {
		t.Fatalf("gatePath = %q", store.gatePath)
	}
	if store.signoffPath != filepath.Join("/some/config/dir", "vision_v1_gate_signoff.json") {
		t.Fatalf("signoffPath = %q", store.signoffPath)
	}
}

func TestStorePathsRespectEnvOverride(t *testing.T) {
	t.Setenv("PLATFORM_VISION_V1_STATE", "/custom/path/gate.json")
	store := NewStore("/ignored")
	if store.gatePath != "/custom/path/gate.json" {
		t.Fatalf("gatePath = %q, want env override", store.gatePath)
	}
	if store.signoffPath != filepath.Join("/custom/path", "gate_signoff.json") {
		t.Fatalf("signoffPath = %q", store.signoffPath)
	}
}

// gatePersister is the common Load/Save gate surface shared by V1/S3/V2–V5 stores.
type gatePersister interface {
	LoadGate() (*GateRecord, error)
	SaveGate(GateRecord) error
}

func TestCrossMilestoneStoreGateRoundTrip(t *testing.T) {
	// One shared path across milestones — not a full V2–V5 service parameterization.
	cases := []struct {
		name string
		new  func(dir string) gatePersister
	}{
		{name: "V1", new: func(dir string) gatePersister { return NewStore(dir) }},
		{name: "S3", new: func(dir string) gatePersister { return NewS3Store(dir) }},
		{name: "V2", new: func(dir string) gatePersister { return NewV2Store(dir) }},
		{name: "V3", new: func(dir string) gatePersister { return NewV3Store(dir) }},
		{name: "V4", new: func(dir string) gatePersister { return NewV4Store(dir) }},
		{name: "V5", new: func(dir string) gatePersister { return NewV5Store(dir) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Clear milestone env overrides so each store derives paths from configDir.
			t.Setenv("PLATFORM_VISION_V1_STATE", "")
			t.Setenv("PLATFORM_VISION_S3_STATE", "")
			t.Setenv("PLATFORM_VISION_V2_STATE", "")
			t.Setenv("PLATFORM_VISION_V3_STATE", "")
			t.Setenv("PLATFORM_VISION_V4_STATE", "")
			t.Setenv("PLATFORM_VISION_V5_STATE", "")

			dir := t.TempDir()
			store := tc.new(dir)

			missing, err := store.LoadGate()
			if err != nil {
				t.Fatalf("LoadGate (missing): %v", err)
			}
			if missing != nil {
				t.Fatalf("LoadGate (missing) = %+v, want nil", missing)
			}

			want := GateRecord{
				At:          time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC),
				Result:      "pass",
				TriggeredBy: "cross-v-test",
				Summary:     tc.name + " gate ok",
				Checks: []GateCheck{
					{ID: "shared", Label: "Shared check", Required: true, Reachability: "ok"},
				},
			}
			if saveErr := store.SaveGate(want); saveErr != nil {
				t.Fatalf("SaveGate: %v", saveErr)
			}
			got, loadErr := store.LoadGate()
			if loadErr != nil || got == nil {
				t.Fatalf("LoadGate after save: got=%v err=%v", got, loadErr)
			}
			if got.Result != want.Result || got.TriggeredBy != want.TriggeredBy || got.Summary != want.Summary {
				t.Fatalf("LoadGate = %+v, want %+v", got, want)
			}
			if len(got.Checks) != 1 || got.Checks[0].ID != "shared" {
				t.Fatalf("Checks = %+v", got.Checks)
			}
		})
	}
}
