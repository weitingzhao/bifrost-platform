package migrate_test

import (
	"context"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
)

// Review 631e probe (round 5) found the debt ledger's one SECURITY DEFINER
// function attachable by a runtime role; the test held every definer in
// agentrt to its owner, its search path and no public EXECUTE. Migration 0021
// removed the ledger and that function with it (2026-11-14), so the schema
// has none, and this holds it there: a definer added later is looked at, with
// those three questions, before this test is changed. Each environment is
// migrated on its own, as the grants differ by environment.
func TestReviewR5DefinerPrivilegesEveryEnvironment(t *testing.T) {
	t.Parallel()
	for _, env := range []string{"dev", "stg", "prod"} {
		t.Run(env, func(t *testing.T) {
			ctx := context.Background()
			db := agentrttest.New(t)
			owner := db.Connect(t, db.Owner)
			if _, err := migrate.Run(ctx, owner, env); err != nil {
				t.Fatal(err)
			}
			var functions, definers int
			var names string
			if err := owner.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE p.prosecdef),
			 coalesce(string_agg(p.oid::regprocedure::text, ', ') FILTER (WHERE p.prosecdef), '')
			 FROM pg_proc p WHERE p.pronamespace='agentrt'::regnamespace`).Scan(&functions, &definers, &names); err != nil {
				t.Fatal(err)
			}
			// The positive control: the scan reads the schema's functions.
			if functions == 0 {
				t.Fatal("the scan found no function in agentrt at all")
			}
			if definers != 0 {
				t.Fatalf("SECURITY DEFINER functions in agentrt: %s; each needs its owner, a pinned search_path and no EXECUTE for PUBLIC or a runtime role", names)
			}
		})
	}
}
