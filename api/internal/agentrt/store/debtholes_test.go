package store_test

import (
	"fmt"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The holes the cross-model review of 8163d6d8 found (M-1 to M-4), each
// written past the store as the runtime role: the refusal, and the allowed
// counterpart beside it.

// specSHA is a debt's current spec hash as the database computes it.
func (dw *debtWorld) specSHA(t *testing.T, debt string) string {
	t.Helper()
	var sum string
	ok(t, dw.rt.QueryRow(ctx, `SELECT encode(sha256(convert_to(acceptance_json::text, 'UTF8')), 'hex') FROM agentrt.debt
	  WHERE debt_id = $1`, debt).Scan(&sum))
	return sum
}

// note is a note text of debt with id, as a write's reason.
func noteSQL(id int64, debt, actor string) string { return debtTextSQL(id, debt, "note", actor) }

func gapSQL(id, state string) string {
	return fmt.Sprintf(`INSERT INTO agentrt.accepted_gap (accepted_gap_id, project, gap_kind, title, reason, reopen_when, state)
	  VALUES ('%s', 'crew', 'data_boundary', 't', 'r', 'w', '%s')`, id, state)
}

func (dw *debtWorld) refToGap(t *testing.T, debt string, ref int64, gap string) {
	t.Helper()
	ok(t, dw.run(t, de(debt, "debt_ref_added", dw.crew.Actor, map[string]any{"debt_ref_id": ref, "ref_kind": "reclassified_as"}),
		fmt.Sprintf(`INSERT INTO agentrt.debt_ref (debt_ref_id, debt_id, ref_kind, accepted_gap_id, created_by_actor_id)
		  OVERRIDING SYSTEM VALUE VALUES (%d, '%s', 'reclassified_as', '%s', '%s')`, ref, debt, gap, dw.crew.Actor)))
}

// M-1: a plain state event closes as boundary only over this debt's own
// link to a gap the Owner accepted and still holds accepted (§6.2, T8); a
// move §6.2 does not list is refused; the runtime's automatic close needs
// its evidence (T12).
func TestDebtStateMovesNeedTheirEvidence(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	A, B := dw.mustFile(t, "stg", "a boundary"), dw.mustFile(t, "stg", "another")
	crew := dw.crew.Actor
	for _, g := range []struct{ id, state string }{{"AG-1", "accepted"}, {"AG-2", "accepted"}} {
		ok(t, dw.run(t, store.Event{Stream: "gap:" + g.id, Type: "gap_accepted", Actor: "owner", Body: map[string]any{"accepted_gap_id": g.id}},
			gapSQL(g.id, g.state)))
	}
	ok(t, dw.run(t, store.Event{Stream: "gap:AG-2", Type: "gap_reopened", Actor: crew, Body: map[string]any{"accepted_gap_id": "AG-2"}},
		`UPDATE agentrt.accepted_gap SET state = 'reopened', version = version + 1 WHERE accepted_gap_id = 'AG-2'`))
	dw.refToGap(t, B, 900201, "AG-1")
	boundary := func(text int64) error {
		return dw.run(t, de(A, "debt_state_changed", crew, map[string]any{"from": "open", "to": "closed", "conclusion": "boundary",
			"debt_text_ids": []any{text}}), noteSQL(text, A, crew), debtMoveSQL(A, "closed", "boundary"))
	}
	wantErr(t, boundary(900301), "accepted")
	dw.refToGap(t, A, 900202, "AG-2")
	wantErr(t, boundary(900302), "accepted")
	wantErr(t, dw.run(t, de(A, "debt_state_changed", crew, map[string]any{"from": "open", "to": "awaiting_signoff"}),
		debtMoveSQL(A, "awaiting_signoff", "")), "does not move")
	const W = "TDS-950"
	ok(t, dw.run(t, de(W, "debt_filed", "controller", map[string]any{"source": "import", "project": "crew", "priority": "P2",
		"state": "watching", "flow": "manual"}), fmt.Sprintf(`INSERT INTO agentrt.debt (debt_id, project, title, priority, state, flow,
		  watch_until, filed_at, state_since, created_by_actor_id) VALUES ('%s', 'crew', 'w', 'P2', 'watching', 'manual',
		  now() - interval '1 day', now(), now(), 'controller')`, W)))
	wantErr(t, dw.run(t, de(W, "debt_auto_closed", "controller", map[string]any{"from": "watching", "to": "closed"}),
		debtMoveSQL(W, "closed", "auto_closed")), "a debt worked by hand is not closed automatically")
	// The allowed one: its own link to a gap still accepted, with a reason.
	dw.refToGap(t, A, 900203, "AG-1")
	ok(t, boundary(900303))
}

// M-2: a review is bound to the spec as it is and to whoever really wrote
// it, never to what the event says (§2.2, §6.2): its author does not review
// it under another name, and a review of a spec since replaced does not
// stand.
func TestDebtSpecReviewIsBoundToTheRealSpec(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	f := debtFiling("a spec to review")
	f.Repos = []string{"bifrost-platform"}
	f.Acceptance = []any{map[string]any{"kind": "manual", "say": "look", "expect": "fine"}}
	out, err := store.FileDebt(ctx, dw.rt, "stg", dw.crew, dw.key(), f)
	ok(t, err)
	S := out["debt_id"].(string)
	const other = "thread:crew-2"
	dw.legacy(t, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ('thread:crew-2', 'thread', 'crew 2')`)
	review := func(actor, claimed, sum string) error {
		return dw.run(t, de(S, "debt_acceptance_reviewed", actor, map[string]any{"acceptance_sha256": sum, "spec_author": claimed}),
			fmt.Sprintf(`UPDATE agentrt.debt SET acceptance_reviewed_sha256 = '%s', acceptance_reviewed_by = '%s', version = version + 1
			  WHERE debt_id = '%s'`, sum, actor, S))
	}
	first := dw.specSHA(t, S)
	wantErr(t, review(dw.crew.Actor, other, first), "")
	// Its author, naming himself truly or naming nobody: the table's check
	// alone refuses it (review 631b R2-S1).
	wantErr(t, review(dw.crew.Actor, dw.crew.Actor, first), "check constraint")
	wantErr(t, dw.run(t, de(S, "debt_acceptance_reviewed", dw.crew.Actor, map[string]any{"acceptance_sha256": first}),
		fmt.Sprintf(`UPDATE agentrt.debt SET acceptance_reviewed_sha256 = '%s', acceptance_reviewed_by = '%s', version = version + 1
		  WHERE debt_id = '%s'`, first, dw.crew.Actor, S)), "check constraint")
	// The author moved by the review's own event, a spec changed or emptied
	// keeping the review: refused.
	wantErr(t, dw.run(t, de(S, "debt_acceptance_reviewed", dw.crew.Actor, map[string]any{"acceptance_sha256": first}),
		fmt.Sprintf(`UPDATE agentrt.debt SET acceptance_by = '%s', acceptance_reviewed_sha256 = '%s', acceptance_reviewed_by = '%s',
		  version = version + 1 WHERE debt_id = '%s'`, other, first, dw.crew.Actor, S)), "a spec is written by debt_acceptance_set")
	ok(t, review(other, dw.crew.Actor, first))
	for spec, by := range map[string]string{`[{"kind":"manual","say":"other","expect":"fine"}]`: "'" + other + "'", `[]`: "NULL"} {
		wantErr(t, dw.run(t, de(S, "debt_acceptance_set", other, map[string]any{"acceptance_sha256": first}),
			fmt.Sprintf(`UPDATE agentrt.debt SET acceptance_json = '%s', acceptance_by = %s, version = version + 1 WHERE debt_id = '%s'`,
				spec, by, S)), "which clears its review")
	}
	// Another writer replaces the spec; a review of the old one is refused.
	ok(t, dw.run(t, de(S, "debt_acceptance_set", other, map[string]any{"acceptance_sha256": first}),
		fmt.Sprintf(`UPDATE agentrt.debt SET acceptance_json = '[{"kind":"manual","say":"look again","expect":"fine"}]',
		  acceptance_by = '%s', acceptance_reviewed_sha256 = NULL, acceptance_reviewed_by = NULL, version = version + 1
		  WHERE debt_id = '%s'`, other, S)))
	wantErr(t, review(dw.crew.Actor, other, first), "")
	wantErr(t, review(other, other, dw.specSHA(t, S)), "")
	ok(t, review(dw.crew.Actor, other, dw.specSHA(t, S)))
}

// M-3: parts of one debt never wait on each other in a circle (§5.5: "段的
// 先后在一条债之内另查"), however many parts the circle goes through.
func TestDebtPartOrderNeverCycles(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	f := debtFiling("three parts")
	f.Repos = []string{"repo-a", "repo-b", "repo-c"}
	out, err := store.FileDebt(ctx, dw.rt, "stg", dw.crew, dw.key(), f)
	ok(t, err)
	D := out["debt_id"].(string)
	var parts []int64
	rows, err := dw.rt.Query(ctx, `SELECT debt_part_id FROM agentrt.debt_part WHERE debt_id = $1 ORDER BY ordinal`, D)
	ok(t, err)
	for rows.Next() {
		var p int64
		ok(t, rows.Scan(&p))
		parts = append(parts, p)
	}
	ok(t, rows.Err())
	order := func(pairs ...[2]int64) error {
		var ids []any
		var stmts []string
		for i, p := range pairs {
			id := int64(900400 + i)
			ids = append(ids, id)
			stmts = append(stmts, fmt.Sprintf(`INSERT INTO agentrt.debt_part_order (debt_part_order_id, debt_id, debt_part_id, after_debt_part_id)
			  OVERRIDING SYSTEM VALUE VALUES (%d, '%s', %d, %d)`, id, D, p[0], p[1]))
		}
		return dw.run(t, de(D, "debt_part_set", dw.crew.Actor, map[string]any{"debt_part_order_ids": ids}), stmts...)
	}
	p1, p2, p3 := parts[0], parts[1], parts[2]
	wantErr(t, order([2]int64{p1, p2}, [2]int64{p2, p1}), "cycle")
	wantErr(t, order([2]int64{p1, p2}, [2]int64{p2, p3}, [2]int64{p3, p1}), "cycle")
	// One statement of several rows sees its own earlier rows (review 631c
	// R3-S3).
	wantErr(t, dw.run(t, de(D, "debt_part_set", dw.crew.Actor, map[string]any{"debt_part_order_ids": []any{900410, 900411}}),
		fmt.Sprintf(`INSERT INTO agentrt.debt_part_order (debt_part_order_id, debt_id, debt_part_id, after_debt_part_id) OVERRIDING SYSTEM VALUE
		  VALUES (900410, '%s', %d, %d), (900411, '%s', %d, %d)`, D, p1, p2, D, p2, p1)), "cycle")
	ok(t, order([2]int64{p2, p1}, [2]int64{p3, p2}))
}

// M-4: a debt's live refs are unique per target, its kind included: a
// pending decision and an approval of the same number are two targets.
func TestDebtRefsAreUniquePerTypedTarget(t *testing.T) {
	t.Parallel()
	dw := newDebtWorld(t, true)
	A := dw.mustFile(t, "stg", "waits twice")
	var decision int64
	ok(t, dw.rt.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('agentrt.debt_decision', 'debt_decision_id'))`).Scan(&decision))
	ok(t, dw.run(t, store.Event{Stream: "debt-decisions", Type: "debt_decision_set", Actor: dw.crew.Actor,
		Body: map[string]any{"debt_decision_id": decision, "state": "pending"}}, fmt.Sprintf(`INSERT INTO agentrt.debt_decision
		  (debt_decision_id, title, body_text, state) OVERRIDING SYSTEM VALUE VALUES (%d, 't', 'b', 'pending')`, decision)))
	wait := func(ref int64, target string) error {
		return dw.run(t, de(A, "debt_ref_added", dw.crew.Actor, map[string]any{"debt_ref_id": ref, "ref_kind": "waits_on_owner"}),
			fmt.Sprintf(`INSERT INTO agentrt.debt_ref (debt_ref_id, debt_id, ref_kind, covers, %s, created_by_actor_id)
			  OVERRIDING SYSTEM VALUE VALUES (%d, '%s', 'waits_on_owner', 'start', %s, '%s')`,
				map[bool]string{true: "debt_decision_id, go_options", false: "approval_no"}[target == "decision"], ref, A,
				map[bool]string{true: fmt.Sprintf("%d, ARRAY['go']", decision), false: fmt.Sprint(decision)}[target == "decision"],
				dw.crew.Actor))
	}
	ok(t, wait(900501, "decision"))
	ok(t, wait(900502, "approval"))
	wantErr(t, wait(900503, "approval"), "debt_ref_live")
}
