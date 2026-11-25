package planstore_test

import (
	"context"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// Origin: 2026-11-25, Owner: 「以后法条统一叫LAW 开始，不用简写」. The laws the
// framework holds (86 on) are LAW-1, LAW-2, …; rows 1 to 85 were material and
// have no code. A card may name a law by its code or its number.
func TestLawCode(t *testing.T) {
	for n, want := range map[string]string{"86": "LAW-1", "98": "LAW-13", "99": "LAW-14", "85": "", "1": "", "x": ""} {
		if got := planstore.LawCode(n); got != want {
			t.Fatalf("LawCode(%s) = %q, want %q", n, got, want)
		}
	}
	for ref, want := range map[string]string{"LAW-1": "86", "LAW-13": "98", " LAW-2 ": "87", "90": "90", "LAW-0": "LAW-0", "law-1": "law-1", "L5": "L5"} {
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
	if got := planstore.LawCardSummary(ctx, reg, add); !strings.HasPrefix(got, "Law add: will become LAW-14") {
		t.Fatalf("summary = %q", got)
	}
}

// Origin: 2026-11-25, the first amend of LAW-1 after round 1's archive: its
// frame still names the material it absorbed (now repealed), which is its
// provenance, not a citation; the card is not refused for it. A repealed law
// as a parent is still refused (the positive control).
func TestAbsorbedArchivedMaterialIsNotACitation(t *testing.T) {
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
