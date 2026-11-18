package regressions

import (
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Origin: 2026-11-17, the coordinator: "a known regression on main" is
// information, no gate. A line records it; the line that fixes it records the
// fix; a PROD release's context lists those still open. Known and fixed pair
// on <repo>@<introduced>; a known reported again after its fix opens it again.
// (The ledger path, Known and Fixed against a database with ErrNotKnown on a
// fix for nothing known, ran green on 2026-11-17 with the store's seed; a test
// here may not import the store, a package several domains share.)
func TestFoldPairsKnownAndFixed(t *testing.T) {
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	at := func(m int) time.Time { return time.Date(2026, 11, 17, 20, m, 0, 0, time.UTC) }
	ev := func(typ, repo, intro string, m int, extra map[string]any) ledgerRow {
		body := map[string]any{"repo": repo, "introduced": intro}
		for k, v := range extra {
			body[k] = v
		}
		return ledgerRow{Type: typ, At: at(m), Body: body}
	}
	// Newest first, as the ledger answers.
	rows := []ledgerRow{
		ev(TypeKnown, "bifrost-platform", a, 9, map[string]any{"impact": "back again", "by": "thread:local_x"}),
		ev(TypeFixed, "bifrost-platform", a, 5, map[string]any{"fixed": b, "by": "thread:local_y"}),
		ev(TypeKnown, "bifrost-trade-api", b, 3, map[string]any{"impact": "orders page", "by": "thread:local_z"}),
		ev(TypeKnown, "bifrost-platform", a, 1, map[string]any{"impact": "module activity", "by": "thread:local_x"}),
	}
	all := fold(rows, "", false)
	if len(all) != 2 || all[0].Repo != "bifrost-platform" || all[0].Impact != "back again" || all[0].Fixed != "" {
		t.Fatalf("all = %+v (a known after its fix reopens it, newest first)", all)
	}
	if open := fold(rows[1:], "bifrost-platform", true); len(open) != 0 {
		t.Fatalf("fixed and not reported again, open = %+v", open)
	}
	if fixed := fold(rows[1:], "bifrost-platform", false); len(fixed) != 1 || fixed[0].Fixed != b || fixed[0].FixedBy != "thread:local_y" {
		t.Fatalf("fixed = %+v", fixed)
	}
	if trade := fold(rows, "bifrost-trade-api", true); len(trade) != 1 || trade[0].Impact != "orders page" {
		t.Fatalf("one repository = %+v", trade)
	}
	// A fix with no known on record is not a regression.
	if lone := fold([]ledgerRow{ev(TypeFixed, "bifrost-platform", b, 2, map[string]any{"fixed": a})}, "", false); len(lone) != 0 {
		t.Fatalf("a fix alone = %+v", lone)
	}
}

func TestCheckAndBody(t *testing.T) {
	a := strings.Repeat("a", 40)
	if err := Check("bifrost-platform", a, "x"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][3]string{{"Bifrost", a, "x"}, {"bifrost-platform", "abc", "x"}, {"bifrost-platform", a, " "}, {"bifrost-platform", a, strings.Repeat("x", MaxImpact+1)}} {
		if Check(bad[0], bad[1], bad[2]) == nil {
			t.Errorf("Check%q passed", bad)
		}
	}
	// The ledger takes an event of these types only with its fixed fields made of
	// repo and introduced (the bus builds the envelope from them).
	good := body(TypeKnown, "bifrost-platform", a)
	good["impact"], good["by"] = "x", "thread:local_x"
	if err := eventlog.CheckBody(TypeKnown, good); err != nil {
		t.Fatalf("good body: %v", err)
	}
	good["subject"] = "bifrost-platform@" + strings.Repeat("d", 40)
	if err := eventlog.CheckBody(TypeKnown, good); err == nil {
		t.Fatal("a body whose subject is not its key was taken")
	}
	fixed := body(TypeFixed, "bifrost-platform", a)
	fixed["fixed"], fixed["by"] = strings.Repeat("e", 40), "thread:local_x"
	if err := eventlog.CheckBody(TypeFixed, fixed); err != nil {
		t.Fatalf("fixed body: %v", err)
	}
	fixed["change"] = "known"
	if err := eventlog.CheckBody(TypeFixed, fixed); err == nil {
		t.Fatal("a fixed whose change is not fixed was taken")
	}
}
