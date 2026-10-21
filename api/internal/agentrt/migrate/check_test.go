package migrate_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
)

// TD-411 (STG 2026-10-20 23:24 CDT: a platform needing 0007 and 0008 ran on a
// database at 0006): the platform reads, as its runtime role, which
// migrations the database applied. A database with none is behind by all; one
// at 0006 is behind by the two this binary carries after it, named by file;
// one that applied them all is current. A migration the database has and the
// binary does not makes it ahead.
func TestCheckSaysWhatTheDatabaseMisses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.New(t)
	owner := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil || len(files) < 8 {
		t.Fatalf("files = %d %v", len(files), err)
	}
	names := func(fs []migrate.File) []string {
		var out []string
		for _, f := range fs {
			out = append(out, strings.TrimSuffix(f.Name, ".sql"))
		}
		return out
	}

	none, err := migrate.Check(ctx, owner)
	if err != nil || none.State != migrate.SchemaBehind || !slices.Equal(none.Missing, names(files)) {
		t.Fatalf("an empty database: %+v (%v)", none, err)
	}
	if _, err = migrate.RunFiles(ctx, owner, "dev", files[:6]); err != nil {
		t.Fatal(err)
	}
	runtime := db.Connect(t, db.Runtime)
	at6, err := migrate.Check(ctx, runtime)
	if err != nil || at6.State != migrate.SchemaBehind || !slices.Equal(at6.Missing, names(files[6:])) ||
		at6.Missing[0] != "0007_gate_tasks" || len(at6.Unknown) != 0 || len(at6.Edited) != 0 {
		t.Fatalf("a database at 0006, read as the runtime: %+v (%v)", at6, err)
	}
	if _, err = migrate.RunFiles(ctx, owner, "dev", files); err != nil {
		t.Fatal(err)
	}
	if all, cerr := migrate.Check(ctx, runtime); cerr != nil || all.State != migrate.SchemaCurrent || len(all.Missing) != 0 {
		t.Fatalf("a database with every migration: %+v (%v)", all, cerr)
	}
	if _, err = owner.Exec(ctx, `INSERT INTO agentrt.schema_migration (schema_migration_id, sha256) VALUES (999, $1)`,
		strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	if ahead, cerr := migrate.Check(ctx, runtime); cerr != nil || ahead.State != migrate.SchemaAhead || !slices.Equal(ahead.Unknown, []int{999}) {
		t.Fatalf("a database with a migration this binary lacks: %+v (%v)", ahead, cerr)
	}
}
