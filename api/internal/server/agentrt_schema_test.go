package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
)

// TD-411 (STG 2026-10-20: a platform needing 0007 and 0008 ran on a database
// at 0006; the controller failed every 15 seconds with a warning, and health
// said ok). Read as the runtime: a database at 0006 is behind, said in health
// with the two missing migrations, with one ERROR in the log and no second
// for the same read; once the Job has run it is current, said with INFO.
// /health keeps answering ok, and says off without the runtime.
func TestHealthSaysTheSchemaIsBehind(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.New(t)
	owner := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil || len(files) < 9 {
		t.Fatalf("files = %d %v", len(files), err)
	}
	if _, err = migrate.RunFiles(ctx, owner, "dev", files[:6]); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var lines []string
	w := &schemaWatch{log: func(level slog.Level, msg string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, level.String()+" "+msg+" "+fmt.Sprint(args...))
	}}
	runtime := db.Connect(t, db.Runtime)
	w.check(ctx, runtime)
	w.check(ctx, runtime)

	srv := newTestServer(t, newTestConfig(t))
	health := func() map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
		var body map[string]any
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil {
			t.Fatalf("health %d %s", rec.Code, rec.Body.String())
		}
		return body
	}
	if off := health(); off["agentrt_schema"] != "off" || off["status"] != "ok" {
		t.Fatalf("without the runtime: %v", off)
	}
	srv.agentRuntime = &agentRuntime{schema: w}
	behind := health()
	missing, _ := behind["agentrt_schema_missing"].([]any)
	if behind["status"] != "ok" || behind["agentrt_schema"] != migrate.SchemaBehind || len(missing) != 3 ||
		missing[0] != "0007_gate_tasks" || missing[1] != "0008_attempt_prepare_failed" || missing[2] != "0009_issues" {
		t.Fatalf("behind: %v", behind)
	}
	// What the PROD deliver pipeline compares the next build's migrations
	// with (TD-411): every applied migration, by number, with its sha256.
	appliedIs := func(h map[string]any, n int) {
		t.Helper()
		list, _ := h["agentrt_schema_applied"].([]any)
		if len(list) != n {
			t.Fatalf("applied: %v; want %d", h["agentrt_schema_applied"], n)
		}
		for i, e := range list {
			m, _ := e.(map[string]any)
			if m["id"] != float64(files[i].ID) || m["sha256"] != files[i].SHA256 {
				t.Fatalf("applied %d: %v; want %d %s", i, e, files[i].ID, files[i].SHA256)
			}
		}
	}
	appliedIs(behind, 6)
	mu.Lock()
	if len(lines) != 1 || lines[0][:5] != "ERROR" {
		t.Fatalf("the log: %q; want one ERROR", lines)
	}
	mu.Unlock()

	if _, err = migrate.RunFiles(ctx, owner, "dev", files); err != nil {
		t.Fatal(err)
	}
	w.check(ctx, runtime)
	current := health()
	if current["agentrt_schema"] != migrate.SchemaCurrent || current["agentrt_schema_missing"] != nil {
		t.Fatalf("after the Job: %v", current)
	}
	appliedIs(current, len(files))
	mu.Lock()
	if len(lines) != 2 || lines[1][:4] != "INFO" || !slices.ContainsFunc(lines, func(l string) bool { return l[:5] == "ERROR" }) {
		t.Fatalf("the log: %q", lines)
	}
	mu.Unlock()

	// Review 466 C-1: a read that fails says so once and makes the schema
	// unknown; the next good read says the state again, the same as before.
	w.check(ctx, unreadable{})
	if h := health(); h["agentrt_schema"] != "unknown" || h["agentrt_schema_applied"] != nil {
		t.Fatalf("after a failed read: %v", h)
	}
	w.check(ctx, runtime)
	mu.Lock()
	defer mu.Unlock()
	if len(lines) != 4 || lines[2][:4] != "WARN" || lines[3][:4] != "INFO" || health()["agentrt_schema"] != migrate.SchemaCurrent {
		t.Fatalf("the log after a failed read and a good one: %q", lines)
	}
}

// unreadable is a database that cannot be read.
type unreadable struct{}

func (unreadable) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("connection refused")
}
