package promote

import (
	"path/filepath"
	"testing"
)

func TestCycleStoreRecordDeployAndGate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PLATFORM_RELEASE_CYCLES_DIR", dir)
	store := NewCycleStore(filepath.Join(dir, "config"))

	rec, err := store.RecordDeploy(DeployRecordOpts{
		Lane:        ReleaseCycleLaneTrade,
		Step:        CycleStepStgDeploy,
		Revision:    "abc1234",
		RunName:     "bifrost-deliver-stg-1",
		TriggeredBy: "owner",
	})
	if err != nil {
		t.Fatalf("RecordDeploy: %v", err)
	}
	if rec.ID == "" || rec.Outcome != CycleOutcomeInProgress {
		t.Fatalf("unexpected cycle: %+v", rec)
	}
	if got := stepByKind(rec, CycleStepStgDeploy); got == nil || got.Result != CycleStepResultRunning {
		t.Fatalf("stg_deploy step: %+v", got)
	}

	rec, err = store.RecordGate(GateRecordOpts{
		Lane:     ReleaseCycleLaneTrade,
		Step:     CycleStepStgGate,
		Revision: "abc1234",
		Result:   "pass",
		Summary:  "stg pass",
		Checks:   []GateCheck{{ID: "smoke", Label: "smoke", Required: true}},
	})
	if err != nil {
		t.Fatalf("RecordGate stg: %v", err)
	}
	if got := stepByKind(rec, CycleStepStgDeploy); got == nil || got.Result != CycleStepResultSuccess {
		t.Fatalf("deploy should be success after gate: %+v", got)
	}
	if got := stepByKind(rec, CycleStepStgGate); got == nil || got.Result != CycleStepResultPass {
		t.Fatalf("stg_gate: %+v", got)
	}

	_, err = store.RecordDeploy(DeployRecordOpts{
		Lane:     ReleaseCycleLaneTrade,
		Step:     CycleStepProdDeploy,
		Revision: "abc1234",
		RunName:  "bifrost-deliver-prod-1",
	})
	if err != nil {
		t.Fatalf("RecordDeploy prod: %v", err)
	}

	rec, err = store.RecordGate(GateRecordOpts{
		Lane:     ReleaseCycleLaneTrade,
		Step:     CycleStepProdGate,
		Revision: "abc1234",
		Result:   "pass",
		Summary:  "prod pass",
	})
	if err != nil {
		t.Fatalf("RecordGate prod: %v", err)
	}
	if rec.Outcome != CycleOutcomeReleased {
		t.Fatalf("expected released, got %s", rec.Outcome)
	}
	if rec.CompletedAt == nil {
		t.Fatal("expected completed_at")
	}

	list := loadCycles(t, store, ReleaseCycleLaneTrade)
	if len(list) != 1 || list[0].ID != rec.ID {
		t.Fatalf("list: %+v", list)
	}
}

func TestCycleStoreSupersedeOnNewRevision(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PLATFORM_RELEASE_CYCLES_DIR", dir)
	store := NewCycleStore(filepath.Join(dir, "config"))

	first, err := store.RecordDeploy(DeployRecordOpts{
		Lane:     ReleaseCycleLanePlatform,
		Step:     CycleStepStgDeploy,
		Revision: "rev-a",
		RunName:  "run-a",
	})
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	second, err := store.RecordDeploy(DeployRecordOpts{
		Lane:     ReleaseCycleLanePlatform,
		Step:     CycleStepStgDeploy,
		Revision: "rev-b",
		RunName:  "run-b",
	})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("expected new cycle id")
	}
	if second.Revision != "rev-b" {
		t.Fatalf("revision: %s", second.Revision)
	}

	old := cycleByID(t, store, ReleaseCycleLanePlatform, first.ID)
	if old == nil {
		t.Fatal("old cycle missing")
	}
	if old.Outcome != CycleOutcomeSuperseded {
		t.Fatalf("expected superseded, got %s", old.Outcome)
	}
}

func TestCycleStoreGateFailKeepsOpen(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PLATFORM_RELEASE_CYCLES_DIR", dir)
	store := NewCycleStore(filepath.Join(dir, "config"))

	_, err := store.RecordDeploy(DeployRecordOpts{
		Lane:     ReleaseCycleLaneTrade,
		Step:     CycleStepStgDeploy,
		Revision: "r1",
		RunName:  "run-1",
	})
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	rec, err := store.RecordGate(GateRecordOpts{
		Lane:   ReleaseCycleLaneTrade,
		Step:   CycleStepStgGate,
		Result: "fail",
	})
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	if rec.Outcome != CycleOutcomeInProgress {
		t.Fatalf("fail should keep in_progress, got %s", rec.Outcome)
	}
	if got := stepByKind(rec, CycleStepStgGate); got == nil || got.Result != CycleStepResultFail {
		t.Fatalf("stg_gate: %+v", got)
	}
}

func TestCycleStoreReopenFailedOnRetry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PLATFORM_RELEASE_CYCLES_DIR", dir)
	store := NewCycleStore(filepath.Join(dir, "config"))

	first, err := store.RecordDeploy(DeployRecordOpts{
		Lane:     ReleaseCycleLaneTrade,
		Step:     CycleStepStgDeploy,
		Revision: "r-retry",
		RunName:  "run-1",
	})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	// A deploy failure recorded before W-32 left the cycle failed on disk.
	entries := loadCycles(t, store, ReleaseCycleLaneTrade)
	entries[0].Outcome = CycleOutcomeFailed
	if err := store.saveLocked(ReleaseCycleLaneTrade, entries); err != nil {
		t.Fatalf("seed failed cycle: %v", err)
	}

	second, err := store.RecordDeploy(DeployRecordOpts{
		Lane:     ReleaseCycleLaneTrade,
		Step:     CycleStepStgDeploy,
		Revision: "r-retry",
		RunName:  "run-2",
	})
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected reopen same cycle, got %s vs %s", second.ID, first.ID)
	}
	if second.Outcome != CycleOutcomeInProgress {
		t.Fatalf("expected in_progress after reopen, got %s", second.Outcome)
	}
	if step := stepByKind(second, CycleStepStgDeploy); step == nil || step.RunName != "run-2" || step.Result != CycleStepResultRunning {
		t.Fatalf("retry step: %+v", step)
	}
}

// loadCycles reads a lane's persisted cycles, oldest first.
func loadCycles(t *testing.T, store *CycleStore, lane ReleaseCycleLane) []ReleaseCycleRecord {
	t.Helper()
	store.mu.RLock()
	defer store.mu.RUnlock()
	entries, err := store.loadLocked(lane)
	if err != nil {
		t.Fatalf("load %s cycles: %v", lane, err)
	}
	return entries
}

func cycleByID(t *testing.T, store *CycleStore, lane ReleaseCycleLane, id string) *ReleaseCycleRecord {
	t.Helper()
	for _, rec := range loadCycles(t, store, lane) {
		if rec.ID == id {
			return &rec
		}
	}
	return nil
}

func stepByKind(rec *ReleaseCycleRecord, kind CycleStepKind) *CycleStepRecord {
	if rec == nil {
		return nil
	}
	for i := range rec.Steps {
		if rec.Steps[i].Kind == kind {
			return &rec.Steps[i]
		}
	}
	return nil
}
