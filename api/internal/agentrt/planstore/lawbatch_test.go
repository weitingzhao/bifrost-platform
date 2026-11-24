package planstore_test

import (
	"context"
	"strings"
	"testing"

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
	if got := planstore.LawCardSummary(ctx, reg, good); !strings.HasPrefix(got, "Law add ×2: will become laws 92–93") {
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
