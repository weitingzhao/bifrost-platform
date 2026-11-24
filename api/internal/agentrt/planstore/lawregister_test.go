package planstore_test

import (
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

const lawTable = "# Owner 定的法\n\n| 栏 | 写什么 |\n|---|---|\n| 1 | 不是法 |\n\n## 登记\n\n" +
	"| # | 哪天 | 他的话（要点） | 落在哪 | 机器强制 | 状态 | 旧说法 |\n|---|---|---|---|---|---|---|\n" +
	"| 1 | 2026-11-14 | 能不能自己做，看有没有后悔药 | `a|b` §5 | 签名规则 | 未落 | — |\n" +
	"| 2 | 2026-11-14 | 先找不用卡的路 | §5 | 只有文字 | 已落 | 「旧」 |\n"

// 2026-11-23, Owner: a ticket for a law changes a real record of the law in
// the system. The table parses (a pipe inside backticks is text; the 「栏」
// table above 「## 登记」 is not a law).
func TestParseLawTable(t *testing.T) {
	rows, err := planstore.ParseLawTable(lawTable)
	ok(t, err)
	if len(rows) != 2 || rows[0].Law != "1" || rows[0].Where != "`a|b` §5" || rows[1].OldWording != "「旧」" {
		t.Fatalf("rows = %+v", rows)
	}
	if _, err := planstore.ParseLawTable("no register"); err == nil {
		t.Fatal("a file without 「## 登记」 parsed")
	}
}

// The register: the table is transcribed once and again only where it
// changed; an approved add takes the next number, an amend a new version, a
// repeal the status 废止; a law a card changed is no longer moved by the
// file; a card is written once; an add waits while the register is empty.
func TestLawRegister(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	announce := func(id string, n int) {
		ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "card:" + id, Type: "approval.card.executed", Actor: "controller",
				Body: map[string]any{"id": id, "card": fmt.Sprintf("#%d", n), "subject": fmt.Sprintf("#%d", n), "key": fmt.Sprintf("#%d", n), "number": n, "action": "law_amend",
					"status": "executed", "ref": "/api/v1/approvals/" + id, "change": "executed"}})
			return err
		}))
	}
	add := planstore.LawCard{ApprovalID: "appr_00000000000000b1", Number: 101, Op: "add", After: "structure follows need", DecidedOn: "2026-11-23",
		Standards: []planstore.LawStandard{{ID: "s1", Standard: "zero by hand", Measure: "Audit counts"}}}
	announce(add.ApprovalID, 101)

	// Empty register: the add waits for its number; its item is still planned.
	_, err := planstore.RecordLaws(ctx, w.rt, []planstore.LawCard{add})
	ok(t, err)
	if next, err := planstore.NextLawNumber(ctx, w.rt); err != nil || next != "" {
		t.Fatalf("next of an empty register = %q %v", next, err)
	}

	rows, err := planstore.ParseLawTable(lawTable)
	ok(t, err)
	n, err := planstore.TranscribeLaws(ctx, w.rt, rows, "OWNER_LAW.md@a")
	ok(t, err)
	if n != 2 {
		t.Fatalf("transcribed %d, want 2", n)
	}
	if n, _ = planstore.TranscribeLaws(ctx, w.rt, rows, "OWNER_LAW.md@b"); n != 0 {
		t.Fatalf("an unchanged table transcribed %d again", n)
	}
	// 2026-11-24, the switch (charters/1-plan.md §7): once the register has
	// laws, a row with a new number is not taken; a new law comes only by a
	// law_amend card.
	handAdded := append(append([]planstore.LawRow(nil), rows...), planstore.LawRow{Law: "9", SaidOn: "2026-11-24", Text: "written in by hand", Status: "未落"})
	if n, _ = planstore.TranscribeLaws(ctx, w.rt, handAdded, "OWNER_LAW.md@hand"); n != 0 {
		t.Fatalf("a hand-added law was transcribed (%d)", n)
	}
	if _, have, _ := planstore.CurrentLaw(ctx, w.rt, "9"); have {
		t.Fatal("law 9 is in the register though no card added it")
	}
	if next, _ := planstore.NextLawNumber(ctx, w.rt); next != "3" {
		t.Fatalf("next = %q, want 3", next)
	}

	// Now the add takes 3; once, however often it is found.
	for range 2 {
		_, err = planstore.RecordLaws(ctx, w.rt, []planstore.LawCard{add})
		ok(t, err)
	}
	l3, have, err := planstore.CurrentLaw(ctx, w.rt, "3")
	ok(t, err)
	if !have || l3.Op != "add" || l3.Text != "structure follows need" || l3.ApprovalID != add.ApprovalID || l3.SaidOn != "2026-11-23" || l3.Version != 1 {
		t.Fatalf("law 3 = %+v", l3)
	}
	if c := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'law_recorded' AND body_json->>'approval_id' = $1`, add.ApprovalID); c != 1 {
		t.Fatalf("the add is in the register %d times", c)
	}

	// An amend of 2 is a new version; then the file no longer moves 2.
	amend := planstore.LawCard{ApprovalID: "appr_00000000000000b2", Number: 102, Op: "amend", Law: "2", Before: "先找不用卡的路", After: "先找不用卡的路，找不到再建卡",
		Standards: []planstore.LawStandard{{ID: "s1", Standard: "cards per week falls", Measure: "list_requests"}}}
	announce(amend.ApprovalID, 102)
	_, err = planstore.RecordLaws(ctx, w.rt, []planstore.LawCard{amend})
	ok(t, err)
	changed := append([]planstore.LawRow(nil), rows...)
	changed[1].Text = "the file's own new words"
	changed[0].Status = "已落"
	if n, _ = planstore.TranscribeLaws(ctx, w.rt, changed, "OWNER_LAW.md@c"); n != 1 {
		t.Fatalf("transcribed %d; want 1 (law 1 changed, law 2 is the card's)", n)
	}
	l2, _, _ := planstore.CurrentLaw(ctx, w.rt, "2")
	if l2.Version != 2 || l2.Op != "amend" || l2.Text != amend.After || l2.Where != "§5" {
		t.Fatalf("law 2 = %+v", l2)
	}
	if l1, _, _ := planstore.CurrentLaw(ctx, w.rt, "1"); l1.Version != 2 || l1.Status != "已落" {
		t.Fatalf("law 1 = %+v", l1)
	}

	// A repeal keeps the text and says 废止; a card on a law not in the
	// register is refused.
	repeal := planstore.LawCard{ApprovalID: "appr_00000000000000b3", Number: 103, Op: "repeal", Law: "1", Before: "能不能自己做，看有没有后悔药"}
	announce(repeal.ApprovalID, 103)
	_, err = planstore.RecordLaws(ctx, w.rt, []planstore.LawCard{repeal})
	ok(t, err)
	if l1, _, _ := planstore.CurrentLaw(ctx, w.rt, "1"); l1.Status != planstore.RepealedStatus || l1.Text != rows[0].Text || l1.Op != "repeal" {
		t.Fatalf("law 1 after the repeal = %+v", l1)
	}
	ghost := planstore.LawCard{ApprovalID: "appr_00000000000000b4", Number: 104, Op: "amend", Law: "99", Before: "x", After: "y"}
	announce(ghost.ApprovalID, 104)
	if _, err = planstore.RecordLaws(ctx, w.rt, []planstore.LawCard{ghost}); err == nil {
		t.Fatal("an amend of a law the register does not hold was taken")
	}
	// A batch repeal (2026-11-24): each law it names becomes 废止, with the
	// card and the reason as its source; once, however often it is found.
	batch := planstore.LawCard{ApprovalID: "appr_00000000000000b5", Number: 105, Op: "repeal_many",
		Repeals: []planstore.LawRepeal{{Law: "2", Before: amend.After, Reason: "duplicate of 3"}, {Law: "3", Before: "structure follows need", Reason: "not a law"}}}
	announce(batch.ApprovalID, 105)
	for range 2 {
		_, err = planstore.RecordLaws(ctx, w.rt, []planstore.LawCard{batch})
		ok(t, err)
	}
	for _, n := range []string{"2", "3"} {
		l, _, _ := planstore.CurrentLaw(ctx, w.rt, n)
		if l.Status != planstore.RepealedStatus || l.Op != "repeal" || l.ApprovalID != batch.ApprovalID {
			t.Fatalf("law %s after the batch = %+v", n, l)
		}
	}
	if c := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'law_recorded' AND body_json->>'approval_id' = $1`, batch.ApprovalID); c != 2 {
		t.Fatalf("the batch wrote %d records, want 2", c)
	}
	laws, err := planstore.CurrentLaws(ctx, w.rt)
	ok(t, err)
	if len(laws) != 3 || laws[0].Law != "1" || laws[2].Law != "3" {
		t.Fatalf("laws = %+v", laws)
	}
	for _, s := range []string{"law:1", "law:2", "law:3"} {
		w.chainHolds(t, s)
	}
}
