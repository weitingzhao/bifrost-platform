package debtimport

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Full ledgers deliberately stay outside this public repository. Snapshot
// state/priority counts are audited. Project/repo ownership is a suggestion,
// explicitly enumerated for crew review, not an independent source count.
func TestFullLedgers(t *testing.T) {
	lpath, rpath := os.Getenv("DEBTIMPORT_LEDGER"), os.Getenv("DEBTIMPORT_RATCHETS")
	if lpath == "" || rpath == "" {
		t.Skip("set DEBTIMPORT_LEDGER and DEBTIMPORT_RATCHETS for frozen-source acceptance")
	}
	ledger, err := os.ReadFile(lpath)
	if err != nil {
		t.Fatal(err)
	}
	ratchets, err := os.ReadFile(rpath)
	if err != nil {
		t.Fatal(err)
	}
	opts := testOptions(t)
	a, report, err := Parse(ledger, ratchets, opts)
	if err != nil {
		t.Fatal(err)
	}
	b, rb, err := Parse(ledger, ratchets, opts)
	if err != nil {
		t.Fatal(err)
	}
	ja, _ := a.JSON()
	jb, _ := b.JSON()
	ra, _ := report.JSON()
	rbb, _ := rb.JSON()
	if !bytes.Equal(ja, jb) || !bytes.Equal(ra, rbb) {
		t.Fatal("same import context produced different bytes")
	}
	var decoded Request
	if err = json.Unmarshal(ja, &decoded); err != nil {
		t.Fatal(err)
	}
	assertAccounting(t, decoded, report, ledger, ratchets)
	if a.Floor < 488 {
		t.Fatalf("floor %d", a.Floor)
	}
	// Independently count source headings, priorities and the documented state
	// prefix (not the parser's functions or its output).
	priorities, states := map[string]int{}, map[string]int{}
	for _, m := range regexp.MustCompile(`(?m)^\*\*(P[0-3]) · [^\n]+\*\*$`).FindAllSubmatch(ledger, -1) {
		priorities[string(m[1])]++
	}
	stateNames := map[string]string{"未开始": "open", "在做": "doing", "观察中": "watching", "待你签收": "awaiting_signoff"}
	for _, m := range regexp.MustCompile(`(?m)^- \*\*状态\*\*[:：][ \t]*(.*)$`).FindAllSubmatch(ledger, -1) {
		v := strings.TrimSpace(strings.ReplaceAll(string(m[1]), "**", ""))
		cut := len(v)
		for _, delimiter := range []string{"（", "(", "——", "。", "｜", "："} {
			if i := strings.Index(v, delimiter); i >= 0 && i < cut {
				cut = i
			}
		}
		states[stateNames[strings.TrimSpace(v[:cut])]]++
	}
	if !reflect.DeepEqual(priorities, report.Distribution.Priority) || !reflect.DeepEqual(states, report.Distribution.State) {
		t.Fatalf("independent source count differs: %v %v", priorities, states)
	}
	var audited struct {
		LedgerSHA256   string       `json:"ledger_sha256"`
		RatchetsSHA256 string       `json:"ratchets_sha256"`
		Distribution   Distribution `json:"distribution"`
	}
	data := fixture(t, "fa7e2e2-counts.json")
	if err = json.Unmarshal(data, &audited); err != nil {
		t.Fatal(err)
	}
	if audited.LedgerSHA256 == digest(string(ledger)) && audited.RatchetsSHA256 == digest(string(ratchets)) {
		if !reflect.DeepEqual(audited.Distribution.State, report.Distribution.State) || !reflect.DeepEqual(audited.Distribution.Priority, report.Distribution.Priority) {
			t.Fatalf("source audit differs\nwant %+v\ngot  %+v", audited.Distribution, report.Distribution)
		}
		if report.Counts["inferred_repo_part"] != 47 || report.Counts["wave_membership"] != 13 || len(a.Runs) != 81 || len(a.Decisions) != 21 {
			t.Fatal("frozen source coverage changed without review")
		}
		assertValuePins(t, a, report, ledger)
	} else {
		t.Log("source differs from audited snapshot; frozen state/priority count fixture is not applicable")
	}
	guards := 0
	for _, ref := range a.Refs {
		if ref.RefKind == "guarded_by" {
			guards++
		}
	}
	t.Logf("debts=%d ratchets=%d parts=%d runs=%d decisions=%d gaps=%d guarded_by=%d floor=%d findings=%d JSON_bytes=%d unreported_diff_bytes=0", len(a.Debts), len(a.Ratchets), len(a.Parts), len(a.Runs), len(a.Decisions), len(a.Gaps), guards, a.Floor, len(report.Findings), len(ja))
}

