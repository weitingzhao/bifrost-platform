package store_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Review 631f (round 6) R6-S1: unlike r5forge's table, these foreign tables
// have the real table's own name, debt, one temporary and one in a schema
// of the runtime's, each with a trigger shaped and named as the real one
// and the caller's search_path pointing at it. EXECUTE is granted on
// purpose, so that only the function's relation check (TG_RELID, not the
// table's name) stands between them and a forged transition.
func TestReviewR6SameNamedForeignDebtIsRefused(t *testing.T) {
	t.Parallel()
	for _, schema := range []string{"pg_temp", "review_r6"} {
		t.Run(schema, func(t *testing.T) {
			t.Parallel()
			dw := newDebtWorld(t, true)
			const d = "TDS-980"
			importAwaiting(t, dw, d, "pass", 0, 980000)
			var role string
			ok(t, dw.rt.QueryRow(ctx, `SELECT current_user`).Scan(&role))
			admin := dw.db.Connect(t, dw.db.Admin)
			_, err := admin.Exec(ctx, `GRANT EXECUTE ON FUNCTION agentrt.record_debt_transition() TO `+pgx.Identifier{role}.Sanitize())
			ok(t, err)
			if schema != "pg_temp" {
				_, err = admin.Exec(ctx, `CREATE SCHEMA review_r6 AUTHORIZATION `+pgx.Identifier{role}.Sanitize())
				ok(t, err)
			}
			table := pgx.Identifier{schema, "debt"}.Sanitize()
			create := `CREATE TABLE ` + table + `(debt_id text, state text)`
			if schema == "pg_temp" {
				create = `CREATE TEMP TABLE debt(debt_id text, state text) ON COMMIT DROP`
			}
			err = store.InTx(ctx, dw.rt, func(tx pgx.Tx) error {
				return r4step(tx, de(d, "debt_state_changed", "controller", map[string]any{
					"from": "awaiting_signoff", "to": "doing",
				}), create,
					`SET LOCAL search_path=review_r6,pg_temp,public,agentrt,pg_catalog`,
					`INSERT INTO `+table+` VALUES ('`+d+`','awaiting_signoff')`,
					`CREATE TRIGGER debt_20_transition AFTER UPDATE ON `+table+` FOR EACH ROW EXECUTE FUNCTION agentrt.record_debt_transition()`,
					`UPDATE `+table+` SET state='doing'`)
			})
			r5forgeRefused(t, dw, d, err, "records only as debt_20_transition")
		})
	}
}

// The real trigger renamed fails closed (a move is refused, not recorded
// silently or skipped) and works again once its name is restored.
func TestReviewR6RenamingTheRealTriggerFailsClosed(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	const d = "TDS-980"
	importAwaiting(t, dw, d, "pass", 0, 980000)
	admin := dw.db.Connect(t, dw.db.Admin)
	_, err := admin.Exec(ctx, `ALTER TRIGGER debt_20_transition ON agentrt.debt RENAME TO debt_21_renamed`)
	ok(t, err)
	wantErr(t, (&closing{dw: dw, t: t, D: d}).signOff(), "records only as debt_20_transition")
	_, err = admin.Exec(ctx, `ALTER TRIGGER debt_21_renamed ON agentrt.debt RENAME TO debt_20_transition`)
	ok(t, err)
	ok(t, (&closing{dw: dw, t: t, D: d}).signOff())
}
