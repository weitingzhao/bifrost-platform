package cluster

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateDataCloneRequest(t *testing.T) {
	ok := DataCloneRequest{
		Source:            "bifrost_prod",
		Targets:           []string{"bifrost_dev", "bifrost_stg"},
		Mode:              "full",
		ConfirmationToken: dataCloneConfirmTok,
		Confirm:           true,
	}
	if err := validateDataCloneRequest(ok); err != nil {
		t.Fatalf("expected ok: %v", err)
	}

	noConfirm := ok
	noConfirm.Confirm = false
	if err := validateDataCloneRequest(noConfirm); err == nil {
		t.Fatal("expected confirm=true required")
	}

	badTok := ok
	badTok.ConfirmationToken = "YES"
	if err := validateDataCloneRequest(badTok); err == nil {
		t.Fatal("expected confirmation error")
	}

	badTarget := ok
	badTarget.Targets = []string{"bifrost_prod"}
	if err := validateDataCloneRequest(badTarget); err == nil {
		t.Fatal("expected prod target rejection")
	}

	sel := ok
	sel.Mode = "selective"
	sel.Tables = nil
	if err := validateDataCloneRequest(sel); err == nil {
		t.Fatal("expected selective tables required")
	}

	sel.Tables = []string{"strategy_instance"}
	if err := validateDataCloneRequest(sel); err != nil {
		t.Fatalf("selective ok: %v", err)
	}

	sel.Tables = []string{"daemon;drop"}
	if err := validateDataCloneRequest(sel); err == nil {
		t.Fatal("expected invalid table name")
	}
}

func TestDataCloneJobStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PLATFORM_DATA_CLONE_JOBS_DIR", dir)
	store := NewDataCloneJobStore()
	job := store.Create(DataCloneJob{
		Status:  "queued",
		Source:  "bifrost_prod",
		Targets: []string{"bifrost_dev"},
		Mode:    "full",
		Trigger: "manual",
	})
	if job.ID == "" {
		t.Fatal("empty id")
	}
	got, ok := store.Get(job.ID)
	if !ok || got.Status != "queued" {
		t.Fatalf("get: %+v ok=%v", got, ok)
	}
	updated, ok := store.Update(job.ID, func(j *DataCloneJob) {
		j.Status = "dumping"
		j.Step = "dumping"
		j.Progress = 0.2
	})
	if !ok || updated.Status != "dumping" {
		t.Fatalf("update: %+v", updated)
	}
	// Reload from disk
	store2 := NewDataCloneJobStore()
	got2, ok := store2.Get(job.ID)
	if !ok || got2.Status != "dumping" {
		t.Fatalf("persist reload: %+v ok=%v", got2, ok)
	}
}

func TestDataCloneScheduleDefaultDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sched.json")
	t.Setenv("PLATFORM_DATA_CLONE_SCHEDULE", path)
	store := NewDataCloneScheduleStore()
	cfg := store.Get()
	if cfg.Enabled {
		t.Fatal("default must be disabled")
	}
	cfg.Enabled = true
	cfg.Interval = "weekly"
	out := store.Put(cfg)
	if !out.Enabled || out.Interval != "weekly" {
		t.Fatalf("put: %+v", out)
	}
	store2 := NewDataCloneScheduleStore()
	if !store2.Get().Enabled {
		t.Fatal("schedule not persisted")
	}
}

func TestRunDataCloneFullSuccess(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PLATFORM_DATA_CLONE_JOBS_DIR", dir)
	t.Setenv("PLATFORM_DATA_CLONE_LAST", filepath.Join(t.TempDir(), "last.json"))
	svc := NewService(nil)
	svc.primaryOverride = "bifrost-postgres-1"
	svc.SetPodExecForTest(func(ctx context.Context, kubeconfig, namespace, pod, container string, command ...string) (string, error) {
		joined := strings.Join(command, " ")
		switch {
		case strings.Contains(joined, "pg_dump"):
			return "", nil
		case strings.Contains(joined, "wc -c"):
			return "5000000", nil
		case strings.Contains(joined, "information_schema.tables"):
			return "42", nil
		case strings.Contains(joined, "strategy_instance"):
			return "3", nil
		default:
			return "", nil
		}
	})
	store := NewDataCloneJobStore()
	svc.cloneJobs = store
	svc.cloneLast = NewDataCloneLastStore()
	job := store.Create(DataCloneJob{
		Status:  "queued",
		Source:  "bifrost_prod",
		Targets: []string{"bifrost_dev"},
		Mode:    "full",
		Trigger: "manual",
	})
	svc.runDataClone(context.Background(), job.ID)
	got, ok := store.Get(job.ID)
	if !ok {
		t.Fatal("job missing")
	}
	if got.Status != "done" {
		t.Fatalf("status=%s detail=%s", got.Status, got.Detail)
	}
	if len(got.Verify) != 1 || !got.Verify[0].OK {
		t.Fatalf("verify: %+v", got.Verify)
	}
}

