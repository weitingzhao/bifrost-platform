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

	list, err := store.List(ReleaseCycleLaneTrade)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != rec.ID {
		t.Fatalf("list: %+v", list)
	}

	got, err := store.Get(rec.ID)
	if err != nil || got == nil || got.ID != rec.ID {
		t.Fatalf("Get: %+v %v", got, err)
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

	old, err := store.Get(first.ID)
	if err != nil || old == nil {
		t.Fatalf("get old: %v", err)
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

func TestCycleStoreSyncRunStatusFailed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PLATFORM_RELEASE_CYCLES_DIR", dir)
	store := NewCycleStore(filepath.Join(dir, "config"))

	rec, err := store.RecordDeploy(DeployRecordOpts{
		Lane:     ReleaseCycleLaneTrade,
		Step:     CycleStepStgDeploy,
		Revision: "r-fail",
		RunName:  "bifrost-deliver-stg-99",
	})
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if err := store.SyncRunStatus(ReleaseCycleLaneTrade, []RunStatusInfo{
		{RunName: "bifrost-deliver-stg-99", Status: "False", Reason: "Failed"},
	}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	got, err := store.Get(rec.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}
	if got.Outcome != CycleOutcomeFailed {
		t.Fatalf("expected failed outcome, got %s", got.Outcome)
	}
	if step := stepByKind(got, CycleStepStgDeploy); step == nil || step.Result != CycleStepResultFailed {
		t.Fatalf("stg_deploy: %+v", step)
	}
}

func TestCycleStoreSyncRunStatusSucceeded(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PLATFORM_RELEASE_CYCLES_DIR", dir)
	store := NewCycleStore(filepath.Join(dir, "config"))

	_, err := store.RecordDeploy(DeployRecordOpts{
		Lane:     ReleaseCycleLanePlatform,
		Step:     CycleStepStgDeploy,
		Revision: "r-ok",
		RunName:  "bifrost-deliver-platform-1",
	})
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if err := store.SyncRunStatus(ReleaseCycleLanePlatform, []RunStatusInfo{
		{RunName: "bifrost-deliver-platform-1", Status: "True", Reason: "Succeeded"},
	}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	active, err := store.ActiveCycle(ReleaseCycleLanePlatform)
	if err != nil || active == nil {
		t.Fatalf("active: %v", err)
	}
	if active.Outcome != CycleOutcomeInProgress {
		t.Fatalf("success should keep cycle in_progress, got %s", active.Outcome)
	}
	if step := stepByKind(active, CycleStepStgDeploy); step == nil || step.Result != CycleStepResultSuccess {
		t.Fatalf("stg_deploy: %+v", step)
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
	_ = store.SyncRunStatus(ReleaseCycleLaneTrade, []RunStatusInfo{
		{RunName: "run-1", Status: "False", Reason: "Failed"},
	})

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
