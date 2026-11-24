package planstore_test

import (
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// 2026-11-24, closed loop standard 2 (the Owner reads on a page how many
// standards of a law are reached, missed and unmeasured; the audit line's
// fold: 部分 and 没达到 are missed, 没核 and no conclusion unmeasured). An
// approved add becomes a law with an item; the audit line's conclusion notes
// on its steps are counted by their latest line; a later line replaces an
// earlier one; a note that is not a conclusion counts for nothing.
func TestLawsViewCountsConclusions(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	rows, err := planstore.ParseLawTable(lawTable)
	ok(t, err)
	_, err = planstore.TranscribeLaws(ctx, w.rt, rows, "OWNER_LAW.md@a")
	ok(t, err)
	id := "appr_00000000000000c1"
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "card:" + id, Type: "approval.card.executed", Actor: "controller",
			Body: map[string]any{"id": id, "card": "#201", "subject": "#201", "key": "#201", "number": 201, "action": "law_amend",
				"status": "executed", "ref": "/api/v1/approvals/" + id, "change": "executed"}})
		return err
	}))
	var std []planstore.LawStandard
	for i := 1; i <= 4; i++ {
		std = append(std, planstore.LawStandard{ID: fmt.Sprintf("s%d", i), Standard: "standard", Measure: "measure"})
	}
	_, err = planstore.RecordLaws(ctx, w.rt, []planstore.LawCard{{ApprovalID: id, Number: 201, Op: "add", After: "a law", Standards: std}})
	ok(t, err)
	view, err := planstore.LawsView(ctx, w.rt)
	ok(t, err)
	var law planstore.LawView
	for _, v := range view {
		if v.ApprovalID == id {
			law = v
		}
	}
	if law.Law.Law != "3" || law.Item == "" || len(law.Steps) != 4 || law.Counts == nil || law.Counts.Unmeasured != 4 {
		t.Fatalf("law before any conclusion = %+v", law)
	}
	audit := actor.Writer{Actor: "thread:audit", Token: "fixture-operator-1"}
	post := func(key, step, text string) {
		_, err := planstore.PostPlanNote(ctx, w.rt, audit, key, step, text)
		ok(t, err)
	}
	post("a1", law.Steps[0].Node, "结论 s1：没达到｜标准版本 "+id+"｜读数：x @2026-11-24T02:10Z")
	post("a2", law.Steps[0].Node, "结论 s1：达到｜标准版本 "+id+"｜读数：y @2026-11-24T05:00Z")
	post("a3", law.Steps[1].Node, "结论 s2：没核｜标准版本 "+id)
	post("a3b", law.Steps[1].Node, "结论 s2（更正，替换 02:10Z 那条）：部分｜标准版本 "+id)
	post("a3c", law.Steps[1].Node, "结论 s2（补读数，结论不变：部分）｜标准版本 "+id)
	post("a4", law.Steps[2].Node, "结论 s3：没核｜标准版本 "+id)
	post("a5", law.Steps[3].Node, "归因 s4：类别 工具｜原因 none｜去向 修")
	view, err = planstore.LawsView(ctx, w.rt)
	ok(t, err)
	for _, v := range view {
		if v.ApprovalID == id {
			law = v
		}
	}
	if c := law.Counts; c.Reached != 1 || c.Missed != 1 || c.Unmeasured != 2 {
		t.Fatalf("counts = %+v; want 1 reached, 1 missed, 2 unmeasured", c)
	}
	if law.Steps[0].Grade != "达到" || law.Steps[0].Version != id || law.Steps[3].Grade != "" {
		t.Fatalf("steps = %+v", law.Steps)
	}
	for _, v := range view {
		if v.Law.Law == "1" && (v.Item != "" || v.Counts != nil) {
			t.Fatalf("a transcribed law has an item: %+v", v)
		}
	}
	w.chainHolds(t, planstore.PlanStream(root))
}
