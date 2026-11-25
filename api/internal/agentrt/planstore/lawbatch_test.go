package planstore_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// Origin: 2026-11-24, Owner in the legislation thread: the laws distilled
// from the 69 rows of material go into the framework as one batch; once they
// cover the material, the material is archived (repealed, then deleted after
// seven days, its words kept in OWNER_LAW.md's history). One card for the
// batch, one for the archive, not one per law.

func batchFrame(absorbs ...any) map[string]any {
	return map[string]any{"level": 2, "scope": "全系统", "subject": "立法", "modality": "必须", "absorbs": absorbs}
}

func TestLawCardAddManyAndArchiveRead(t *testing.T) {
	reg := fakeLookup("92")
	ctx := context.Background()
	std := []any{map[string]any{"id": "s1", "standard": "x", "measure": "y"}}
	good := map[string]any{"op": "add_many", "revert": "repeal them",
		"adds": []any{map[string]any{"after": "law one", "standards": std, "frame": batchFrame("79")},
			map[string]any{"after": "law two", "standards": std, "frame": batchFrame()}}}
	if err := planstore.CheckLawCard(ctx, reg, good); err != nil {
		t.Fatalf("good batch refused: %v", err)
	}
	if got := planstore.LawCardSummary(ctx, reg, good); !strings.HasPrefix(got, "Law add ×2: will become LAW-7–LAW-8") {
		t.Fatalf("summary = %q", got)
	}
	enforced := batchFrame()
	enforced["enforce"] = planstore.EnforceOn
	bad := map[string]map[string]any{
		"empty":     {"op": "add_many", "revert": "r", "adds": []any{}},
		"top after": {"op": "add_many", "revert": "r", "after": "x", "adds": good["adds"]},
		"no std":    {"op": "add_many", "revert": "r", "adds": []any{map[string]any{"after": "x"}}},
		"enforced":  {"op": "add_many", "revert": "r", "adds": []any{map[string]any{"after": "x", "standards": std, "frame": enforced}}},
		"no revert": {"op": "add_many", "adds": good["adds"]},
	}
	for name, p := range bad {
		if err := planstore.CheckLawCard(ctx, reg, p); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	// Archive: only material that a law in force absorbs. In fakeLookup
	// nothing absorbs 79 yet, so archiving it is refused.
	arch := map[string]any{"op": "archive", "revert": "add it back", "repeals": []any{map[string]any{"law": "79", "reason": "covered"}}}
	if err := planstore.CheckLawCard(ctx, reg, arch); err == nil || !strings.Contains(err.Error(), "absorbs") {
		t.Fatalf("archive of material no law absorbs: %v", err)
	}
	notMaterial := map[string]any{"op": "archive", "revert": "r", "repeals": []any{map[string]any{"law": "90", "reason": "x"}}}
	if err := planstore.CheckLawCard(ctx, reg, notMaterial); err == nil {
		t.Fatal("archive of a law in force accepted")
	}
}

// In the register: a batch takes the next numbers in order, once; an archive
// repeals only material absorbed by a law in force, and says which.
func TestLawAddManyAndArchive(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	rows, err := planstore.ParseLawTable(lawTable)
	ok(t, err)
	_, err = planstore.TranscribeLaws(ctx, w.rt, rows, "OWNER_LAW.md@a")
	ok(t, err)
	lv := 2
	std := []planstore.LawStandard{{ID: "s1", Standard: "x", Measure: "y"}}
	batch := planstore.LawCard{ApprovalID: "appr_00000000000000d1", Number: 701, Op: "add_many", Adds: []planstore.LawCard{
		{Op: "add", After: "absorbs law 1", Standards: std, Frame: &planstore.LawFrame{Level: &lv, Scope: "全系统", Subject: "立法", Modality: "必须", Absorbs: []string{"1"}}},
		{Op: "add", After: "absorbs nothing", Standards: std, Frame: &planstore.LawFrame{Level: &lv, Scope: "全系统", Subject: "审批", Modality: "必须"}}}}
	for range 2 {
		_, err = planstore.RecordLaws(ctx, w.rt, []planstore.LawCard{batch})
		ok(t, err)
	}
	for n, text := range map[string]string{"3": "absorbs law 1", "4": "absorbs nothing"} {
		l, have, err := planstore.CurrentLaw(ctx, w.rt, n)
		ok(t, err)
		if !have || l.Text != text || l.Op != "add" || l.ApprovalID != batch.ApprovalID || l.Version != 1 {
			t.Fatalf("law %s = %+v, %v", n, l, have)
		}
	}
	if next, _ := planstore.NextLawNumber(ctx, w.rt); next != "5" {
		t.Fatalf("next after the batch = %q, want 5 (written once)", next)
	}
	reg := planstore.DBLawLookup(func() (eventlog.DB, error) { return w.rt, nil })
	refused := map[string]any{"op": "archive", "revert": "r", "repeals": []any{map[string]any{"law": "1", "reason": "covered"}, map[string]any{"law": "2", "reason": "covered"}}}
	if err := planstore.CheckLawCard(ctx, reg, refused); err == nil {
		t.Fatal("archive including material 2, which no law absorbs, accepted")
	}
	good := map[string]any{"op": "archive", "revert": "r", "repeals": []any{map[string]any{"law": "1", "reason": "covered"}}}
	ok(t, planstore.CheckLawCard(ctx, reg, good))
	arch := planstore.LawCard{ApprovalID: "appr_00000000000000d2", Number: 702, Op: "archive",
		Repeals: []planstore.LawRepeal{{Law: "1", Reason: "covered"}, {Law: "2", Reason: "not covered, must stay"}}}
	_, err = planstore.RecordLaws(ctx, w.rt, []planstore.LawCard{arch})
	ok(t, err)
	l1, _, _ := planstore.CurrentLaw(ctx, w.rt, "1")
	if l1.Status != planstore.RepealedStatus || !strings.Contains(l1.Source, "被第 3 条吸收") {
		t.Fatalf("material 1 after the archive = %+v", l1)
	}
	if l2, _, _ := planstore.CurrentLaw(ctx, w.rt, "2"); l2.Status == planstore.RepealedStatus {
		t.Fatal("the archive repealed material 2, which no law absorbs")
	}
	w.chainHolds(t, planstore.LawStream("1"))
}

// Material that came in by a card before the framework (laws 83 to 85 in
// PROD) has a plan item; archiving it drops the item's live steps, as a
// repeal does, so the archived material is no longer measured.
func TestArchivedMaterialLeavesThePlan(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	rows, err := planstore.ParseLawTable(lawTable)
	ok(t, err)
	_, err = planstore.TranscribeLaws(ctx, w.rt, rows, "OWNER_LAW.md@a")
	ok(t, err)
	announce := func(id string, n int) {
		ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "card:" + id, Type: "approval.card.executed", Actor: "controller",
				Body: map[string]any{"id": id, "card": fmt.Sprintf("#%d", n), "subject": fmt.Sprintf("#%d", n), "key": fmt.Sprintf("#%d", n), "number": n,
					"action": "law_amend", "status": "executed", "ref": "/api/v1/approvals/" + id, "change": "executed"}})
			return err
		}))
	}
	old := planstore.LawCard{ApprovalID: "appr_00000000000000c1", Number: 801, Op: "add", After: "a law from before the framework",
		Standards: []planstore.LawStandard{{ID: "s1", Standard: "x", Measure: "y"}}}
	announce(old.ApprovalID, 801)
	_, err = planstore.RecordLaws(ctx, w.rt, []planstore.LawCard{old})
	ok(t, err)
	live := `SELECT count(*) FROM agentrt.plan_node s JOIN agentrt.plan_node i ON i.plan_node_id = s.parent_plan_node_id
	  WHERE strpos(i.title, $1) > 0 AND s.node_kind = 'step' AND s.state IN ('planned', 'active')`
	if n := w.count(t, live, old.ApprovalID); n != 1 {
		t.Fatalf("the old law has %d live steps, want 1", n)
	}
	lv := 2
	batch := planstore.LawCard{ApprovalID: "appr_00000000000000c2", Number: 802, Op: "add_many", Adds: []planstore.LawCard{
		{Op: "add", After: "absorbs law 3", Standards: old.Standards, Frame: &planstore.LawFrame{Level: &lv, Absorbs: []string{"3"}}}}}
	arch := planstore.LawCard{ApprovalID: "appr_00000000000000c3", Number: 803, Op: "archive", Repeals: []planstore.LawRepeal{{Law: "3", Reason: "covered by 4"}}}
	announce(batch.ApprovalID, 802)
	announce(arch.ApprovalID, 803)
	_, err = planstore.RecordLaws(ctx, w.rt, []planstore.LawCard{batch, arch})
	ok(t, err)
	if l, _, _ := planstore.CurrentLaw(ctx, w.rt, "3"); l.Status != planstore.RepealedStatus {
		t.Fatalf("law 3 after the archive = %+v", l)
	}
	if n := w.count(t, live, old.ApprovalID); n != 0 {
		t.Fatalf("archived material still has %d live steps", n)
	}
}

