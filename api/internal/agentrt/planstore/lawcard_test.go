package planstore_test

import (
	"context"
	"slices"
	"strings"
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
		{Law: "90", Code: "LAW-5", Text: "approvals go by law", Status: "已批", Op: "add",
			Frame: &planstore.LawFrame{Level: lv(2), Scope: "全系统", Subject: "审批", Modality: "必须"}},
		{Law: "91", Code: "LAW-6", Text: "the six boundaries", Status: "已批", Op: "add",
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
	if got := planstore.LawCardSummary(context.Background(), reg, add); !strings.HasPrefix(got, "Law add: will become LAW-7") {
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
	if got := planstore.LawCardSummary(context.Background(), reg, good); got != "Law repeal ×1: 第 79 条" {
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

// 2026-11-24, Owner: a repealed law is deleted for good after seven days or
// when he asks. His purge card names only repealed laws, each quoted as it
// stands; a law in force (with or without its frame) is refused at creation.
func TestLawCardPurge(t *testing.T) {
	reg := fakeLookup("92")
	good := map[string]any{"op": "purge", "revert": "cannot be undone; add the law again by a new card",
		"repeals": []any{map[string]any{"law": "5", "before": "old", "reason": "Owner asked"}}}
	if err := planstore.CheckLawCard(context.Background(), reg, good); err != nil {
		t.Fatalf("purge of a repealed law refused: %v", err)
	}
	if got := planstore.LawCardSummary(context.Background(), reg, good); got != "Law purge (delete for good) ×1: 第 5 条" {
		t.Fatalf("summary = %q", got)
	}
	bad := map[string]map[string]any{
		"in force":     {"op": "purge", "revert": "r", "repeals": []any{map[string]any{"law": "90", "before": "approvals go by law", "reason": "x"}}},
		"unframed":     {"op": "purge", "revert": "r", "repeals": []any{map[string]any{"law": "79", "before": "a", "reason": "x"}}},
		"stale before": {"op": "purge", "revert": "r", "repeals": []any{map[string]any{"law": "5", "before": "new", "reason": "x"}}},
		"not in reg":   {"op": "purge", "revert": "r", "repeals": []any{map[string]any{"law": "999", "before": "z", "reason": "x"}}},
		"no reason":    {"op": "purge", "revert": "r", "repeals": []any{map[string]any{"law": "5", "before": "old"}}},
	}
	for name, p := range bad {
		if err := planstore.CheckLawCard(context.Background(), reg, p); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

// 2026-11-24, the framework of laws v1, step by step (Owner: first a
// framework that can hold good laws, then the laws in it, then enforcement
// law by law): what the framework finds goes on the card as notes; only a
// malformed frame or a conflict with a fundamental law refuses it.
func TestLawCardFramework(t *testing.T) {
	reg := fakeLookup("92")
	ctx := context.Background()
	std := []any{map[string]any{"id": "s1", "standard": "x", "measure": "y"}}
	card := func(f map[string]any) map[string]any {
		c := map[string]any{"op": "add", "after": "a law", "standards": std, "revert": "repeal it"}
		if f != nil {
			c["frame"] = f
		}
		return c
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
	// Notes, not refusals.
	notes := map[string]struct {
		c    map[string]any
		want string
	}{
		// 2026-11-25, LAW-1 四 / ISSUE-104: no frame is a law with fields
		// missing, said on the card.
		"no frame":               {card(nil), "缺框架字段"},
		"no level":               {card(with(2, "level", nil)), "缺层级"},
		"basic without review":   {card(with(2, "review", nil)), "审计复核"},
		"basic without evidence": {card(with(2, "evidence", nil)), "证据"},
		"domain without parent":  {card(with(3, "parent", nil)), "上位法"},
		"parent not above":       {card(with(2, "parent", "90")), "不比它高"},
		"parent not in register": {card(with(3, "parent", "999")), "不是现行法"},
		"parent lacks a level":   {card(with(3, "parent", "79")), "缺层级"},
		"unknown subject":        {card(with(2, "subject", "心情")), "主题表"},
		"bad scope":              {card(with(2, "scope", "everywhere")), "格式"},
		"fundamental by 自觉":      {card(with(1, "enforcement", "自觉")), "机器强制"},
		"duplicate":              {card(with(2, "subject", "审批")), "重复"},
		"not enforced":           {card(frame(2)), "未执行"},
		"enforced":               {card(with(2, "enforce", "执行中")), "开始执法"},
	}
	for name, c := range notes {
		if err := planstore.CheckLawCard(ctx, reg, c.c); err != nil {
			t.Errorf("%s refused: %v", name, err)
			continue
		}
		if line := planstore.LawCardSummary(ctx, reg, c.c); !strings.Contains(line, c.want) {
			t.Errorf("%s: the card's line %q does not say %q", name, line, c.want)
		}
	}
	// A complete frame with no finding has only the enforcement note. An
	// absorbs field is ignored, not noted (2026-11-25, LAW-1 四 / ISSUE-104).
	complete := frame(2)
	complete["absorbs"] = []any{"79"}
	if n := planstore.LawCardNotes(ctx, reg, card(complete)); len(n) != 1 {
		t.Errorf("a complete frame has notes %v", n)
	}
	// The integrity: a malformed frame, or loosening a fundamental law.
	refused := map[string]map[string]any{
		"level 9":                card(with(2, "level", float64(9))),
		"bad modality":           card(with(2, "modality", "最好")),
		"bad enforce":            card(with(2, "enforce", "maybe")),
		"bad expires_on":         card(with(2, "expires_on", "next week")),
		"loosens fundamental 91": card(func() map[string]any { f := with(2, "subject", "安全"); f["modality"] = "允许"; return f }()),
	}
	for name, c := range refused {
		if err := planstore.CheckLawCard(ctx, reg, c); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// Origin: 2026-11-25, LAW-1 四 / ISSUE-104: two states. A repealed law or a
// stage law past its day is 废止; every other law is 现行, a transcribed row
// or a card without a frame included. The repealed and stage cases
// are the positive control.
func TestLawState(t *testing.T) {
	lv := 2
	now, _ := time.Parse("2006-01-02", "2026-11-24")
	cases := map[string]struct {
		l    planstore.Law
		want string
	}{
		"transcribed":    {planstore.Law{Op: "transcribed", Status: "已落"}, planstore.StateInForce},
		"card, no frame": {planstore.Law{Op: "add", Status: "已批"}, planstore.StateInForce},
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

// 2026-11-24, Owner: step one is a framework, seen on Ops, with no law in it
// yet. GET /laws carries the framework itself: five levels with what each
// requires and how many laws are in force there, the rules, and the count in
// force (2026-11-25, LAW-1 四 / ISSUE-104: 现行 is the only state counted; a
// repealed row is not counted, so a register of repealed rows reads 现行 0).
func TestTheFramework(t *testing.T) {
	now, _ := time.Parse("2006-01-02", "2026-11-24")
	empty := planstore.TheFramework([]planstore.Law{{Law: "1", Op: "repeal", Status: planstore.RepealedStatus}}, now)
	if len(empty.Levels) != 4 || empty.Rule == "" || len(empty.States) != 1 || empty.States[planstore.StateInForce] != 0 {
		t.Fatalf("an empty frame = %+v", empty.States)
	}
	if empty.Levels[3].Name != "领域法" || !slices.Contains(empty.Levels[3].Required, "parent") || slices.Contains(empty.Levels[0].Required, "standards") {
		t.Fatalf("levels = %+v", empty.Levels)
	}
	if len(empty.Integrity) != 4 || len(empty.Fields) == 0 || len(empty.Subjects) == 0 {
		t.Fatalf("framework = %+v", empty)
	}
	lv := 2
	one := planstore.TheFramework([]planstore.Law{
		{Law: "1", Op: "transcribed", Status: "已落"},
		{Law: "2", Op: "add", Status: "已批", Frame: &planstore.LawFrame{Level: &lv}},
		{Law: "3", Op: "repeal", Status: planstore.RepealedStatus},
	}, now)
	// The transcribed row counts as in force, at no level.
	if _, has := one.States[planstore.StateRepealed]; has || one.States[planstore.StateInForce] != 2 || one.Levels[2].InForce != 1 {
		t.Fatalf("counts = %+v, level 2 = %d", one.States, one.Levels[2].InForce)
	}
}

// Origin: 2026-11-25, LAW-1 version 3 (#754): the details a domain owner sets
// are RULE, not LAW. The framework lists four levels of LAW; a card that says
// level 4 is noted, not refused, and a level-4 law in the register does not
// break the framework's counts.
func TestLevelFourIsRule(t *testing.T) {
	reg := fakeLookup("99")
	ctx := context.Background()
	card := map[string]any{"op": "add", "after": "a detail", "revert": "r", "standards": []any{map[string]any{"id": "s1", "standard": "x", "measure": "y"}},
		"frame": map[string]any{"level": 4, "parent": "LAW-5"}}
	ok(t, planstore.CheckLawCard(ctx, reg, card))
	if got := planstore.LawCardSummary(ctx, reg, card); !strings.Contains(got, "是 RULE，不立为 LAW") {
		t.Fatalf("summary = %q", got)
	}
	four := 4
	fw := planstore.TheFramework([]planstore.Law{{Law: "99", Op: "add", Text: "x", Status: "已批", Frame: &planstore.LawFrame{Level: &four}}}, time.Now())
	if len(fw.Levels) != 4 {
		t.Fatalf("levels = %d, want 4", len(fw.Levels))
	}
}
