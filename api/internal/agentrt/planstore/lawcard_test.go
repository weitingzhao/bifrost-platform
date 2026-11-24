package planstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// Moved from actions/law_amend_test.go on 2026-11-24 with the rules
// (legislation and administration agreed the split). 2026-11-23, Owner: only
// a change of law opens a ticket; it names add/amend/repeal and measurable
// standards; it changes a real record of the law in the register, never
// stands alone. fakeLookup holds law 79 in force ("a"), law 5 repealed.
func fakeLookup(next string) planstore.LawLookup {
	lv := func(n int) *int { return &n }
	laws := []planstore.Law{
		{Law: "5", Text: "old", Status: planstore.RepealedStatus, Op: "repeal"},
		{Law: "79", Text: "a\n", Status: "已落", Op: "transcribed"},
		{Law: "90", Text: "approvals go by law", Status: "已批", Op: "add",
			Frame: &planstore.LawFrame{Level: lv(2), Scope: "全系统", Subject: "审批", Modality: "必须"}},
		{Law: "91", Text: "the six boundaries", Status: "已批", Op: "add",
			Frame: &planstore.LawFrame{Level: lv(1), Scope: "全系统", Subject: "安全", Modality: "禁止"}},
	}
	return planstore.LawLookup{
		Current: func(_ context.Context, n string) (planstore.Law, bool, error) {
			for _, l := range laws {
				if l.Law == n {
					return l, true, nil
				}
			}
			return planstore.Law{}, false, nil
		},
		Next: func(context.Context) (string, error) { return next, nil },
		All:  func(context.Context) ([]planstore.Law, error) { return laws, nil },
	}
}

// frame is a complete level-n frame on subject 计划 (no law in force there).
func frame(level int) map[string]any {
	f := map[string]any{"level": float64(level), "scope": "全系统", "subject": "计划", "modality": "必须", "condition": "when a line plans",
		"executor": "legislation", "enforcement": "planstore check", "evidence": "GET /laws",
		"review": map[string]any{"by": "audit", "at": "2026-11-24T10:00Z", "verdict": "可量"}}
	if level >= 3 {
		f["parent"] = "90"
	}
	return f
}

func TestLawCardReads(t *testing.T) {
	reg := fakeLookup("80")
	std := []any{map[string]any{"id": "s1", "standard": "C cards per week fall", "measure": "count approval.card.created"}}
	good := []map[string]any{
		{"op": "add", "after": "text", "standards": std, "revert": "repeal it", "frame": frame(2)},
		{"op": "amend", "law": "79", "before": "a", "after": "b", "standards": std, "revert": "amend back", "frame": frame(2)},
		{"op": "repeal", "law": "79", "before": "a", "revert": "add it again"},
		{"op": "add", "law": "80", "after": "text", "standards": std, "revert": "repeal it", "frame": frame(2)},
	}
	for i, p := range good {
		if err := planstore.CheckLawCard(context.Background(), reg, p); err != nil {
			t.Fatalf("good %d refused: %v", i, err)
		}
	}
	dup := []any{std[0], std[0]}
	bad := []map[string]any{
		{"op": "change", "after": "x", "standards": std, "revert": "r"},
		{"op": "add", "after": "x", "revert": "r"},                                  // no standard
		{"op": "add", "after": "x", "standards": std},                               // no revert
		{"op": "amend", "law": "79", "after": "b", "standards": std, "revert": "r"}, // no before
		{"op": "repeal", "law": "79", "before": "a", "after": "b", "revert": "r"},   // after on repeal
		{"op": "add", "after": "x", "standards": dup, "revert": "r"},                // id twice
		{"op": "add", "after": "x", "standards": []any{map[string]any{"id": "s1"}}, "revert": "r"},
	}
	for i, p := range bad {
		if err := planstore.CheckLawCard(context.Background(), reg, p); err == nil {
			t.Fatalf("bad %d was accepted: %v", i, p)
		}
	}
}

