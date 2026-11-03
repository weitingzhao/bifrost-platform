package store_test

import (
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The runtime closes a debt on its own only with every condition of §6.6 and
// T12 the database can read, each refused while it is missing (review 631
// M-1); the crew reopens an automatic close within seven days, with its
// reason, and not after; the runtime never reopens.
func TestDebtAutoCloseNeedsItsEvidence(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	const crew, other = "thread:crew-1", "thread:crew-2"
	D := dw.fileWithSpec(t, "closes on its own")
	dw.legacy(t, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ('thread:crew-2', 'thread', 'crew 2')`)
	var part int64
	ok(t, dw.rt.QueryRow(ctx, `SELECT debt_part_id FROM agentrt.debt_part WHERE debt_id = $1`, D).Scan(&part))
	var proof string
	ok(t, store.InTx(ctx, dw.rt, func(tx pgx.Tx) error {
		var err error
		proof, err = store.PutArtifact(ctx, tx, crew, "report", "text/markdown", []byte("looked, fine"))
		return err
	}))
	closeIt := func() error {
		return dw.run(t, de(D, "debt_auto_closed", "controller", map[string]any{"from": "watching", "to": "closed"}), fmt.Sprintf(`UPDATE agentrt.debt SET state = 'closed',
		  conclusion = 'auto_closed', watch_until = NULL, watch_text_id = NULL, state_since = clock_timestamp(), version = version + 1
		  WHERE debt_id = '%s'`, D))
	}
	addText := func(id int64, kind string) {
		ok(t, dw.run(t, de(D, "debt_text_added", crew, map[string]any{"debt_text_id": id, "text_kind": kind,
			"sha256": "3e23e8160039594a33894f6564e1b1348bbd7a0088d42c4acb73eeaed59c009d"}), debtTextSQL(id, D, kind, crew)))
	}
	work := de(D, "debt_work_linked", crew, map[string]any{"debt_work_id": 900601, "from": "open", "to": "doing"})
	work.MissionID = "M-1"
	ok(t, dw.run(t, work, fmt.Sprintf(`INSERT INTO agentrt.debt_work (debt_work_id, debt_id, mission_id, started_at)
	  OVERRIDING SYSTEM VALUE VALUES (900601, '%s', 'M-1', now())`, D), debtMoveSQL(D, "doing", "")))
	watch := func(text int64) error {
		return dw.run(t, de(D, "debt_state_changed", crew, map[string]any{"from": "doing", "to": "watching", "debt_text_ids": []any{text}}),
			debtTextSQL(text, D, "watch", crew), fmt.Sprintf(`UPDATE agentrt.debt SET state = 'watching', watch_until = now() - interval '1 hour',
			  watch_text_id = %d, state_since = clock_timestamp(), version = version + 1 WHERE debt_id = '%s'`, text, D))
	}
	wantErr(t, watch(900602), "a part has no landing the runtime verified")
	ok(t, dw.run(t, de(D, "debt_part_landed", "controller", map[string]any{"debt_part_id": part, "debt_part_landing_id": 900603,
		"commit": "0123456789abcdef0123456789abcdef01234567", "via": "runtime_landing"}), fmt.Sprintf(`INSERT INTO agentrt.debt_part_landing
		  (debt_part_landing_id, debt_part_id, commit_sha, via, deployed_env, deployed_at, deploy_read, created_by_actor_id)
		  OVERRIDING SYSTEM VALUE VALUES (900603, %d, '0123456789abcdef0123456789abcdef01234567', 'runtime_landing', 'stg',
		    now() - interval '2 days', 'observed', 'controller')`, part)))
	ok(t, watch(900604))
	wantErr(t, closeIt(), "its acceptance spec was not reviewed")
	ok(t, dw.run(t, de(D, "debt_acceptance_reviewed", other, map[string]any{"acceptance_sha256": dw.specSHA(t, D)}),
		fmt.Sprintf(`UPDATE agentrt.debt SET acceptance_reviewed_sha256 = agentrt.debt_spec_sha(acceptance_json),
		  acceptance_reviewed_by = '%s', version = version + 1 WHERE debt_id = '%s'`, other, D)))
	wantErr(t, closeIt(), "its latest acceptance run is not a pass")
	ran := func(id int64, result, actor string) store.Event {
		return de(D, "debt_acceptance_ran", actor, map[string]any{"debt_acceptance_run_id": id, "runner": "manual", "result": result})
	}
	ok(t, dw.run(t, ran(900605, "unknown", crew), debtRunSQL(900605, D, "manual", "unknown", crew, "", proof, 0)))
	ok(t, dw.run(t, ran(900606, "pass", other), debtRunSQL(900606, D, "manual", "pass", crew, other, proof, 900605)))
	// The Owner's class does not apply: it does not await him.
	wantErr(t, dw.run(t, de(D, "debt_state_changed", crew, map[string]any{"from": "watching", "to": "awaiting_signoff"}),
		fmt.Sprintf(`UPDATE agentrt.debt SET state = 'awaiting_signoff', watch_until = NULL, watch_text_id = NULL,
		  state_since = clock_timestamp(), version = version + 1 WHERE debt_id = '%s'`, D)), "it closes on its own")
	wantErr(t, closeIt(), "no sign-off summary")
	addText(900607, "signoff_summary")
	wantErr(t, closeIt(), "no ratchet")
	addText(900608, "no_ratchet_reason")
	wantErr(t, closeIt(), "no follow-up")
	addText(900609, "no_follow_up_reason")
	ok(t, closeIt())

	// Reopening: never the runtime; the crew with its reason, within seven
	// days of the close (the close is aged past the store, as the owner).
	reopen := func(actor string, text int64) error {
		return dw.run(t, de(D, "debt_reopened", actor, map[string]any{"debt_text_ids": []any{text}, "from": "closed", "to": "doing"}),
			debtTextSQL(text, D, "note", actor),
			debtMoveSQL(D, "doing", ""))
	}
	wantErr(t, reopen("controller", 900610), "controller does not write a debt_reopened event")
	wantErr(t, dw.run(t, de(D, "debt_reopened", crew, map[string]any{"from": "closed", "to": "doing"}), debtMoveSQL(D, "doing", "")),
		"carries its reason")
	age := func(days int) {
		admin := dw.db.Connect(t, dw.db.Admin)
		_, err := admin.Exec(ctx, `SET session_replication_role = replica`)
		ok(t, err)
		_, err = admin.Exec(ctx, fmt.Sprintf(`UPDATE agentrt.debt SET state_since = now() - interval '%d days' WHERE debt_id = '%s'`, days, D))
		ok(t, err)
	}
	age(8)
	wantErr(t, reopen(crew, 900611), "only the Owner reopens it")
	age(6)
	ok(t, reopen(crew, 900612))
}
