package planstore_test

import (
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	// The card events' vocabulary (approval.card.executed), as the server
	// registers it.
	_ "github.com/weitingzhao/bifrost-platform/api/internal/bus/cards"
)

// 2026-11-23, the closed loop (Owner: only a change of law opens a ticket;
// the plan owns the loop): an executed law_amend card becomes one item of
// the newest round with a step per standard, its plan_changed events name
// the card's executed event, it is recorded once however often a pass finds
// it, and a card the ledger has not announced yet waits.
func TestRecordLawsOncePerCard(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	card := planstore.LawCard{ApprovalID: "appr_00000000000000a1", Number: 91, Op: "amend", Law: "29", Before: "old", After: "new text",
		Standards: []planstore.LawStandard{{ID: "s1", Standard: "zero by hand", Measure: "Audit counts"}, {ID: "s2", Standard: "page shows 3 numbers", Measure: "viewer opens Console"}}}
	early := planstore.LawCard{ApprovalID: "appr_00000000000000a2", Number: 92, Op: "add", After: "later"}

	var executed int64
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		ev, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "card:appr_00000000000000a1", Type: "approval.card.executed", Actor: "controller",
			Body: map[string]any{"id": "appr_00000000000000a1", "card": "#91", "subject": "#91", "key": "#91", "number": 91, "action": "law_amend",
				"status": "executed", "ref": "/api/v1/approvals/appr_00000000000000a1", "change": "executed"}})
		executed = ev.EventID
		return err
	}))
	for range 2 {
		n, err := planstore.RecordLaws(ctx, w.rt, []planstore.LawCard{card, early})
		ok(t, err)
		_ = n
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.plan_node WHERE node_kind = 'item' AND title = $1`, planstore.LawTitle(card)); n != 1 {
		t.Fatalf("%d items for the card, want 1", n)
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.plan_node s JOIN agentrt.plan_node i ON s.parent_plan_node_id = i.plan_node_id
	  WHERE i.title = $1 AND s.node_kind = 'step' AND s.state = 'planned' AND s.done_rule_json = '{}'::jsonb`, planstore.LawTitle(card)); n != 2 {
		t.Fatalf("%d unmeasured steps, want 2", n)
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'plan_changed' AND body_json->>'note' = 'law_amend appr_00000000000000a1'
	  AND (body_json->>'decided_event_id')::bigint = $1`, executed); n != 2 {
		t.Fatalf("%d noted plan_changed events naming the card, want 2", n)
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.event WHERE body_json->>'note' = 'law_amend appr_00000000000000a2'`); n != 0 {
		t.Fatal("a card the ledger has not announced was recorded")
	}
	w.chainHolds(t, planstore.PlanStream(root))
}
