package cluster

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestClassifyBackupFreshness(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	fresh, signal, detail := classifyBackupFreshness(nil, now, backupFreshMaxAge)
	if fresh || signal != "fail" {
		t.Fatalf("nil last → fail, got fresh=%v signal=%s detail=%s", fresh, signal, detail)
	}

	okAt := now.Add(-12 * time.Hour)
	fresh, signal, detail = classifyBackupFreshness(&okAt, now, backupFreshMaxAge)
	if !fresh || signal != "ok" {
		t.Fatalf("12h ago should be fresh: fresh=%v signal=%s detail=%s", fresh, signal, detail)
	}

	staleAt := now.Add(-49 * time.Hour)
	fresh, signal, detail = classifyBackupFreshness(&staleAt, now, backupFreshMaxAge)
	if fresh || signal != "fail" {
		t.Fatalf("49h ago should fail: fresh=%v signal=%s detail=%s", fresh, signal, detail)
	}
	if detail == "" {
		t.Fatal("stale detail should mention age")
	}

	edge := now.Add(-48 * time.Hour)
	fresh, signal, _ = classifyBackupFreshness(&edge, now, backupFreshMaxAge)
	if !fresh || signal != "ok" {
		t.Fatalf("exactly 48h should still be fresh, got fresh=%v signal=%s", fresh, signal)
	}
}

func TestPickLatestCompletedBackup(t *testing.T) {
	items := []unstructured.Unstructured{
		*backupCR("failed-1", "failed", "2026-08-09T10:00:00Z"),
		*backupCR("old-ok", "completed", "2026-08-08T03:00:00Z"),
		*backupCR("newest-ok", "completed", "2026-08-10T03:00:00Z"),
		*backupCR("running", "running", ""),
	}
	got := pickLatestCompletedBackup(items)
	if got == nil || got.Name != "newest-ok" {
		t.Fatalf("expected newest-ok, got %+v", got)
	}
	if got.StoppedAt.UTC().Format(time.RFC3339) != "2026-08-10T03:00:00Z" {
		t.Fatalf("stoppedAt = %s", got.StoppedAt)
	}
}

func TestPickLatestCompletedBackupEmpty(t *testing.T) {
	if got := pickLatestCompletedBackup(nil); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
	items := []unstructured.Unstructured{*backupCR("only-fail", "failed", "2026-08-10T03:00:00Z")}
	if got := pickLatestCompletedBackup(items); got != nil {
		t.Fatalf("failed-only should be nil, got %+v", got)
	}
}

func TestNewOnDemandBackupCR(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 30, 0, 0, time.UTC)
	obj := newOnDemandBackupCR("bifrost-postgres-ondemand-test", now)
	if obj.GetKind() != "Backup" || obj.GetName() != "bifrost-postgres-ondemand-test" {
		t.Fatalf("unexpected CR: kind=%s name=%s", obj.GetKind(), obj.GetName())
	}
	if obj.GetNamespace() != cnpgNamespace {
		t.Fatalf("ns = %s", obj.GetNamespace())
	}
	method := stringFromUnstructured(obj, "spec", "method")
	cluster := stringFromUnstructured(obj, "spec", "cluster", "name")
	if method != "barmanObjectStore" || cluster != cnpgClusterName {
		t.Fatalf("spec method=%s cluster=%s", method, cluster)
	}
}

func backupCR(name, phase, stoppedAt string) *unstructured.Unstructured {
	status := map[string]any{"phase": phase}
	if stoppedAt != "" {
		status["stoppedAt"] = stoppedAt
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "postgresql.cnpg.io/v1",
		"kind":       "Backup",
		"metadata":   map[string]any{"name": name, "namespace": cnpgNamespace},
		"status":     status,
	}}
}