func TestRunDataCloneSelectiveDumpArgs(t *testing.T) {
	var seen []string
	svc := NewService(nil)
	svc.SetPodExecForTest(func(ctx context.Context, kubeconfig, namespace, pod, container string, command ...string) (string, error) {
		seen = append(seen, strings.Join(command, " "))
		joined := strings.Join(command, " ")
		if strings.Contains(joined, "wc -c") {
			return "100", nil
		}
		if strings.Contains(joined, "information_schema") {
			return "10", nil
		}
		if strings.Contains(joined, "strategy_instance") {
			return "1", nil
		}
		return "", nil
	})
	if err := svc.restoreTarget(context.Background(), "pod", "bifrost_dev", "selective", []string{"strategy_instance"}); err != nil {
		t.Fatal(err)
	}
	foundTruncate := false
	for _, s := range seen {
		if strings.Contains(s, "TRUNCATE TABLE strategy_instance") {
			foundTruncate = true
		}
	}
	if !foundTruncate {
		t.Fatalf("expected truncate in selective restore, seen=%v", seen)
	}
}

func TestDataCloneDumpArgsSelectiveIsDataOnly(t *testing.T) {
	args := dataCloneDumpArgs("bifrost_prod", "selective", []string{"strategy_instance", "account_positions"})
	joined := strings.Join(args, " ")
	for _, want := range []string{"--data-only", "-t strategy_instance", "-t account_positions", "--no-owner", "--no-acl"} {
		if !strings.Contains(joined, want) {
			t.Errorf("selective dump args %q missing %q", joined, want)
		}
	}
	if strings.Contains(joined, "--exclude-table-data=") {
		t.Errorf("selective dump must not exclude audit table data: %q", joined)
	}
}

func TestDataCloneDumpArgsFullExcludesAuditData(t *testing.T) {
	args := dataCloneDumpArgs("bifrost_prod", "full", nil)
	joined := strings.Join(args, " ")
	want := "--exclude-table-data=" + dataCloneAuditTable
	if !strings.Contains(joined, want) {
		t.Fatalf("full dump args %q missing %q", joined, want)
	}
	if strings.Contains(joined, "--data-only") {
		t.Fatalf("full dump must not be data-only: %q", joined)
	}
}

func TestRestoreTargetFullDropsAllUserSchemas(t *testing.T) {
	var seen []string
	svc := NewService(nil)
	svc.SetPodExecForTest(func(ctx context.Context, kubeconfig, namespace, pod, container string, command ...string) (string, error) {
		seen = append(seen, strings.Join(command, " "))
		return "", nil
	})
	if err := svc.restoreTarget(context.Background(), "pod", "bifrost_dev", "full", nil); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(seen, "\n")
	if !strings.Contains(joined, "DROP SCHEMA IF EXISTS") {
		t.Fatalf("expected multi-schema drop in full reset, seen=%v", seen)
	}
	if !strings.Contains(joined, dataCloneRemoteDump) {
		t.Fatalf("expected dump restore path, seen=%v", seen)
	}
	auditBackup := dataCloneAuditBackupPath("bifrost_dev")
	if !strings.Contains(joined, auditBackup) {
		t.Fatalf("expected audit backup path %q in full restore, seen=%v", auditBackup, seen)
	}
	if !strings.Contains(joined, "pg_dump") || !strings.Contains(joined, "-t "+dataCloneAuditTable) {
		t.Fatalf("expected audit backup pg_dump before reset, seen=%v", seen)
	}
	if strings.Count(joined, "DROP SCHEMA public CASCADE; CREATE SCHEMA public") > 0 {
		t.Fatalf("legacy public-only reset must not be used, seen=%v", seen)
	}
}