// assertValuePins pins reviewed parser output for fa7e2e2. The parser reports
// its own rendering differences, so their count per bound column and the text
// kind distribution are fixed here: a value that is rewritten, or a field that
// silently moves to another kind (e.g. note), changes a pinned number.
func assertValuePins(t *testing.T, r Request, report Report, ledger []byte) {
	t.Helper()
	pins := []struct {
		name      string
		got, want map[string]int
	}{
		{"rendered differences per column", differenceTypes(report), map[string]int{"state": 237, "apply_gate": 200, "risk": 24}},
		{"debt_text kinds", countBy(r.Texts, func(v Text) string { return v.TextKind }), map[string]int{
			"import_raw": 481, "measured": 347, "ratchet_plan": 248, "fix": 238, "acceptance_text": 231, "impact": 222, "claim": 194,
			"note": 78, "watch": 55, "follow_up_text": 27, "signoff_summary": 26, "ratchet_text": 26, "now": 21, "next": 18}},
		{"source_text kinds", countBy(r.SourceTexts, func(v SourceText) string { return v.TextKind }), map[string]int{"import_raw": 36}},
		{"evidence kinds", countBy(r.Evidence, func(v Evidence) string { return v.EvidenceKind }), map[string]int{"code_ref": 318, "measurement": 302, "commit": 114, "doc_ref": 21}},
		{"report categories", report.Counts, map[string]int{
			"ambiguous_evidence_commit": 26, "approval_direction": 9, "approval_direction_group": 1, "closed_decision_member": 13, "conditional_approval": 22,
			"cross_project": 18, "decision_review_required": 12, "gap_project_required": 2, "incomplete_floor": 1, "incomplete_ratchet_row": 2,
			"inferred_repo_part": 47, "inferred_repos": 41, "malformed_table_markdown": 1, "measurement_text": 55, "missing_acceptance": 19,
			"missing_approval": 50, "missing_evidence": 1, "missing_repos": 12, "overdue_watch": 27, "plain_acceptance_result": 21,
			"project_suggestion": 219, "raw_heading": 2212, "raw_import_raw": 517, "raw_note": 78, "rendered_difference": 461, "stale_decision": 10,
			"umbrella_needs_id": 1, "unassigned_acceptance_commits": 60, "undated_watch": 26, "unmapped_acceptance": 1, "unmapped_approval": 23,
			"unmapped_project": 31, "unmapped_state": 4, "unrecognized_repo": 4, "unresolved_code_repo": 19, "wave_membership": 13,
			"workspace_path_outside_repos": 2}},
	}
	for _, pin := range pins {
		if !maps.Equal(pin.got, pin.want) {
			t.Errorf("%s changed without review\nwant %v\ngot  %v", pin.name, pin.want, pin.got)
		}
	}
	// Each canonical difference keeps the source meaning: med becomes medium,
	// a state keeps its leading label, a gate becomes one closed enum value.
	gates := []string{"none", "public_interface", "ddl", "security", "delete", "prod_change", "cluster_write", "rule_sign", "cross_repo_release", "other"}
	for _, f := range report.Findings {
		if f.Category != "rendered_difference" {
			continue
		}
		d := f.Source
		column := d.Paths[0][strings.LastIndex(d.Paths[0], "/")+1:]
		label, _, _ := strings.Cut(d.Rendered, "（")
		if (column == "risk" && (d.Original != "med" || d.Rendered != "medium")) ||
			(column == "state" && !strings.HasPrefix(strings.TrimLeft(d.Original, "*"), label)) ||
			(column == "apply_gate" && !slices.Contains(gates, d.Rendered)) {
			t.Errorf("line %d: %s difference changes meaning: %q -> %q", d.Line, column, d.Original, d.Rendered)
		}
	}
	// Independently of the parser's key table: every bold section 3 key (and
	// the plain 验收 line) in the source is one text of the design's kind.
	for key, kind := range sectionThreeKinds {
		pattern := `(?m)^- \*\*` + regexp.QuoteMeta(key) + `\*\*[:：]`
		if key == "验收" {
			pattern = `(?m)^- (?:\*\*验收\*\*|验收)[:：]`
		}
		want := len(regexp.MustCompile(pattern).FindAll(ledger, -1))
		got := 0
		for _, v := range r.Texts {
			if v.Heading == key && v.TextKind == kind {
				got++
			}
		}
		if want == 0 || got != want {
			t.Errorf("%s: %d source fields, %d %s texts", key, want, got, kind)
		}
	}
}

func countBy[T any](rows []T, key func(T) string) map[string]int {
	out := map[string]int{}
	for _, v := range rows {
		out[key(v)]++
	}
	return out
}
