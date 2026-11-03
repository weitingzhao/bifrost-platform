package migrate_test

import (
	"context"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
)

// Review 631f (round 6) R6-S2: the explicit runtime REVOKE in 0011, shown
// on its own. Before migrating, the migration owner's default privileges
// in this test's own database give the environment's runtime EXECUTE on
// every function the owner creates (pg_default_acl is per database). The
// PUBLIC revoke does not touch that direct grant; only the runtime revoke
// does.
func TestReviewR6RuntimeDefaultExecuteIsRevoked(t *testing.T) {
	t.Parallel()
	for _, env := range []string{"dev", "stg", "prod"} {
		t.Run(env, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			db := agentrttest.New(t)
			owner := db.Connect(t, db.Owner)
			rt := "platform_agentrt_" + env
			if _, err := owner.Exec(ctx, `CREATE SCHEMA agentrt; ALTER DEFAULT PRIVILEGES IN SCHEMA agentrt GRANT EXECUTE ON FUNCTIONS TO `+rt); err != nil {
				t.Fatal(err)
			}
			if _, err := migrate.Run(ctx, owner, env); err != nil {
				t.Fatal(err)
			}
			direct := `SELECT EXISTS (SELECT 1 FROM pg_proc p, aclexplode(p.proacl) a
			 WHERE p.oid = $1::regprocedure AND a.grantee = $2::regrole AND a.privilege_type = 'EXECUTE')`
			// A positive control: a function the owner creates now carries the
			// default grant, so the setup above is in force.
			if _, err := owner.Exec(ctx, `CREATE FUNCTION agentrt.review_r6_probe() RETURNS int LANGUAGE sql AS 'SELECT 1'`); err != nil {
				t.Fatal(err)
			}
			var granted bool
			if err := owner.QueryRow(ctx, direct, "agentrt.review_r6_probe()", rt).Scan(&granted); err != nil {
				t.Fatal(err)
			}
			if !granted {
				t.Fatalf("the default privileges gave %s no EXECUTE on a new function", rt)
			}
			var held, execute bool
			if err := owner.QueryRow(ctx, direct, "agentrt.record_debt_transition()", rt).Scan(&held); err != nil {
				t.Fatal(err)
			}
			if err := owner.QueryRow(ctx, `SELECT has_function_privilege($1,'agentrt.record_debt_transition()','EXECUTE')`, rt).Scan(&execute); err != nil {
				t.Fatal(err)
			}
			if held || execute {
				t.Fatalf("%s kept EXECUTE on the SECURITY DEFINER function: direct grant=%v effective=%v", rt, held, execute)
			}
		})
	}
}
