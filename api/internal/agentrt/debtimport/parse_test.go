package debtimport

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

type fakeHistory struct{ err error }

func (h fakeHistory) FirstSeen(string) (time.Time, error) {
	return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), h.err
}
func testOptions(t *testing.T) Options {
	t.Helper()
	data, err := os.ReadFile("../../../cmd/debtimport/projects.json")
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Projects    []Project           `json:"projects"`
		CrewProject string              `json:"crew_project"`
		RepoRoots   map[string][]string `json:"repo_roots"`
	}
	if err = json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	return Options{ImportedAt: time.Date(2026, 11, 2, 18, 0, 0, 0, time.UTC), History: fakeHistory{}, Projects: c.Projects, CrewProject: c.CrewProject, RepoRoots: c.RepoRoots}
}
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func parseFixture(t *testing.T, name string) (Request, Report) {
	t.Helper()
	r, report, err := Parse(fixture(t, name), fixture(t, "ratchets.md"), testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	return r, report
}
func debt(t *testing.T, r Request, id string) Debt {
	t.Helper()
	for _, d := range r.Debts {
		if d.DebtID == id {
			return d
		}
	}
	t.Fatalf("missing %s", id)
	return Debt{}
}
func hasFinding(report Report, id, category string) bool {
	return slices.ContainsFunc(report.Findings, func(f Finding) bool { return f.Item == id && f.Category == category })
}
func textOf(r Request, id, kind string) string {
	for _, v := range r.Texts {
		if v.DebtID == id && v.TextKind == kind {
			return v.Body
		}
	}
	return ""
}

func TestRule01RejectsMisplacedAndDuplicateHeadings(t *testing.T) {
	_, _, err := Parse(fixture(t, "misplaced.md"), nil, testOptions(t))
	if err == nil || !strings.Contains(err.Error(), "outside ## 条目") {
		t.Fatalf("misplaced: %v", err)
	}
	data := fixture(t, "legacy.md")
	_, _, err = Parse(append(append([]byte{}, data...), data...), nil, testOptions(t))
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate: %v", err)
	}
}
func TestRule02RendererTitleAndLateTitle(t *testing.T) {
	r, _ := parseFixture(t, "legacy.md")
	d := debt(t, r, "TD-91")
	if d.Priority != "P1" || d.Area != "flex-ib" || d.Title != "A failed cash-transactions write is recorded as a successful run: core returns 0 on any exception and the job counts it as 'ok, 0 rows'" {
		t.Fatalf("title: %+v", d)
	}
	data := fixture(t, "fields.md")
	data = bytes.Replace(data, []byte("### TD-268\n\n"), []byte("### TD-268\n\n- **交付状态 2026-10-11**：已合入／未发布\n\n"), 1)
	if _, _, err := Parse(data, nil, testOptions(t)); err != nil {
		t.Fatal(err)
	}
}
func TestRule03FieldsAndUnknownHeadingRetainWholeParagraph(t *testing.T) {
	r, _ := parseFixture(t, "fields.md")
	if !strings.Contains(textOf(r, "TD-268", "acceptance_text"), "TestRemovedPrimarySQLHelperStaysGone") {
		t.Fatal("plain acceptance lost")
	}
	if textOf(r, "TD-268", "fix") == "" || textOf(r, "TD-268", "claim") == "" {
		t.Fatal("English mapping lost")
	}
	if !slices.ContainsFunc(r.Texts, func(v Text) bool {
		return v.DebtID == "TD-92" && v.TextKind == "note" && v.Heading == "交付状态 2026-10-11" && strings.HasSuffix(v.Body, "未据源码合入结债。")
	}) {
		t.Fatal("unknown field lost")
	}
	for _, v := range r.Texts {
		if v.SHA256 != digest(v.Body) {
			t.Fatal("body hash mismatch")
		}
	}
}
func TestRule04ApprovalFormsAndGates(t *testing.T) {
	r, report := parseFixture(t, "approvals.md")
	if report.Counts["missing_approval"] != 0 {
		t.Fatalf("forms lost: %+v", report.Counts)
	}
	want := map[string]bool{"prod_change": false, "security": false, "cluster_write": false}
	for _, v := range r.Parts {
		if _, ok := want[v.ApplyGate]; ok {
			want[v.ApplyGate] = true
		}
	}
	for gate, found := range want {
		if !found {
			t.Errorf("missing %s", gate)
		}
	}
	r, _ = parseFixture(t, "legacy.md")
	if !slices.ContainsFunc(r.Refs, func(v Ref) bool {
		return v.DebtID == "TD-91" && v.RefKind == "waits_on_owner" && v.Covers == "start" && v.DebtDecisionID > 0
	}) {
		t.Fatal("public interface should gate starting")
	}
	for _, tc := range []struct {
		raw, gate string
		start, ok bool
	}{{"不用批", "none", false, true}, {"不需要", "none", false, true}, {"无", "none", false, true}, {"不要", "none", false, true}, {"要你批", "other", false, false}} {
		g, s, ok := classifyApproval(tc.raw)
		if g != tc.gate || s != tc.start || ok != tc.ok {
			t.Errorf("%s: %s %v %v", tc.raw, g, s, ok)
		}
	}
}
func TestRule05StateDateChicagoAndUnknown(t *testing.T) {
	r, report := parseFixture(t, "states.md")
	d := debt(t, r, "TD-85")
	if d.State != "watching" || d.Flow != "manual" || d.WatchUntil.Format(time.RFC3339) != "2026-10-09T23:59:00-05:00" || !hasFinding(report, d.DebtID, "overdue_watch") {
		t.Fatalf("watch: %+v", d)
	}
	d = debt(t, r, "TD-95")
	if d.WatchUntil == nil || !d.WatchUntil.Equal(r.Source.ImportedAt) || !hasFinding(report, d.DebtID, "undated_watch") {
		t.Fatalf("undated: %+v", d)
	}
	for _, id := range []string{"TD-448", "TD-451"} {
		if debt(t, r, id).State != "" || !hasFinding(report, id, "unmapped_state") {
			t.Fatalf("unknown %s silently mapped", id)
		}
	}
	data := bytes.Replace(fixture(t, "states.md"), []byte("到 10-09"), []byte("到 11-03"), 1)
	r, _, err := Parse(data, nil, testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	if debt(t, r, "TD-85").WatchUntil.Format(time.RFC3339) != "2026-11-03T23:59:00-06:00" {
		t.Fatal("Chicago daylight saving transition ignored")
	}
}
func TestRule06LegacyAndFreeformEvidence(t *testing.T) {
	r, _ := parseFixture(t, "legacy.md")
	var refs []Evidence
	for _, e := range r.Evidence {
		if e.DebtID == "TD-91" && e.EvidenceKind == "code_ref" {
			refs = append(refs, e)
		}
	}
	if len(refs) != 4 || refs[0].Repo != "bifrost-trade-core" || refs[0].Path != "src/bifrost_core/portfolio/reader/accounts.py" || refs[0].LineFrom != 1417 || refs[0].Quote != `logger.warning("upsert_account_transactions failed: %s", e)` {
		t.Fatalf("evidence: %+v", refs)
	}
	r, _ = parseFixture(t, "modern.md")
	if !slices.ContainsFunc(r.Evidence, func(e Evidence) bool { return e.EvidenceKind == "measurement" }) || !slices.ContainsFunc(r.Evidence, func(e Evidence) bool { return e.EvidenceKind == "code_ref" && e.Repo == "bifrost-platform" }) {
		t.Fatal("freeform evidence or extra code ref lost")
	}
	for _, e := range r.Evidence {
		if e.EvidenceKind == "measurement" && len([]rune(e.Quote)) > 400 {
			t.Fatal("oversized quote")
		}
	}
}
func TestRule07PartsOrderedProjectSuggestionsAndMissingRepos(t *testing.T) {
	r, report := parseFixture(t, "legacy.md")
	if len(r.Parts) != 2 || r.Parts[0].Ordinal != 1 || r.Parts[1].Ordinal != 2 || r.Parts[1].Repo != "bifrost-platform-plugin-flex-query" {
		t.Fatalf("parts: %+v", r.Parts)
	}
	if debt(t, r, "TD-91").Project != "trade" || !hasFinding(report, "TD-91", "cross_project") {
		t.Fatal("cross-project suggestion lost")
	}
	r, report = parseFixture(t, "modern.md")
	if !hasFinding(report, "TD-475", "inferred_repos") || debt(t, r, "TD-475").Project != "crew" {
		t.Fatal("evidence inference lost")
	}
	r, report = parseFixture(t, "states.md")
	if len(r.Parts) != 0 || !hasFinding(report, "TD-451", "missing_repos") {
		t.Fatal("missing repo must remain unknown")
	}
	if !ownsMatch("repo/k8s/overlays/{dev,stg,prod}/**", "repo/k8s/overlays/stg/x/y") || ownsMatch("repo/k8s/overlays/{dev,stg,prod}/**", "repo/k8s/overlays/other/x") {
		t.Fatal("owns glob")
	}
}
func TestRule08TagsAndUmbrellaDoNotIssueNumbers(t *testing.T) {
	r, _ := parseFixture(t, "sections.md")
	d := debt(t, r, "TD-91")
	if !slices.Contains(d.Tags, "wave:1") || !slices.Contains(d.Tags, "look-first") || !slices.Contains(d.Tags, "theme:green-while-wrong-jobs-succeed-on-zero-partial-or-garbage-output") {
		t.Fatalf("tags: %v", d.Tags)
	}
	if len(r.Umbrellas) != 1 || len(r.Edges) != 2 || r.Edges[0].FromDebtID != "TD-142" || r.Edges[1].FromDebtID != "TD-172" || r.Edges[0].ToDebtID != r.Umbrellas[0].Handle {
		t.Fatalf("umbrella: %+v %+v", r.Umbrellas, r.Edges)
	}
}
func TestRule09ImportedAcceptanceAndNoFollowup(t *testing.T) {
	r, _ := parseFixture(t, "signoffs.md")
	if len(r.Runs) != 3 {
		t.Fatalf("runs %d", len(r.Runs))
	}
	for _, id := range []string{"TD-392", "TD-384", "TD-378"} {
		if !strings.HasPrefix(textOf(r, id, "follow_up_text"), "无后续：") {
			t.Fatalf("no follow-up lost for %s", id)
		}
		if textOf(r, id, "ratchet_text") == "" || textOf(r, id, "signoff_summary") == "" {
			t.Fatal("signoff fields lost")
		}
		for _, run := range r.Runs {
			if run.DebtID == id {
				raw := ""
				for _, v := range r.Texts {
					if v.DebtID == id && v.TextKind == "import_raw" && v.Heading == "待你签收" {
						raw = v.Body
					}
				}
				if run.Runner != "imported" || run.Result != "pass" || run.SourceSHA256 != digest(raw) {
					t.Fatalf("run %+v", run)
				}
			}
		}
	}
	r, _ = parseFixture(t, "fields.md")
	if !slices.ContainsFunc(r.Runs, func(v AcceptanceRun) bool {
		return v.DebtID == "TD-92" && v.Result == "partial" && slices.Contains(v.AtCommits["unassigned"], "d628fa6")
	}) {
		t.Fatal("partial PASS or short commit lost")
	}
	for _, verdict := range []string{"PASS", "FAIL"} {
		other := "PASS"
		if verdict == "PASS" {
			other = "FAIL"
		}
		data := bytes.Replace(fixture(t, "legacy.md"), []byte("PASS（代码层）"), []byte(verdict+"（之前 "+other+"）"), 1)
		parsed, _, err := Parse(data, nil, testOptions(t))
		if err != nil {
			t.Fatal(err)
		}
		if len(parsed.Runs) != 1 || parsed.Runs[0].Result != strings.ToLower(verdict) {
			t.Fatalf("leading %s overridden by historical %s", verdict, other)
		}
	}
}
func TestRule10PendingDecisionsStaleGroupsAndMissingMembers(t *testing.T) {
	r, report := parseFixture(t, "sections.md")
	groups := 0
	for _, d := range r.Decisions {
		if !strings.HasPrefix(d.Title, "Approval direction") {
			groups++
			if d.State != "pending" {
				t.Fatal("stale group auto-dropped")
			}
		}
	}
	if groups != 2 || report.Counts["stale_decision"] < 2 {
		t.Fatalf("groups %d report %+v", groups, report.Counts)
	}
	data := bytes.Replace(fixture(t, "sections.md"), []byte("- 项：TD-91"), []byte("- 项：TD-91, TD-80"), 1)
	_, report, err := Parse(data, nil, testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	if !hasFinding(report, "TD-80", "closed_decision_member") {
		t.Fatal("closed member not reported")
	}
}
func TestRule11GapsAndCrewNotes(t *testing.T) {
	r, _ := parseFixture(t, "sections.md")
	if len(r.Gaps) != 3 {
		t.Fatal("gap count")
	}
	for _, g := range r.Gaps {
		if g.GapKind != "data_boundary" || g.State != "accepted" || g.ReopenWhen != "订阅或市场变化时重开" || g.Reason == "" {
			t.Fatalf("gap %+v", g)
		}
	}
	if len(r.ProjectNotes) != 4 {
		t.Fatalf("notes %d", len(r.ProjectNotes))
	}
	for _, n := range r.ProjectNotes {
		if n.Project != "crew" || n.Body == "" {
			t.Fatal("crew note")
		}
	}
}
func TestRule12HistoryAndLastSeen(t *testing.T) {
	r, _ := parseFixture(t, "sections.md")
	want, _ := fakeHistory{}.FirstSeen("")
	for _, d := range r.Debts {
		if d.FiledAt == nil || !d.FiledAt.Equal(want) {
			t.Fatal("filed_at")
		}
	}
	if len(r.LastSeen) != 9 {
		t.Fatalf("last seen: %d", len(r.LastSeen))
	}
	for _, v := range r.LastSeen {
		if v.Commit != "92c0271" {
			t.Fatal("last seen commit")
		}
	}
	opts := testOptions(t)
	opts.History = fakeHistory{errors.New("unavailable")}
	r, report, err := Parse(fixture(t, "legacy.md"), nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if r.Debts[0].FiledAt != nil || !hasFinding(report, "TD-91", "filed_at_unavailable") {
		t.Fatal("failed history invented date")
	}
}
func TestRule13RatchetsHistoricalRefsAndGuardedBy(t *testing.T) {
	r, report := parseFixture(t, "signoffs.md")
	if len(r.Ratchets) != 4 {
		t.Fatalf("ratchets %d", len(r.Ratchets))
	}
	for i, v := range r.Ratchets {
		if number(v.RatchetID) != i+1 {
			t.Fatal("unstable ids")
		}
	}
	if r.Ratchets[0].StrengthClass != "blocking" || r.Ratchets[3].StrengthClass != "warning" {
		t.Fatalf("strengths %+v", r.Ratchets)
	}
	if !slices.Contains(r.Ratchets[0].TDRefs, "TD-473") {
		t.Fatal("historical ref lost")
	}
	for _, ref := range r.Refs {
		if ref.RefKind == "guarded_by" && ref.DebtID == "TD-473" {
			t.Fatal("closed historical ref became an FK")
		}
	}
	if !slices.ContainsFunc(r.Refs, func(v Ref) bool { return v.RefKind == "guarded_by" && v.DebtID == "TD-384" }) {
		t.Fatal("open guarded_by lost")
	}
	if report.Counts["incomplete_ratchet_row"] != 1 || report.Counts["malformed_table_markdown"] != 1 {
		t.Fatalf("malformed source not reported %+v", report.Counts)
	}
}
func TestDeterminismLosslessExportAndStructuredEdits(t *testing.T) {
	for _, name := range []string{"legacy.md", "fields.md", "modern.md", "states.md", "approvals.md", "signoffs.md", "sections.md"} {
		t.Run(name, func(t *testing.T) {
			a, ra := parseFixture(t, name)
			b, rb := parseFixture(t, name)
			ja, _ := a.JSON()
			jb, _ := b.JSON()
			rja, _ := ra.JSON()
			rjb, _ := rb.JSON()
			if !bytes.Equal(ja, jb) || !bytes.Equal(rja, rjb) {
				t.Fatal("non-deterministic")
			}
			var decoded Request
			if err := json.Unmarshal(ja, &decoded); err != nil {
				t.Fatal(err)
			}
			assertAccounting(t, decoded, ra, fixture(t, name), fixture(t, "ratchets.md"))
		})
	}
	r, _ := parseFixture(t, "legacy.md")
	r.Debts[0].Title = "Changed title"
	r.Debts[0].State = "doing"
	r.Debts[0].WatchUntil = nil
	r.Parts[0].Repo = "example-core"
	for i := range r.Texts {
		if r.Texts[i].TextKind == "measured" {
			r.Texts[i].Body = "Changed evidence"
			break
		}
	}
	ledger, _, err := Export(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{" · Changed title**", "- **状态**：在做", "Changed evidence", "repos: example-core, bifrost-platform-plugin-flex-query"} {
		if !bytes.Contains(ledger, []byte(s)) {
			t.Fatalf("export ignored records: %s", s)
		}
	}
}

func TestExportRejectsMissingPresentationAndUnrepresentableChanges(t *testing.T) {
	if _, _, err := Export(Request{Format: Format}); err == nil {
		t.Fatal("missing source presentation silently discarded fields")
	}
	r, _ := parseFixture(t, "legacy.md")
	r.Parts[0].ApplyGate = "security"
	if _, _, err := Export(r); err == nil {
		t.Fatal("different per-part gates silently flattened into one approval")
	}
}