// Origin: 2026-11-24, the audit line checked the first batch against the
// material and asked for changes to seven laws at once; one card amends them
// all, each checked as an amend card (in the register, quoted as it stands,
// enforcement not turned on), written once.
func TestLawAmendMany(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	rows, err := planstore.ParseLawTable(lawTable)
	ok(t, err)
	_, err = planstore.TranscribeLaws(ctx, w.rt, rows, "OWNER_LAW.md@a")
	ok(t, err)
	lv := 2
	std := []planstore.LawStandard{{ID: "s1", Standard: "x", Measure: "y"}}
	frame := func() *planstore.LawFrame {
		return &planstore.LawFrame{Level: &lv, Scope: "全系统", Subject: "立法", Modality: "必须", Absorbs: []string{"1"}}
	}
	batch := planstore.LawCard{ApprovalID: "appr_00000000000000b1", Number: 901, Op: "add_many", Adds: []planstore.LawCard{
		{Op: "add", After: "law three", Standards: std, Frame: frame()}, {Op: "add", After: "law four", Standards: std, Frame: frame()}}}
	_, err = planstore.RecordLaws(ctx, w.rt, []planstore.LawCard{batch})
	ok(t, err)
	reg := planstore.DBLawLookup(func() (eventlog.DB, error) { return w.rt, nil })
	stdAny := []any{map[string]any{"id": "s1", "standard": "x", "measure": "y"}}
	good := map[string]any{"op": "amend_many", "revert": "amend back", "amends": []any{
		map[string]any{"law": "3", "before": "law three", "after": "law three, amended", "standards": stdAny},
		map[string]any{"law": "4", "before": "law four", "after": "law four, amended", "standards": stdAny}}}
	ok(t, planstore.CheckLawCard(ctx, reg, good))
	if got := planstore.LawCardSummary(ctx, reg, good); !strings.HasPrefix(got, "Law amend ×2: 第 3 条, 第 4 条") {
		t.Fatalf("summary = %q", got)
	}
	bad := map[string]map[string]any{
		"stale before": {"op": "amend_many", "revert": "r", "amends": []any{map[string]any{"law": "3", "before": "old", "after": "x", "standards": stdAny}}},
		"twice": {"op": "amend_many", "revert": "r", "amends": []any{map[string]any{"law": "3", "before": "law three", "after": "x", "standards": stdAny},
			map[string]any{"law": "3", "before": "law three", "after": "y", "standards": stdAny}}},
		"enforce": {"op": "amend_many", "revert": "r", "amends": []any{map[string]any{"law": "3", "before": "law three", "after": "x", "standards": stdAny,
			"frame": map[string]any{"level": 2, "enforce": planstore.EnforceOn}}}},
		"missing law": {"op": "amend_many", "revert": "r", "amends": []any{map[string]any{"law": "99", "before": "z", "after": "x", "standards": stdAny}}},
	}
	for name, p := range bad {
		if err := planstore.CheckLawCard(ctx, reg, p); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	amend := planstore.LawCard{ApprovalID: "appr_00000000000000b2", Number: 902, Op: "amend_many", Amends: []planstore.LawCard{
		{Op: "amend", Law: "3", Before: "law three", After: "law three, amended", Standards: std, Frame: frame()},
		{Op: "amend", Law: "4", Before: "law four", After: "law four, amended", Standards: std, Frame: frame()}}}
	for range 2 {
		_, err = planstore.RecordLaws(ctx, w.rt, []planstore.LawCard{amend})
		ok(t, err)
	}
	for _, n := range []string{"3", "4"} {
		l, _, _ := planstore.CurrentLaw(ctx, w.rt, n)
		if l.Version != 2 || !strings.HasSuffix(l.Text, ", amended") || l.ApprovalID != amend.ApprovalID || l.Op != "amend" {
			t.Fatalf("law %s after the amend = %+v", n, l)
		}
		w.chainHolds(t, planstore.LawStream(n))
	}
}