func TestLawCardMustMatchTheRegister(t *testing.T) {
	std := []any{map[string]any{"id": "s1", "standard": "x", "measure": "y"}}
	reg := fakeLookup("80")
	refused := map[string]map[string]any{
		"law not in register":   {"op": "amend", "law": "999", "before": "a", "after": "b", "standards": std, "revert": "r"},
		"before differs":        {"op": "amend", "law": "79", "before": "stale", "after": "b", "standards": std, "revert": "r"},
		"already repealed":      {"op": "repeal", "law": "5", "before": "old", "revert": "r"},
		"add with wrong number": {"op": "add", "law": "81", "after": "x", "standards": std, "revert": "r", "frame": frame(2)},
	}
	for name, p := range refused {
		if err := planstore.CheckLawCard(context.Background(), reg, p); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	add := map[string]any{"op": "add", "after": "x", "standards": std, "revert": "r", "frame": frame(2)}
	if got := planstore.LawCardSummary(context.Background(), reg, add); got != "Law add: will become law 80" {
		t.Fatalf("add summary = %q", got)
	}
	if err := planstore.CheckLawCard(context.Background(), fakeLookup(""), add); err == nil {
		t.Fatal("add accepted while the register is empty")
	}
	if err := planstore.CheckLawCard(context.Background(), planstore.LawLookup{}, add); err == nil {
		t.Fatal("card accepted with no register wired")
	}
}

// 2026-11-24, Owner: clear the register's duplicates, superseded laws and
// entries that are not laws, one card for a batch. Each law must be in force
// and quoted as it stands; one law that does not read refuses the card.
func TestLawCardRepealMany(t *testing.T) {
	reg := fakeLookup("80")
	good := map[string]any{"op": "repeal_many", "revert": "add them back",
		"repeals": []any{map[string]any{"law": "79", "before": "a", "reason": "superseded by 80"}}}
	if err := planstore.CheckLawCard(context.Background(), reg, good); err != nil {
		t.Fatalf("good batch refused: %v", err)
	}
	if got := planstore.LawCardSummary(context.Background(), reg, good); got != "Law repeal ×1: 79" {
		t.Fatalf("summary = %q", got)
	}
	bad := map[string]map[string]any{
		"empty":         {"op": "repeal_many", "revert": "r", "repeals": []any{}},
		"no reason":     {"op": "repeal_many", "revert": "r", "repeals": []any{map[string]any{"law": "79", "before": "a"}}},
		"twice":         {"op": "repeal_many", "revert": "r", "repeals": []any{map[string]any{"law": "79", "before": "a", "reason": "x"}, map[string]any{"law": "79", "before": "a", "reason": "y"}}},
		"stale before":  {"op": "repeal_many", "revert": "r", "repeals": []any{map[string]any{"law": "79", "before": "old", "reason": "x"}}},
		"repealed":      {"op": "repeal_many", "revert": "r", "repeals": []any{map[string]any{"law": "5", "before": "old", "reason": "x"}}},
		"not in reg":    {"op": "repeal_many", "revert": "r", "repeals": []any{map[string]any{"law": "79", "before": "a", "reason": "x"}, map[string]any{"law": "999", "before": "z", "reason": "x"}}},
		"top-level law": {"op": "repeal_many", "law": "79", "revert": "r", "repeals": []any{map[string]any{"law": "79", "before": "a", "reason": "x"}}},
		"no revert":     {"op": "repeal_many", "repeals": []any{map[string]any{"law": "79", "before": "a", "reason": "x"}}},
	}
	for name, p := range bad {
		if err := planstore.CheckLawCard(context.Background(), reg, p); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

// 2026-11-24, the framework of laws v1 (Owner: 「挺好的，就按这个来」),
// written as code so it does not drift: what each level needs, conflicts,
// duplicates, dependencies and the parent are checked at creation.
func TestLawCardFramework(t *testing.T) {
	reg := fakeLookup("92")
	std := []any{map[string]any{"id": "s1", "standard": "x", "measure": "y"}}
	card := func(f map[string]any) map[string]any {
		return map[string]any{"op": "add", "after": "a law", "standards": std, "revert": "repeal it", "frame": f}
	}
	with := func(level int, k string, v any) map[string]any {
		f := frame(level)
		if v == nil {
			delete(f, k)
		} else {
			f[k] = v
		}
		return f
	}
	good := map[string]map[string]any{
		"basic":                 card(frame(2)),
		"domain under 90":       card(frame(3)),
		"detail under 90":       card(map[string]any{"level": float64(4), "scope": "领域:plan", "subject": "计划", "modality": "允许", "executor": "legislation", "parent": "90"}),
		"dao":                   card(map[string]any{"level": float64(0)}),
		"duplicate, superseded": card(with(2, "subject", "审批")),
		"duplicate, explained": card(func() map[string]any {
			f := with(2, "subject", "审批")
			f["distinct_from"] = "only for law cards"
			return f
		}()),
	}
	good["duplicate, superseded"]["frame"].(map[string]any)["supersedes"] = []any{"90"}
	for name, c := range good {
		if err := planstore.CheckLawCard(context.Background(), reg, c); err != nil {
			t.Errorf("%s refused: %v", name, err)
		}
	}
	bad := map[string]map[string]any{
		"no frame":                {"op": "add", "after": "a law", "standards": std, "revert": "r"},
		"no level":                card(with(2, "level", nil)),
		"level 9":                 card(with(2, "level", float64(9))),
		"basic without standards": {"op": "add", "after": "a law", "revert": "r", "frame": frame(2)},
		"basic without review":    card(with(2, "review", nil)),
		"review not 可量":           card(with(2, "review", map[string]any{"by": "audit", "at": "x", "verdict": "量不了"})),
		"basic without evidence":  card(with(2, "evidence", nil)),
		"domain without parent":   card(with(3, "parent", nil)),
		"parent not above":        card(with(2, "parent", "90")),
		"parent is material":      card(with(3, "parent", "79")),
		"unknown subject":         card(with(2, "subject", "心情")),
		"bad scope":               card(with(2, "scope", "everywhere")),
		"bad modality":            card(with(2, "modality", "最好")),
		"fundamental by 自觉":       card(with(1, "enforcement", "自觉")),
		"depends on repealed":     card(with(2, "depends_on", []any{"5"})),
		"conflict with 91":        card(func() map[string]any { f := with(2, "subject", "安全"); f["modality"] = "允许"; return f }()),
		"duplicate, unexplained":  card(with(2, "subject", "审批")),
		"supersedes repealed":     card(with(2, "supersedes", []any{"5"})),
		"bad expires_on":          card(with(2, "expires_on", "next week")),
	}
	for name, c := range bad {
		if err := planstore.CheckLawCard(context.Background(), reg, c); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// The three states: a transcribed row is 素材, a carded law with a level is
// 现行, a repealed one or a stage law past its day is 废止.
func TestLawState(t *testing.T) {
	lv := 2
	now, _ := time.Parse("2006-01-02", "2026-11-24")
	cases := map[string]struct {
		l    planstore.Law
		want string
	}{
		"transcribed":    {planstore.Law{Op: "transcribed", Status: "已落"}, planstore.StateMaterial},
		"card, no frame": {planstore.Law{Op: "add", Status: "已批"}, planstore.StateMaterial},
		"card, framed":   {planstore.Law{Op: "add", Status: "已批", Frame: &planstore.LawFrame{Level: &lv}}, planstore.StateInForce},
		"repealed":       {planstore.Law{Op: "repeal", Status: planstore.RepealedStatus, Frame: &planstore.LawFrame{Level: &lv}}, planstore.StateRepealed},
		"stage, past":    {planstore.Law{Op: "add", Status: "已批", Frame: &planstore.LawFrame{Level: &lv, ExpiresOn: "2026-11-01"}}, planstore.StateRepealed},
		"stage, running": {planstore.Law{Op: "add", Status: "已批", Frame: &planstore.LawFrame{Level: &lv, ExpiresOn: "2026-12-31"}}, planstore.StateInForce},
	}
	for name, c := range cases {
		if got := planstore.LawState(c.l, now); got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
}
