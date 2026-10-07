package cluster

import (
	"net/http"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestRepairPostgresWalStoreDoesNotDeleteFailedBackup(t *testing.T) {
	now := time.Now().UTC()
	failed := backupCRAged("bifrost-postgres-ondemand-failed", "failed", now.Add(-40*24*time.Hour))
	recent := backupCRAged("bifrost-postgres-daily-recent", "walArchivingFailing", now.Add(-time.Hour))
	dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), failed, recent)
	svc, _ := externalMinioService(t, http.StatusOK)
	svc.SetDynamicFactoryForTest(func() (dynamic.Interface, error) {
		return dyn, nil
	})

	resp, err := svc.RepairPostgresWalStore(t.Context())
	if err != nil || !resp.OK {
		t.Fatalf("repair should still trigger a backup, err=%v resp=%+v", err, resp)
	}
	for _, action := range dyn.Actions() {
		if action.GetVerb() == "delete" {
			t.Fatalf("repair issued a delete: %#v", action)
		}
	}
	for _, name := range []string{failed.GetName(), recent.GetName()} {
		if _, gerr := dyn.Resource(cnpgBackupGVR).Namespace(cnpgNamespace).Get(t.Context(), name, metav1.GetOptions{}); gerr != nil {
			t.Fatalf("failed Backup %s was removed: %v", name, gerr)
		}
	}
}

func TestSweepExpiredFailedBackupsKeepsRecentFailures(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	oldFailed := backupCRAged("bifrost-postgres-daily-old", "failed", now.Add(-31*24*time.Hour))
	youngFailed := backupCRAged("bifrost-postgres-daily-young", "failed", now.Add(-29*24*time.Hour))
	oldWal := backupCRAged("bifrost-postgres-daily-wal", "walArchivingFailing", now.Add(-40*24*time.Hour))
	oldOK := backupCRAged("bifrost-postgres-daily-ok", "completed", now.Add(-40*24*time.Hour))
	foreign := backupCRAged("other-system-backup", "failed", now.Add(-40*24*time.Hour))
	undated := backupCRAged("bifrost-postgres-daily-undated", "failed", time.Time{})
	dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), oldFailed, youngFailed, oldWal, oldOK, foreign, undated)
	svc := NewService(nil)
	svc.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })

	resp, err := svc.sweepExpiredFailedBackupCRs(t.Context(), now)
	if err != nil || !resp.OK || !resp.Changed {
		t.Fatalf("sweep err=%v resp=%+v", err, resp)
	}
	got := map[string]bool{}
	for _, name := range resp.DeletedBackups {
		got[name] = true
	}
	if !got[oldFailed.GetName()] || !got[oldWal.GetName()] || len(got) != 2 {
		t.Fatalf("deleted = %v", resp.DeletedBackups)
	}
	for _, name := range []string{youngFailed.GetName(), oldOK.GetName(), foreign.GetName(), undated.GetName()} {
		if _, gerr := dyn.Resource(cnpgBackupGVR).Namespace(cnpgNamespace).Get(t.Context(), name, metav1.GetOptions{}); gerr != nil {
			t.Fatalf("%s should remain: %v", name, gerr)
		}
	}
	for _, action := range dyn.Actions() {
		if action.GetVerb() != "delete" {
			continue
		}
		del, ok := action.(k8stesting.DeleteAction)
		if !ok {
			continue
		}
		if del.GetName() == youngFailed.GetName() || del.GetName() == oldOK.GetName() {
			t.Fatalf("sweep deleted %s", del.GetName())
		}
	}
}

func TestPickExpiredFailedBackupNames(t *testing.T) {
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	items := []unstructured.Unstructured{
		*backupCRAged("bifrost-postgres-a", "failed", now.Add(-30*24*time.Hour)),
		*backupCRAged("bifrost-postgres-b", "failed", now.Add(-30*24*time.Hour-time.Second)),
	}
	got := pickExpiredFailedBackupNames(items, now, failedBackupMaxAge)
	if len(got) != 1 || got[0] != "bifrost-postgres-b" {
		t.Fatalf("exactly 30 days stays, one second over goes: %v", got)
	}
	if !strings.Contains(strings.Join(got, ","), "bifrost-postgres-b") {
		t.Fatal(got)
	}
}

func backupCRAged(name, phase string, created time.Time) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "Backup"})
	u.SetName(name)
	u.SetNamespace(cnpgNamespace)
	if !created.IsZero() {
		u.SetCreationTimestamp(metav1.NewTime(created))
	}
	_ = unstructured.SetNestedField(u.Object, phase, "status", "phase")
	return u
}
