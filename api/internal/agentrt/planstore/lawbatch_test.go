package planstore_test

import (
	"context"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// Origin: 2026-11-24, legislation thread: the first laws go into the
// framework as one batch, one card for the batch, not one per law.
// 2026-11-25, LAW-1 四 / ISSUE-104: the register holds laws only; the archive
// op left with the third status (LAW-1 四); only add_many is tested here.

func batchFrame() map[string]any {
	return map[string]any{"level": 2, "scope": "全系统", "subject": "立法", "modality": "必须"}
}

func TestLawCardAddManyRead(t *testing.T) {
	reg := fakeLookup("92")
	ctx := context.Background()
	std := []any{map[string]any{"id": "s1", "standard": "x", "measure": "y"}}
	good := map[string]any{"op": "add_many", "revert": "repeal them",
		"adds": []any{map[string]any{"after": "law one", "standards": std, "frame": batchFrame()},
			map[string]any{"after": "law two", "standards": std, "frame": batchFrame()}}}
	if err := planstore.CheckLawCard(ctx, reg, good); err != nil {
		t.Fatalf("good batch refused: %v", err)
	}
	if got := planstore.LawCardSummary(ctx, reg, good); !strings.HasPrefix(got, "Law add ×2: will become LAW-7, LAW-8") {
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
	// 2026-11-25, LAW-1 四 / ISSUE-104: archive is no longer an op (the good
	// batch above is the positive control).
	arch := map[string]any{"op": "archive", "revert": "add it back", "repeals": []any{map[string]any{"law": "79", "reason": "covered"}}}
	if err := planstore.CheckLawCard(ctx, reg, arch); err == nil || !strings.Contains(err.Error(), "op must be") {
		t.Fatalf("an archive card: %v", err)
	}
}

// In the register: a batch takes the next numbers in order, once.
func TestLawAddMany(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	rows, err := planstore.ParseLawTable(lawTable)
	ok(t, err)
	_, err = planstore.TranscribeLaws(ctx, w.rt, rows, "OWNER_LAW.md@a")
	ok(t, err)
	lv := 2
	std := []planstore.LawStandard{{ID: "s1", Standard: "x", Measure: "y"}}
	batch := planstore.LawCard{ApprovalID: "appr_00000000000000d1", Number: 701, Op: "add_many", Adds: []planstore.LawCard{
		{Op: "add", After: "law three", Standards: std, Frame: &planstore.LawFrame{Level: &lv, Scope: "全系统", Subject: "立法", Modality: "必须"}},
		{Op: "add", After: "law four", Standards: std, Frame: &planstore.LawFrame{Level: &lv, Scope: "全系统", Subject: "审批", Modality: "必须"}}}}
	for range 2 {
		_, err = planstore.RecordLaws(ctx, w.rt, []planstore.LawCard{batch})
		ok(t, err)
	}
	for n, text := range map[string]string{"3": "law three", "4": "law four"} {
		l, have, err := planstore.CurrentLaw(ctx, w.rt, n)
		ok(t, err)
		if !have || l.Text != text || l.Op != "add" || l.ApprovalID != batch.ApprovalID || l.Version != 1 {
			t.Fatalf("law %s = %+v, %v", n, l, have)
		}
	}
	if next, _ := planstore.NextLawNumber(ctx, w.rt); next != "5" {
		t.Fatalf("next after the batch = %q, want 5 (written once)", next)
	}
	w.chainHolds(t, planstore.LawStream("3"))
}

// Origin: 2026-11-24, the audit line checked the first batch and asked for
// changes to seven laws at once; one card amends them
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
		return &planstore.LawFrame{Level: &lv, Scope: "全系统", Subject: "立法", Modality: "必须"}
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
	if got := planstore.LawCardSummary(ctx, reg, good); !strings.HasPrefix(got, "Law amend ×2: LAW-1, LAW-2") {
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
