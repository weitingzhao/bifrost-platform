package remediation

import (
	"testing"
	"time"
)

func newTestJobStoreDir(t *testing.T) *JobStore {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PLATFORM_REMEDIATION_JOBS_DIR", dir)
	return NewJobStore()
}

func TestJobStorePutGetRoundTrip(t *testing.T) {
	store := newTestJobStoreDir(t)

	if _, ok := store.Get("missing"); ok {
		t.Fatal("Get(missing) = true, want false")
	}

	store.Put(Job{ID: "job-1", Status: JobRunning, Phase: PhaseDiagnosing, Scope: "test-scope"})

	got, ok := store.Get("job-1")
	if !ok {
		t.Fatal("Get(job-1) = false, want true")
	}
	if got.Status != JobRunning || got.Phase != PhaseDiagnosing || got.Scope != "test-scope" {
		t.Fatalf("Get(job-1) = %+v", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("expected CreatedAt/UpdatedAt to be populated by Put: %+v", got)
	}
}

func TestJobStorePutPreservesCreatedAtOnUpdate(t *testing.T) {
	store := newTestJobStoreDir(t)

	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store.Put(Job{ID: "job-1", Status: JobRunning, CreatedAt: created})
	first, _ := store.Get("job-1")

	time.Sleep(2 * time.Millisecond)
	store.Put(Job{ID: "job-1", Status: JobDone, CreatedAt: created})
	second, _ := store.Get("job-1")

	if !second.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt changed on update: got %v, want %v", second.CreatedAt, created)
	}
	if !second.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("UpdatedAt did not advance: first=%v second=%v", first.UpdatedAt, second.UpdatedAt)
	}
	if second.Status != JobDone {
		t.Fatalf("Status = %q, want done", second.Status)
	}
}

func TestJobStoreListOrdersByUpdatedAtDesc(t *testing.T) {
	store := newTestJobStoreDir(t)

	store.Put(Job{ID: "a"})
	time.Sleep(2 * time.Millisecond)
	store.Put(Job{ID: "b"})
	time.Sleep(2 * time.Millisecond)
	store.Put(Job{ID: "c"})

	list := store.List()
	if len(list) != 3 {
		t.Fatalf("List() len = %d, want 3", len(list))
	}
	if list[0].ID != "c" || list[1].ID != "b" || list[2].ID != "a" {
		t.Fatalf("List() order = [%s %s %s], want [c b a]", list[0].ID, list[1].ID, list[2].ID)
	}
}

func TestJobStoreListEmptyDirReturnsEmpty(t *testing.T) {
	store := newTestJobStoreDir(t)
	list := store.List()
	if len(list) != 0 {
		t.Fatalf("List() = %+v, want empty", list)
	}
}

func TestMergeJobsPrefersNewerUpdatedAt(t *testing.T) {
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)

	stored := []Job{{ID: "j1", Status: JobRunning, UpdatedAt: newer}}
	runner := []Job{{ID: "j1", Status: JobDone, UpdatedAt: older}}

	merged := mergeJobs(runner, stored)
	if len(merged) != 1 || merged[0].Status != JobRunning {
		t.Fatalf("mergeJobs() = %+v, want stored (newer) job to win", merged)
	}

	// Runner job is newer this time — it should win instead.
	runner[0].UpdatedAt = newer.Add(time.Hour)
	merged = mergeJobs(runner, stored)
	if len(merged) != 1 || merged[0].Status != JobDone {
		t.Fatalf("mergeJobs() = %+v, want runner (newer) job to win", merged)
	}
}

func TestReconcileOrphanedJobsMarksStaleRunningJobsCancelled(t *testing.T) {
	stored := []Job{
		{ID: "running-orphan", Status: JobRunning},
		{ID: "already-done", Status: JobDone},
	}

	merged := ReconcileOrphanedJobs(nil, stored)
	if len(merged) != 2 {
		t.Fatalf("ReconcileOrphanedJobs() len = %d, want 2", len(merged))
	}

	var orphan, done *Job
	for i := range merged {
		switch merged[i].ID {
		case "running-orphan":
			orphan = &merged[i]
		case "already-done":
			done = &merged[i]
		}
	}
	if orphan == nil || orphan.Status != JobCancelled || orphan.Phase != PhaseCancelled || orphan.Error != "orphaned" {
		t.Fatalf("orphaned job = %+v", orphan)
	}
	if done == nil || done.Status != JobDone {
		t.Fatalf("done job should be untouched: %+v", done)
	}
}

func TestReconcileOrphanedJobsKeepsActiveRunnerJobs(t *testing.T) {
	stored := []Job{{ID: "active-1", Status: JobRunning}}
	runner := []Job{{ID: "active-1", Status: JobRunning, UpdatedAt: time.Now().UTC()}}

	merged := ReconcileOrphanedJobs(runner, stored)
	if len(merged) != 1 || merged[0].Status != JobRunning {
		t.Fatalf("expected active runner job to remain running, got %+v", merged)
	}
}
