package migrate_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
)

// The debt tables of 0011 (review 631 S-2, S-3).
var (
	debtState  = []string{"debt", "debt_counter", "debt_edge", "debt_ref", "debt_work", "debt_part", "debt_part_order", "debt_decision", "ratchet", "accepted_gap"}
	debtAppend = []string{"debt_text", "debt_evidence", "debt_evidence_check", "debt_part_landing", "debt_acceptance_run"}
)

// 0011 grants the runtime role exactly SELECT, INSERT and UPDATE on the
// debt state tables, SELECT and INSERT on the append-only ones and SELECT
// alone on the transitions the database records, nothing more, and
// agent_reader no write at all; in the test cluster the runtime
// role is a member of neither the migration owner nor another environment's
// runtime (the clusters' own role matrix is infra's to check).
func TestDebtTablesGrantExactly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	admin := db.Connect(t, db.Admin)
	privileges := func(role, tbl string) []string {
		t.Helper()
		rows, err := admin.Query(ctx, `SELECT p FROM unnest(ARRAY['SELECT','INSERT','UPDATE','DELETE','TRUNCATE','REFERENCES','TRIGGER']) p
		  WHERE has_table_privilege($1, 'agentrt.' || $2, p) ORDER BY p`, role, tbl)
		if err != nil {
			t.Fatal(err)
		}
		out, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	for _, tbl := range append(slices.Clone(debtState), debtAppend...) {
		want := []string{"INSERT", "SELECT", "UPDATE"}
		if slices.Contains(debtAppend, tbl) {
			want = []string{"INSERT", "SELECT"}
		}
		if got := privileges("platform_agentrt_dev", tbl); !slices.Equal(got, want) {
			t.Errorf("platform_agentrt_dev on %s: %v, want %v", tbl, got, want)
		}
		for _, p := range privileges("agent_reader", tbl) {
			if p != "SELECT" {
				t.Errorf("agent_reader may %s %s", p, tbl)
			}
		}
	}
	// The transitions are the database's own record: the runtime reads them.
	if got := privileges("platform_agentrt_dev", "debt_transition"); !slices.Equal(got, []string{"SELECT"}) {
		t.Errorf("platform_agentrt_dev on debt_transition: %v, want only SELECT", got)
	}
	for _, other := range []string{"agentrt_owner_dev", "platform_agentrt_stg", "platform_agentrt_prod"} {
		var member bool
		if err := admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1) AND pg_has_role('platform_agentrt_dev', $1, 'MEMBER')`,
			other).Scan(&member); err != nil {
			t.Fatal(err)
		}
		if member {
			t.Errorf("platform_agentrt_dev may act as %s", other)
		}
	}
}

// A failure late in 0011 rolls the whole file back: the event vocabulary,
// the two replaced functions, every new table and the migration record stay
// as 0010 left them; the file then applies as it is.
func TestUpgradeTo0011FailureRollsBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.New(t)
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil || len(files) < 11 || files[10].ID != 11 {
		t.Fatalf("0011 is missing: %v", err)
	}
	files = files[:11]
	if _, err := migrate.RunFiles(ctx, conn, "dev", files[:10]); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var s string
		if err := conn.QueryRow(ctx, `SELECT concat_ws('|',
		    (SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'event_type_closed'),
		    (SELECT md5(prosrc) FROM pg_proc WHERE proname = 'event_may_write'),
		    (SELECT md5(prosrc) FROM pg_proc WHERE proname = 'event_names_row'),
		    (SELECT count(*) FROM pg_class WHERE relnamespace = 'agentrt'::regnamespace AND relname ~ '^(debt|ratchet|accepted_gap)'),
		    (SELECT max(schema_migration_id) FROM agentrt.schema_migration))`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	before := snapshot()
	const anchor = "-- Grants, named one by one (as 0009)"
	if strings.Count(files[10].SQL, anchor) != 1 {
		t.Fatal("failure injection anchor changed")
	}
	broken := slices.Clone(files)
	broken[10].SQL = strings.Replace(files[10].SQL, anchor, "SELECT 1/0;\n"+anchor, 1)
	broken[10].SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(broken[10].SQL)))
	if applied, err := migrate.RunFiles(ctx, conn, "dev", broken); err == nil || !strings.Contains(err.Error(), "division by zero") || len(applied) != 0 {
		t.Fatalf("the broken 0011: applied %v, err %v", applied, err)
	}
	if after := snapshot(); after != before {
		t.Fatalf("a failed 0011 left a trace:\n%s\nwas\n%s", after, before)
	}
	if applied, err := migrate.RunFiles(ctx, conn, "dev", files); err != nil || !slices.Equal(applied, []int{11}) {
		t.Fatalf("0011 after the failure: applied %v, err %v", applied, err)
	}
}
