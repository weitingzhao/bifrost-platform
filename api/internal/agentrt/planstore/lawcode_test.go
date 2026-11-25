package planstore_test

import (
	"context"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// Origin: 2026-11-25, Owner: 「以后法条统一叫LAW 开始，不用简写」. The laws the
// framework holds (86 on) are LAW-1, LAW-2, …; rows 1 to 85 have no code. A card may name a law by its code or its number.
func TestLawCode(t *testing.T) {
	for n, want := range map[string]string{"86": "LAW-1", "89": "CORE-1", "98": "LAW-13", "85": "", "1": "", "x": ""} {
		if got := planstore.LawCode(n); got != want {
			t.Fatalf("LawCode(%s) = %q, want %q", n, got, want)
		}
	}
	for ref, want := range map[string]string{"LAW-1": "86", "LAW-13": "98", " LAW-2 ": "87", "CORE-1": "89", "90": "90", "LAW-14": "LAW-14", "DAO-1": "DAO-1", "LAW-0": "LAW-0", "law-1": "law-1", "L5": "L5"} {
		if got := planstore.LawNumberOf(ref); got != want {
			t.Fatalf("LawNumberOf(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestLawCardTakesCodes(t *testing.T) {
	reg := fakeLookup("99")
	ctx := context.Background()
	// fakeLookup's law 90 in force is LAW-5: an amend by code reads as an
	// amend of 90, and the card says LAW-5.
	byCode := map[string]any{"op": "amend", "law": "LAW-5", "before": "approvals go by law", "after": "approvals go by law, amended", "revert": "r",
		"standards": []any{map[string]any{"id": "s1", "standard": "x", "measure": "y"}}}
	la, err := planstore.ReadLawAmend(byCode)
	ok(t, err)
	if la.Law != "90" {
		t.Fatalf("law read as %q, want 90", la.Law)
	}
	ok(t, planstore.CheckLawCard(ctx, reg, byCode))
	if got := planstore.LawCardSummary(ctx, reg, byCode); !strings.HasPrefix(got, "Law amend LAW-5") {
		t.Fatalf("summary = %q", got)
	}
	add := map[string]any{"op": "add", "after": "x", "revert": "r", "standards": []any{map[string]any{"id": "s1", "standard": "x", "measure": "y"}},
		"frame": map[string]any{"level": 3, "parent": "LAW-5"}}
	la, err = planstore.ReadLawAmend(add)
	ok(t, err)
	if la.Frame.Parent != "90" {
		t.Fatalf("parent read as %q, want 90", la.Frame.Parent)
	}
	if got := planstore.LawCardSummary(ctx, reg, add); !strings.HasPrefix(got, "Law add: will become ZONE-1") {
		t.Fatalf("summary = %q", got)
	}
}

// Origin: 2026-11-25, the first amend of LAW-1 after round 1's archive
// carried absorbs naming repealed rows. Since LAW-1 四 / ISSUE-104 absorbs is
// not read at all: the card is not refused and not noted for it. A repealed
// law as a parent is still refused (the positive control).
func TestAbsorbsIsNotRead(t *testing.T) {
	reg := fakeLookup("99")
	ctx := context.Background()
	std := []any{map[string]any{"id": "s1", "standard": "x", "measure": "y"}}
	keeps := map[string]any{"op": "amend", "law": "90", "before": "approvals go by law", "after": "approvals go by law, again", "revert": "r", "standards": std,
		"frame": map[string]any{"level": 2, "absorbs": []any{"5"}}}
	ok(t, planstore.CheckLawCard(ctx, reg, keeps))
	if got := planstore.LawCardSummary(ctx, reg, keeps); strings.Contains(got, "吸收") {
		t.Fatalf("an archived row in absorbs is noted: %q", got)
	}
	parent := map[string]any{"op": "amend", "law": "90", "before": "approvals go by law", "after": "x", "revert": "r", "standards": std,
		"frame": map[string]any{"level": 3, "parent": "5"}}
	if err := planstore.CheckLawCard(ctx, reg, parent); err == nil {
		t.Fatal("a repealed law as parent was accepted")
	}
}

// Origin: 2026-11-25, Owner: 「每一层都用特定的单词表示，同时采用特定的编号体系」.
// A new law is coded by its level, each level counting on its own (DAO,
// CORE, LAW, ZONE), the code kept in its record; an amend keeps the
// code and may not change the level; the codes before (LAW-1…13) stay.
func TestLevelCodes(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	rows, err := planstore.ParseLawTable(lawTable)
	ok(t, err)
	_, err = planstore.TranscribeLaws(ctx, w.rt, rows, "OWNER_LAW.md@a")
	ok(t, err)
	lv := func(n int) *int { return &n }
	std := []planstore.LawStandard{{ID: "s1", Standard: "x", Measure: "y"}}
	batch := planstore.LawCard{ApprovalID: "appr_00000000000000a1", Number: 911, Op: "add_many", Adds: []planstore.LawCard{
		{Op: "add", After: "a belief", Standards: std, Frame: &planstore.LawFrame{Level: lv(0)}},
		{Op: "add", After: "a basic law", Standards: std, Frame: &planstore.LawFrame{Level: lv(2)}},
		{Op: "add", After: "a domain law", Standards: std, Frame: &planstore.LawFrame{Level: lv(3), Parent: "4"}},
		{Op: "add", After: "another belief", Standards: std, Frame: &planstore.LawFrame{Level: lv(0)}}}}
	_, err = planstore.RecordLaws(ctx, w.rt, []planstore.LawCard{batch})
	ok(t, err)
	for n, want := range map[string]string{"3": "DAO-1", "4": "LAW-1", "5": "ZONE-1", "6": "DAO-2"} {
		l, _, _ := planstore.CurrentLaw(ctx, w.rt, n)
		if l.Code != want {
			t.Fatalf("law %s code = %q, want %q", n, l.Code, want)
		}
	}
	// An amend keeps the code; one that changes the level is refused.
	keep := planstore.LawCard{ApprovalID: "appr_00000000000000a2", Number: 912, Op: "amend", Law: "4", Before: "a basic law", After: "a basic law, amended",
		Standards: std, Frame: &planstore.LawFrame{Level: lv(2)}}
	_, err = planstore.RecordLaws(ctx, w.rt, []planstore.LawCard{keep})
	ok(t, err)
	if l, _, _ := planstore.CurrentLaw(ctx, w.rt, "4"); l.Code != "LAW-1" || l.Version != 2 {
		t.Fatalf("law 4 after the amend = %+v", l)
	}
	move := planstore.LawCard{ApprovalID: "appr_00000000000000a3", Number: 913, Op: "amend", Law: "4", Before: "a basic law, amended", After: "now a domain law",
		Standards: std, Frame: &planstore.LawFrame{Level: lv(3)}}
	if _, err := planstore.RecordLaws(ctx, w.rt, []planstore.LawCard{move}); err == nil {
		t.Fatal("an amend that changes the level was written")
	}
	if l, _, _ := planstore.CurrentLaw(ctx, w.rt, "4"); l.Version != 2 {
		t.Fatalf("law 4 moved level: %+v", l)
	}
}
