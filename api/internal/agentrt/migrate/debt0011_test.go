package migrate_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
)

// The debt tables of 0011 (review 631 S-2, S-3).
var (
	debtState  = []string{"debt", "debt_counter", "debt_edge", "debt_ref", "debt_work", "debt_part", "debt_part_order", "debt_decision", "ratchet", "accepted_gap"}
	debtAppend = []string{"debt_text", "debt_evidence", "debt_evidence_check", "debt_part_landing", "debt_acceptance_run"}
)

// A failure late in 0011 rolls the whole file back: the event vocabulary,
// the two replaced functions, every new table and the migration record stay
// as 0010 left them, and so does what snapshot0011 reads (the definitions,
// ACLs, trigger states, rows and sequences it lists); the file then applies
// as it is. It fails once before the
// grants (after 0010's functions are replaced and the triggers on event are
// added) and once after its last statement (the grants done too).
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
	before, full := snapshot(), snapshot0011(ctx, t, conn)
	const anchor = "-- Grants, named one by one (as 0009)"
	if strings.Count(files[10].SQL, anchor) != 1 {
		t.Fatal("failure injection anchor changed")
	}
	for where, sql := range map[string]string{
		"before the grants":        strings.Replace(files[10].SQL, anchor, "SELECT 1/0;\n"+anchor, 1),
		"after the last statement": files[10].SQL + "\nSELECT 1/0;\n",
	} {
		broken := slices.Clone(files)
		broken[10].SQL = sql
		broken[10].SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(broken[10].SQL)))
		if applied, err := migrate.RunFiles(ctx, conn, "dev", broken); err == nil || !strings.Contains(err.Error(), "division by zero") || len(applied) != 0 {
			t.Fatalf("0011 broken %s: applied %v, err %v", where, applied, err)
		}
		if after := snapshot(); after != before {
			t.Fatalf("0011 broken %s left a trace:\n%s\nwas\n%s", where, after, before)
		}
		assertSnapshot0011(ctx, t, conn, full)
	}
	if applied, err := migrate.RunFiles(ctx, conn, "dev", files); err != nil || !slices.Equal(applied, []int{11}) {
		t.Fatalf("0011 after the failure: applied %v, err %v", applied, err)
	}
}
