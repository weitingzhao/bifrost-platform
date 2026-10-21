package store_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The Console's fixture of these moments (console/src/api/__fixtures__/agentrt-progress-schema.testdb.json):
// Progress on a database at 0006 and with a failing loop, written when CONSOLE_SCHEMA_FIXTURE_WRITE=1.
const consoleSchemaFixture = "../../../../console/src/api/__fixtures__/agentrt-progress-schema.testdb.json"

type schemaDoc struct {
	Schema struct {
		State   string   `json:"state"`
		Missing []string `json:"missing"`
		Unknown []int    `json:"unknown"`
	} `json:"schema"`
	Alerts []struct {
		Loop  string `json:"loop"`
		Since string `json:"since"`
		Error string `json:"error"`
	} `json:"controller_alerts"`
}

func progressOf(t *testing.T, db store.DB) (schemaDoc, json.RawMessage) {
	t.Helper()
	out, err := store.Progress(ctx, db)
	ok(t, err)
	raw, err := json.Marshal(out)
	ok(t, err)
	var doc schemaDoc
	ok(t, json.Unmarshal(raw, &doc))
	return doc, raw
}

// TD-411 (STG 2026-10-20: the platform needed 0007 and 0008, the database was
// at 0006, the facts pass failed every 15 seconds). Progress, read as the
// runtime, says the schema is behind and names the two; on a database with
// every migration it is current, and ahead with one the binary lacks. A loop
// alerted as failing is listed with when and its error until it passes again;
// an alert names its loop by its digest, so an unknown name is not listed.
func TestProgressSaysTheSchemaAndTheFailingLoops(t *testing.T) {
	t.Parallel()
	at6 := agentrttest.New(t)
	files, err := migrate.Files()
	ok(t, err)
	_, err = migrate.RunFiles(ctx, at6.Connect(t, at6.Owner), "dev", files[:6])
	ok(t, err)
	behind, behindRaw := progressOf(t, at6.Connect(t, at6.Runtime))
	if behind.Schema.State != migrate.SchemaBehind || strings.Join(behind.Schema.Missing, ",") != "0007_gate_tasks,0008_attempt_prepare_failed" ||
		behind.Alerts == nil || len(behind.Alerts) != 0 {
		t.Fatalf("a database at 0006: %+v", behind)
	}

	w := newKernelWorld(t)
	if current, _ := progressOf(t, w.rt); current.Schema.State != migrate.SchemaCurrent || len(current.Schema.Missing) != 0 {
		t.Fatalf("a database with every migration: %+v", current.Schema)
	}
	failure := `ERROR: new row for relation "task" violates check constraint "task_check2" (SQLSTATE 23514)`
	ok(t, store.AlertFailingLoop(ctx, w.rt, "agentrt-facts-rules", store.FailuresToAlert, failure))
	ok(t, store.AlertFailingLoop(ctx, w.rt, "not-a-loop", store.FailuresToAlert, failure))
	failing, failingRaw := progressOf(t, w.rt)
	if len(failing.Alerts) != 1 || failing.Alerts[0].Loop != "agentrt-facts-rules" || failing.Alerts[0].Since == "" ||
		failing.Alerts[0].Error != "agentrt-facts-rules failed 4 times in a row:\n"+failure {
		t.Fatalf("a failing loop: %+v", failing.Alerts)
	}
	if os.Getenv("CONSOLE_SCHEMA_FIXTURE_WRITE") == "1" {
		raw, merr := json.MarshalIndent(map[string]any{"behind": behindRaw, "failing": failingRaw}, "", "  ")
		ok(t, merr)
		ok(t, os.WriteFile(consoleSchemaFixture, append(raw, '\n'), 0o644))
	}
	// Review 466 B-1: another loop alerted and answered a hundred times over
	// does not push the standing alert out of what Progress reads.
	for range 101 {
		ok(t, store.AlertFailingLoop(ctx, w.rt, "agentrt-assess", store.FailuresToAlert, "the evidence read failed"))
		ok(t, store.NoteLoopPasses(ctx, w.rt, "agentrt-assess", store.FailuresToAlert))
	}
	if still, _ := progressOf(t, w.rt); len(still.Alerts) != 1 || still.Alerts[0].Loop != "agentrt-facts-rules" {
		t.Fatalf("after another loop's 202 records: %+v", still.Alerts)
	}
	if open, oerr := store.LoopAlertOpen(ctx, w.rt, "agentrt-facts-rules"); oerr != nil || !open {
		t.Fatalf("the standing alert reads open %v (%v)", open, oerr)
	}
	ok(t, store.NoteLoopPasses(ctx, w.rt, "agentrt-facts-rules", 6))
	if open, oerr := store.LoopAlertOpen(ctx, w.rt, "agentrt-facts-rules"); oerr != nil || open {
		t.Fatalf("an answered alert reads open %v (%v)", open, oerr)
	}
	if passed, _ := progressOf(t, w.rt); len(passed.Alerts) != 0 {
		t.Fatalf("after it passes again: %+v", passed.Alerts)
	}
	_, err = w.db.Connect(t, w.db.Owner).Exec(ctx, `INSERT INTO agentrt.schema_migration (schema_migration_id, sha256) VALUES (999, $1)`,
		strings.Repeat("ab", 32))
	ok(t, err)
	if ahead, _ := progressOf(t, w.rt); ahead.Schema.State != migrate.SchemaAhead || len(ahead.Schema.Unknown) != 1 {
		t.Fatalf("a database with a migration this binary lacks: %+v", ahead.Schema)
	}
}
