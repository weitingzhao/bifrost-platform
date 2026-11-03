package migrate_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
)

// Review 631e probe (round 5), kept as a regression test and widened to
// every SECURITY DEFINER function in agentrt. No broad runtimeAs grants:
// migrate each environment independently and examine the actual
// permissions created by the migrations.
func TestReviewR5DefinerPrivilegesEveryEnvironment(t *testing.T) {
	t.Parallel()
	for _, env := range []string{"dev", "stg", "prod"} {
		t.Run(env, func(t *testing.T) {
			ctx := context.Background()
			db := agentrttest.New(t)
			owner := db.Connect(t, db.Owner)
			_, err := migrate.Run(ctx, owner, env)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := owner.Query(ctx, `SELECT p.oid::regprocedure::text, p.proowner=(SELECT oid FROM pg_roles WHERE rolname=current_user),
			 coalesce(array_to_string(p.proconfig,','),''), has_function_privilege('public', p.oid, 'EXECUTE')
			 FROM pg_proc p WHERE p.pronamespace='agentrt'::regnamespace AND p.prosecdef ORDER BY 1`)
			if err != nil {
				t.Fatal(err)
			}
			var definers []string
			for rows.Next() {
				var fn, path string
				var owned, public bool
				if err = rows.Scan(&fn, &owned, &path, &public); err != nil {
					t.Fatal(err)
				}
				definers = append(definers, fn)
				if !owned || path != "search_path=pg_catalog, agentrt, pg_temp" || public {
					t.Errorf("%s: migration_owner=%v config=%q public_execute=%v", fn, owned, path, public)
				}
			}
			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}
			// A positive control: the scan reads the one definer 0011 creates.
			if len(definers) == 0 || definers[0] != "agentrt.record_debt_transition()" {
				t.Fatalf("SECURITY DEFINER functions in agentrt: %v", definers)
			}
			for _, role := range []string{"platform_agentrt_dev", "platform_agentrt_stg", "platform_agentrt_prod", "agent_reader"} {
				var insert, update, usage, reset bool
				err = owner.QueryRow(ctx, `SELECT has_table_privilege($1,'agentrt.debt_transition','INSERT'),
				 has_table_privilege($1,'agentrt.debt_transition','UPDATE'),
				 has_sequence_privilege($1,'agentrt.debt_record_seq','USAGE'),
				 has_sequence_privilege($1,'agentrt.debt_record_seq','UPDATE')`, role).Scan(&insert, &update, &usage, &reset)
				if err != nil {
					t.Fatal(err)
				}
				if insert || update || reset || usage != (role == "platform_agentrt_"+env) {
					t.Errorf("%s permissions: insert=%v update=%v usage=%v reset=%v", role, insert, update, usage, reset)
				}
				for _, fn := range definers {
					var execute bool
					if err = owner.QueryRow(ctx, `SELECT has_function_privilege($1,$2::regprocedure,'EXECUTE')`, role, fn).Scan(&execute); err != nil {
						t.Fatal(err)
					}
					if execute {
						t.Errorf("%s may attach the SECURITY DEFINER %s to its own table", role, fn)
					}
				}
				// SET ROLE is only the fixture's superuser selector. No grants are
				// added to the tested role. DML must fail before FK validation.
				admin := db.Connect(t, db.Admin)
				_, err = admin.Exec(ctx, fmt.Sprintf(`SET ROLE %s`, role))
				if err != nil {
					t.Fatal(err)
				}
				_, err = admin.Exec(ctx, `INSERT INTO agentrt.debt_transition(debt_id,event_id,from_state,to_state) VALUES('TDD-999',1,'open','doing')`)
				if err == nil || !strings.Contains(err.Error(), "permission denied") {
					t.Errorf("%s direct insert did not fail on permissions: %v", role, err)
				}
			}
		})
	}
}
