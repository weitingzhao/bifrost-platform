package store_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The PROD reads, against the database: an issue whose latest
// classification is a fault leaks at the module (item) of its work's task; a
// fault with no work taken is unattributed; another category is no leak.
func TestLeakValueReadsTheFaults(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	taken := iw.submit(t, "the page crashes")
	iw.readySmall(t, taken, "fault")
	out, err := iw.take(t, taken, fitting)
	ok(t, err)
	mission, _ := out["mission_id"].(string)
	for _, n := range []store.PlanNode{
		{ID: "R9", Kind: "round", Label: "9", Title: "round", Ordinal: 90, Root: "R9"},
		{ID: "R9:1", Parent: "R9", Kind: "item", Label: "api", Title: "Agent runtime API", Root: "R9"},
		{ID: "R9:1-1", Parent: "R9:1", Kind: "step", Label: "1-1", Title: "fix", MissionID: mission, StepKind: "agent", Root: "R9"},
	} {
		ok(t, store.InTx(ctx, iw.rt, func(tx pgx.Tx) error {
			_, err := store.CreatePlanNode(ctx, tx, "controller", n)
			return err
		}))
	}
	iw.proposed(t, store.Task{MissionID: mission, PlanNode: "R9:1-1"})

	untaken := iw.submit(t, "the totals are wrong")
	_, err = iw.answer(t, iw.dots, untaken, iw.brief(t, untaken, "brief of "+untaken), "confirmed")
	ok(t, err)
	ok(t, iw.classify(t, untaken, "small", "fault"))
	other := iw.submit(t, "how do I export")
	_, err = iw.answer(t, iw.dots, other, iw.brief(t, other, "brief of "+other), "confirmed")
	ok(t, err)
	ok(t, iw.classify(t, other, "small", "guidance"))

	doc, err := store.LeakValue(ctx, iw.rt, time.Time{}, 1)
	ok(t, err)
	if len(doc.Rows) != 2 || doc.Rows[0].Place != "R9:1" || doc.Rows[1].Place != store.LeakValueUnattributed {
		t.Fatalf("rows %+v", doc.Rows)
	}
	row := doc.Rows[0]
	current := row.Cells[len(row.Cells)-1]
	if row.Label != "api Agent runtime API" || current.PROD != 1 || current.STG != 0 || current.Week != store.LeakValueWeek(time.Now()) {
		t.Fatalf("R9:1 %+v", row)
	}
	if un := doc.Rows[1].Cells[len(doc.Rows[1].Cells)-1]; un.PROD != 1 {
		t.Fatalf("unattributed %+v", doc.Rows[1])
	}
}