func TestRestoreTargetSelectiveFailsWhenTruncateFails(t *testing.T) {
	svc := NewService(nil)
	svc.SetPodExecForTest(func(ctx context.Context, kubeconfig, namespace, pod, container string, command ...string) (string, error) {
		if strings.Contains(strings.Join(command, " "), "TRUNCATE TABLE strategy_instance") {
			return "", fmt.Errorf("relation does not exist")
		}
		return "", nil
	})
	err := svc.restoreTarget(context.Background(), "pod", "bifrost_dev", "selective", []string{"strategy_instance"})
	if err == nil || !strings.Contains(err.Error(), "truncate selective table") {
		t.Fatalf("expected truncate error, got %v", err)
	}
}

func TestStartDataCloneRejectsWhenRunning(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PLATFORM_DATA_CLONE_JOBS_DIR", dir)
	t.Setenv("PLATFORM_DATA_CLONE_LAST", filepath.Join(t.TempDir(), "last.json"))
	svc := NewService(nil)
	svc.cloneJobs = NewDataCloneJobStore()
	svc.cloneLast = NewDataCloneLastStore()
	// Seed an in-flight job without starting the executor.
	existing := svc.cloneJobs.Create(DataCloneJob{
		Status:  "dumping",
		Step:    "dumping",
		Source:  "bifrost_prod",
		Targets: []string{"bifrost_dev"},
		Mode:    "full",
		Trigger: "manual",
	})

	_, err := svc.startDataClone(context.Background(), DataCloneRequest{
		Source:            "bifrost_prod",
		Targets:           []string{"bifrost_stg"},
		Mode:              "full",
		ConfirmationToken: dataCloneConfirmTok,
		Confirm:           true,
	}, "manual", "tester")
	busy, ok := err.(*ErrCloneInProgress)
	if !ok {
		t.Fatalf("expected ErrCloneInProgress, got %v", err)
	}
	if busy.ExistingJobID != existing.ID {
		t.Fatalf("existing_job_id=%s want %s", busy.ExistingJobID, existing.ID)
	}
	if busy.Status != "dumping" {
		t.Fatalf("status=%s want dumping", busy.Status)
	}
}

func TestRunDataCloneRecordsLastCloneAt(t *testing.T) {
	jobsDir := t.TempDir()
	lastPath := filepath.Join(t.TempDir(), "last.json")
	t.Setenv("PLATFORM_DATA_CLONE_JOBS_DIR", jobsDir)
	t.Setenv("PLATFORM_DATA_CLONE_LAST", lastPath)
	svc := NewService(nil)
	svc.primaryOverride = "bifrost-postgres-1"
	svc.SetPodExecForTest(func(ctx context.Context, kubeconfig, namespace, pod, container string, command ...string) (string, error) {
		joined := strings.Join(command, " ")
		switch {
		case strings.Contains(joined, "pg_dump"):
			return "", nil
		case strings.Contains(joined, "wc -c"):
			return "5000000", nil
		case strings.Contains(joined, "information_schema.tables"):
			return "42", nil
		case strings.Contains(joined, "strategy_instance"):
			return "3", nil
		default:
			return "", nil
		}
	})
	store := NewDataCloneJobStore()
	svc.cloneJobs = store
	svc.cloneLast = NewDataCloneLastStore()
	job := store.Create(DataCloneJob{
		Status:  "queued",
		Source:  "bifrost_prod",
		Targets: []string{"bifrost_dev"},
		Mode:    "full",
		Trigger: "manual",
	})
	svc.runDataClone(context.Background(), job.ID)
	meta := svc.cloneLast.Get()
	if meta.LastCloneAt == nil {
		t.Fatal("expected last_clone_at recorded")
	}
	if meta.LastCloneJobID != job.ID {
		t.Fatalf("job id=%s want %s", meta.LastCloneJobID, job.ID)
	}
}
