package debtimport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
)

// Apply only explicit source-located report exceptions to the source. Expected
// bytes never come from the exporter's pointers or its rendering functions.
func reportedExport(t *testing.T, source []byte, report Report, doc string) []byte {
	t.Helper()
	differences := []SourceDetail{}
	for _, f := range report.Findings {
		if f.Category == "rendered_difference" && f.Source != nil && f.Source.Document == doc {
			differences = append(differences, *f.Source)
		}
	}
	sort.Slice(differences, func(i, j int) bool { return differences[i].Start < differences[j].Start })
	var out bytes.Buffer
	at := 0
	for _, d := range differences {
		if d.Start < at || d.End > len(source) || string(source[d.Start:d.End]) != d.Original {
			t.Fatal("invalid source-located difference")
		}
		if d.Line != bytes.Count(source[:d.Start], []byte("\n"))+1 {
			t.Fatal("wrong report line")
		}
		out.Write(source[at:d.Start])
		out.WriteString(d.Rendered)
		at = d.End
	}
	out.Write(source[at:])
	return out.Bytes()
}

func assertAccounting(t *testing.T, r Request, report Report, ledger, ratchets []byte) {
	t.Helper()
	root, err := projection(r)
	if err != nil {
		t.Fatal(err)
	}
	for doc, fragments := range [][]Fragment{r.Markdown.Ledger, r.Markdown.Ratchets} {
		source := [][]byte{ledger, ratchets}[doc]
		at := 0
		// Cover every byte, hence every line, not just recognized field counts.
		for _, f := range fragments {
			if f.Start != at || f.End <= f.Start || f.End > len(source) {
				t.Fatalf("unowned/overlapping source at %d", at)
			}
			original := string(source[f.Start:f.End])
			at = f.End
			if len(f.Paths) == 0 {
				if !layoutOnly(original) || original != f.Syntax {
					t.Fatalf("prose hidden as formatting: %q", original)
				}
				continue
			}
			if f.Syntax != "" {
				t.Fatal("field plus literal")
			}
			values, err := valuesAt(root, f.Paths)
			if err != nil {
				t.Fatal(err)
			}
			owned := slices.ContainsFunc(values, func(v any) bool { return fmt.Sprint(v) == original })
			if f.Render == "repos" {
				parts := []string{}
				for _, v := range values {
					parts = append(parts, fmt.Sprint(v))
				}
				owned = original == strings.Join(parts, ", ")
			}
			if f.Render == "state" && len(values) > 0 {
				labels := map[string]string{"open": "未开始", "doing": "在做", "watching": "观察中", "awaiting_signoff": "待你签收", "closed": "已关闭"}
				owned = labels[fmt.Sprint(values[0])] == original
			}
			if !owned {
				owned = slices.ContainsFunc(report.Findings, func(v Finding) bool {
					return v.Category == "rendered_difference" && v.Source != nil && v.Source.Document == documentName(doc) && v.Source.Start == f.Start && v.Source.End == f.End && v.Source.Original == original
				})
			}
			if !owned {
				t.Fatalf("line %d has no field/report ownership", bytes.Count(source[:f.Start], []byte("\n"))+1)
			}
		}
		if at != len(source) {
			t.Fatalf("source tail unowned: %d", at)
		}
	}
	for i, v := range r.Texts {
		if v.TextKind == "import_raw" || v.TextKind == "note" {
			assertRawListed(t, report, pointer("debt_text", i, "body"), v.Body)
		}
		if v.Heading != "" {
			assertRawListed(t, report, pointer("debt_text", i, "heading"), v.Heading)
		}
	}
	for i, v := range r.SourceTexts {
		assertRawListed(t, report, pointer("source_text", i, "body"), v.Body)
	}
	lout, rout, err := Export(r)
	if err != nil {
		t.Fatal(err)
	}
	for i, actual := range [][]byte{lout, rout} {
		want := reportedExport(t, [][]byte{ledger, ratchets}[i], report, documentName(i))
		if !bytes.Equal(actual, want) {
			t.Fatalf("%s export has unreported differences", documentName(i))
		}
	}
}
func assertRawListed(t *testing.T, report Report, path, body string) {
	t.Helper()
	if !slices.ContainsFunc(report.Findings, func(f Finding) bool {
		return strings.HasPrefix(f.Category, "raw_") && f.Source != nil && slices.Contains(f.Source.Paths, path) && f.Source.Original == body
	}) {
		t.Fatalf("raw record unlisted: %s", path)
	}
}

func TestEverySourceLineHasFieldOrReportedRawOwnership(t *testing.T) {
	for _, name := range []string{"legacy.md", "modern.md", "fields.md", "states.md", "approvals.md", "sections.md", "signoffs.md", "annotated-approvals.md", "orphan-prose.md", "field-keys.md", "locations.md"} {
		t.Run(name, func(t *testing.T) {
			r, review := parseFixture(t, name)
			assertAccounting(t, r, review, fixture(t, name), fixture(t, "ratchets.md"))
		})
	}
	r, _ := parseFixture(t, "orphan-prose.md")
	if !slices.ContainsFunc(r.Texts, func(v Text) bool {
		return v.DebtID == "TD-445" && v.TextKind == "import_raw" && strings.Contains(v.Body, "撞号后改为 TD-445")
	}) {
		t.Fatal("TD-445 renumbering note missing from debt_text")
	}
}

func TestExportHasNoOriginalOrBaselineEscapeHatch(t *testing.T) {
	r, review := parseFixture(t, "annotated-approvals.md")
	serialized, _ := json.Marshal(r.Markdown)
	if bytes.Contains(serialized, []byte(`"original"`)) || bytes.Contains(serialized, []byte(`"baseline"`)) {
		t.Fatal("source baseline in layout")
	}
	before, _, err := Export(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Debts[0].Cost = "XL"
	after, _, err := Export(r)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, after) || bytes.Equal(after, reportedExport(t, fixture(t, "annotated-approvals.md"), review, "ledger")) {
		t.Fatal("changed parsed cost invisible to source comparison")
	}
	r.Markdown.Ledger[0] = Fragment{Syntax: "hidden narrative", Start: 0, End: 1}
	if _, _, err := Export(r); err == nil {
		t.Fatal("prose allowed in syntax")
	}
}
