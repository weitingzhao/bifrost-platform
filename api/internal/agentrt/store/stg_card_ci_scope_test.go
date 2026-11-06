package store

import (
	"strings"
	"testing"
	"time"
)

// An STG CI block carries the scope that decided P and the last full green's
// run, commit and age, so the card says why P waits (R5:5.7-1). A block
// without it reads as before.
func TestStgCIBlockCardSaysWhyPWaits(t *testing.T) {
	t.Parallel()
	h := stgReady(t, stgTestMember())
	obs := StgCIObservation{Status: "Missing", ReadAt: h.at, Scope: "incremental", Stale: 1, LastFullRun: "full-ci", LastFullCommit: stgSHA("c"),
		LastFullAt: h.at.Add(-51*time.Hour - 10*time.Minute), FullMaxAge: 48 * time.Hour}
	base := stgTestBase()
	detail := map[string]any{"members": []any{stgTestMember()}, "ci_scope": obs.ScopeDetail()}
	h.add(stgRecord("blocked", "block", StgBlockID(stgSHA("b"), base["P"].(string), "ci_missing", "v1"), "why", "ci_missing", "P", stgSHA("b"), "base", base, "first_at", h.at.Format(time.RFC3339Nano), "evidence_version", "v1", "detail", detail))
	trs := stgTriggers(h.state())
	if len(trs) != 1 || trs[0].CIScope == nil {
		t.Fatal("block lost its CI scope", trs)
	}
	body := stgCardBody("bifrost-platform", trs[0])
	want := "Platform CI scope of P: incremental. 1 incremental green CI run(s) of P set aside. Last full CI green 51 h ago (run full-ci, commit " + stgSHA("c") + "), cap 48 h."
	if !strings.Contains(body, want) {
		t.Fatalf("body lacks %q:\n%s", want, body)
	}

	none := obs
	none.LastFullRun, none.LastFullCommit, none.LastFullAt = "", "", time.Time{}
	tr := trs[0]
	tr.CIScope = none.ScopeDetail()
	if line := stgCIScopeLine(tr); !strings.Contains(line, "No full CI green found") || !strings.Contains(line, "cap 48 h") {
		t.Fatal(line)
	}
	tr.CIScope = nil
	if strings.Contains(stgCardBody("bifrost-platform", tr), "Platform CI scope") {
		t.Fatal("a block without a CI scope changed its card")
	}
	if (StgCIObservation{ReadAt: h.at}).ScopeDetail() != nil {
		t.Fatal("an observation not read from CI claimed a scope")
	}
}
