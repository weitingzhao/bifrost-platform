package actuation

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

// Origin: 2026-11-16, an independent review of the foundation code found the
// three writes of the audit log dropped their error (_ = l.persistLocked()):
// a log that could not reach its file went on as if it had, and a restart
// lost the records with nothing said. Now the failure is logged, the record
// stays in memory and the action is not failed.
func TestAuditLogSaysWhenItCannotWriteItsFile(t *testing.T) {
	var out bytes.Buffer
	was := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&out, nil)))
	t.Cleanup(func() { slog.SetDefault(was) })

	// The positive control: a file that can be written says nothing.
	good := NewAuditLog(filepath.Join(t.TempDir(), "audit.json"))
	good.RecordDirect("tester", RoleAdmin, "probe", "target", "ok", "")
	if strings.Contains(out.String(), "audit log not written") {
		t.Fatalf("a log that was written reported a failure: %s", out.String())
	}

	// A path under a file cannot be created.
	bad := &AuditLog{path: filepath.Join(good.path, "under-a-file", "audit.json")}
	bad.RecordDirect("tester", RoleAdmin, "probe", "target", "ok", "")
	if !strings.Contains(out.String(), "audit log not written to its file") {
		t.Fatalf("the failed write was not reported: %q", out.String())
	}
	if len(bad.records) != 1 {
		t.Fatalf("the record is not held in memory: %d", len(bad.records))
	}
}
