package debtimport

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestApprovalCostsSurviveParsing(t *testing.T) {
	r, _ := parseFixture(t, "annotated-approvals.md")
	for id, want := range map[string]string{"TD-293": "M", "TD-337": "L", "TD-393": "M", "TD-391": "M"} {
		if got := debt(t, r, id).Cost; got != want {
			t.Errorf("%s cost = %q, want %q", id, got, want)
		}
	}
}
func TestImpactMapsToImpact(t *testing.T) {
	r, _ := parseFixture(t, "modern.md")
	want := "规则循环的耗时和数据库读随步骤数乘以落地流长度增长；与 TD-476 同一类。"
	if got := textOf(r, "TD-475", "impact"); got != want {
		t.Fatalf("impact = %q", got)
	}
}
func TestEvidenceCommitIsStructured(t *testing.T) {
	r, _ := parseFixture(t, "evidence-regressions.md")
	if !slices.ContainsFunc(r.Evidence, func(e Evidence) bool {
		return e.DebtID == "TD-441" && e.EvidenceKind == "commit" && e.CommitSHA == "45ad51ee"
	}) {
		t.Fatal("TD-441 commit evidence 45ad51ee missing")
	}
	if !slices.ContainsFunc(r.Evidence, func(e Evidence) bool {
		return e.DebtID == "TD-441" && e.EvidenceKind == "code_ref" && e.Path == "api/internal/actions/catalog.go" && e.LineFrom == 452 && e.CommitSHA == "45ad51ee"
	}) {
		t.Fatal("commit not attached to code location")
	}
}
func TestAcceptanceDateDrivesRanAt(t *testing.T) {
	r, _ := parseFixture(t, "fields.md")
	for _, run := range r.Runs {
		if run.DebtID == "TD-92" {
			if run.RanAt.Format(time.RFC3339) != "2026-10-10T00:00:00-05:00" {
				t.Fatalf("date ignored: %s", run.RanAt)
			}
			return
		}
	}
	t.Fatal("missing acceptance run")
}
func TestRiskMedNormalizes(t *testing.T) {
	r, _ := parseFixture(t, "annotated-approvals.md")
	for id, want := range map[string]string{"TD-293": "medium", "TD-337": "medium", "TD-393": "low", "TD-391": "medium"} {
		if got := debt(t, r, id).Risk; got != want {
			t.Errorf("%s risk=%q, want %q", id, got, want)
		}
	}
}
func TestDecisionMembersHaveStartReferences(t *testing.T) {
	r, _ := parseFixture(t, "sections.md")
	for _, id := range []string{"TD-91", "TD-95"} {
		refs := []Ref{}
		for _, ref := range r.Refs {
			if ref.DebtID == id && ref.RefKind == "waits_on_owner" {
				refs = append(refs, ref)
			}
		}
		if len(refs) != 1 || refs[0].Covers != "start" {
			t.Fatalf("%s references: %+v", id, refs)
		}
		if !slices.ContainsFunc(r.Decisions, func(d Decision) bool {
			return d.DebtDecisionID == refs[0].DebtDecisionID && !strings.HasPrefix(d.Title, "Approval direction") && strings.Contains(d.BodyText, "- 项："+id)
		}) {
			t.Fatal("reference does not target its group")
		}
	}
	if len(r.Decisions) != 2 {
		t.Fatalf("duplicate group/directional decision: %d", len(r.Decisions))
	}
}
func TestAnnotatedApprovalClausesAreIndependent(t *testing.T) {
	r, _ := parseFixture(t, "annotated-approvals.md")
	for id, want := range map[string]string{"TD-293": "security", "TD-337": "rule_sign", "TD-393": "rule_sign", "TD-391": "none"} {
		parts := 0
		for _, part := range r.Parts {
			if part.DebtID == id {
				parts++
				if part.ApplyGate != want {
					t.Errorf("%s gate=%s want %s", id, part.ApplyGate, want)
				}
			}
		}
		if parts == 0 {
			t.Errorf("%s lost repos", id)
		}
		if !slices.ContainsFunc(r.Texts, func(v Text) bool {
			return v.DebtID == id && v.TextKind == "import_raw" && v.Heading == "审批" && strings.Contains(v.Body, "（")
		}) {
			t.Errorf("%s annotation lost", id)
		}
	}
}
func TestAddressesAreNotFileLineEvidence(t *testing.T) {
	r, _ := parseFixture(t, "evidence-regressions.md")
	for _, e := range r.Evidence {
		if e.EvidenceKind == "code_ref" || e.EvidenceKind == "doc_ref" {
			if e.Path == "0.1" || e.Path == "10.50" || e.LineFrom == 8780 || e.LineFrom == 8783 {
				t.Fatalf("address became a location: %+v", e)
			}
		}
	}
	for _, tc := range []struct {
		token string
		want  bool
	}{{"127.0.0.1", false}, {"0.1", false}, {"192.168.10.50", false}, {"run.go", true}, {"src/file", true}, {"README.md", true}} {
		if got := fileLike(tc.token); got != tc.want {
			t.Errorf("%s: %v", tc.token, got)
		}
	}
	// Exercise both legacy and freeform paths with an explicit repository.
	data := []byte("## 条目\n### TD-1\n**P2 · api · Address evidence**\n- **证据**：127.0.0.1:8780, 192.168.10.50:8783, run.go:42\n  - `127.0.0.1:8780` — `probe`\n- 审批 无 · 代价 S · 风险 low · repos: bifrost-platform\n")
	got, _, err := Parse(data, nil, testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range got.Evidence {
		if e.EvidenceKind == "code_ref" {
			count++
			if e.Path != "run.go" || e.LineFrom != 42 {
				t.Fatalf("bad file ref %+v", e)
			}
		}
	}
	if count != 1 {
		t.Fatalf("valid file evidence lost: %d", count)
	}
}
func TestOnlyPlainAcceptanceIsAField(t *testing.T) {
	source := "- 中间结果 2026-10-21 00:17：still running\n- 验收: check one\n- 验收：check two\n- Thing: prose\n"
	ff := fields(source, 0)
	if len(ff) != 2 || ff[0].key != "验收" || ff[1].key != "验收" || !strings.Contains(ff[1].value, "- Thing: prose") {
		t.Fatalf("plain field grammar: %+v", ff)
	}
}
func TestUnknownOptionalEnumsAreOmitted(t *testing.T) {
	source := []byte("## 条目\n### TD-1\n**P2 · test · Unknown enums**\n- 审批 unknown · 代价 enormous · 风险 severe\n")
	r, _, err := Parse(source, nil, testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := r.JSON()
	var decoded map[string]any
	if err = json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	d := decoded["debt"].([]any)[0].(map[string]any)
	for _, key := range []string{"cost", "risk", "project"} {
		if _, ok := d[key]; ok {
			t.Errorf("unknown %s was emitted", key)
		}
	}
}
func TestClosedWaveIsAccountedFor(t *testing.T) {
	source := append([]byte("## 还债顺序\n### 第 9 波 · 已完成\n项：无 · 已还：TD-400\n"), fixture(t, "legacy.md")...)
	r, report, err := Parse(source, nil, testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	if !hasFinding(report, "wave:9", "wave_membership") {
		t.Fatal("closed wave absent from report")
	}
	for _, d := range r.Debts {
		if slices.Contains(d.Tags, "wave:9") {
			t.Fatal("closed wave tagged an active debt")
		}
	}
}

func TestFloorComesFromAuditedSources(t *testing.T) {
	opts := testOptions(t)
	opts.FloorSources = []FloorSource{{Kind: "infra_history_heading", DebtID: "TD-510"}, {Kind: "main_work_trailer", DebtID: "TD-520"}, {Kind: "reserved", DebtID: "TD-530"}}
	r, report, err := Parse(fixture(t, "legacy.md"), nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if r.Floor != 530 || report.Counts["floor_source"] != 3 {
		t.Fatalf("floor=%d report=%v", r.Floor, report.Counts)
	}
}

func TestPlainAcceptanceResultUsesRuleNineOnly(t *testing.T) {
	source := []byte("## 条目\n### TD-1\n**P2 · test · Plain result**\n- 验收: run check\n- 验收结果：PASS 2026-10-19 1b894f93\n")
	r, report, err := Parse(source, nil, testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Runs) != 1 || r.Runs[0].Result != "pass" || r.Runs[0].RanAt.Format(time.RFC3339) != "2026-10-19T00:00:00-05:00" || !hasFinding(report, "TD-1", "plain_acceptance_result") {
		t.Fatal("explicit acceptance result lost")
	}
	if textOf(r, "TD-1", "acceptance_text") != "run check" {
		t.Fatal("result merged into acceptance command")
	}
}

// Section 3 of the design, written out independently of the parser's table.
var sectionThreeKinds = map[string]string{
	"Claim": "claim", "Measured": "measured", "Evidence": "measured", "Impact": "impact", "Fix": "fix", "Ratchet": "ratchet_plan",
	"证据": "measured", "影响": "impact", "修法": "fix", "防线": "ratchet_plan", "现在": "now", "下一步": "next", "验收": "acceptance_text",
}

func TestSectionThreeKeysKeepTheirKinds(t *testing.T) {
	r, report := parseFixture(t, "field-keys.md")
	want := map[string][]string{
		"TD-152": {"验收", "下一步", "现在", "Claim", "Measured", "Evidence", "Impact", "Fix", "Ratchet"},
		"TD-472": {"证据", "影响", "修法", "防线", "验收"},
	}
	for id, keys := range want {
		for _, key := range keys {
			n := 0
			for _, v := range r.Texts {
				if v.DebtID == id && v.Heading == key {
					n++
					if v.TextKind != sectionThreeKinds[key] {
						t.Errorf("%s %s stored as %s, want %s", id, key, v.TextKind, sectionThreeKinds[key])
					}
				}
			}
			if n != 1 {
				t.Errorf("%s %s: %d texts", id, key, n)
			}
		}
		if textOf(r, id, "note") != "" {
			t.Errorf("%s: a mapped key fell back to note", id)
		}
	}
	// Only the canonical state and gate renderings may differ from the source.
	if got := differenceTypes(report); !maps.Equal(got, map[string]int{"state": 2, "apply_gate": 1}) || report.Counts["raw_note"] != 0 {
		t.Fatalf("value-level loss: %v %v", got, report.Counts)
	}
}

// differenceTypes counts rendered differences by the bound column they render.
func differenceTypes(report Report) map[string]int {
	out := map[string]int{}
	for _, f := range report.Findings {
		if f.Category == "rendered_difference" && f.Source != nil && len(f.Source.Paths) > 0 {
			p := f.Source.Paths[0]
			out[p[strings.LastIndex(p, "/")+1:]]++
		}
	}
	return out
}

func TestAreaAndTitleKeepSourceCase(t *testing.T) {
	r, report := parseFixture(t, "field-keys.md")
	d := debt(t, r, "TD-472")
	if d.Area != "S5" || !strings.HasPrefix(d.Title, "准入策略文件里的 S5 段在 Kubernetes 1.35 上") {
		t.Fatalf("area/title case changed: %q %q", d.Area, d.Title)
	}
	if debt(t, r, "TD-152").Area != "ops-platform" {
		t.Fatal("area changed")
	}
	if got := differenceTypes(report); got["area"] != 0 || got["title"] != 0 || got["priority"] != 0 {
		t.Fatalf("source value rewritten: %v", got)
	}
}

func TestHostPortIsNotALocation(t *testing.T) {
	r, _ := parseFixture(t, "locations.md")
	for _, e := range r.Evidence {
		if e.DebtID == "TD-467" && (strings.Contains(e.Path, "local") || e.LineFrom == 5000 || e.LineFrom == 30500) {
			t.Fatalf("host:port became a location: %+v", e)
		}
	}
	if !slices.ContainsFunc(r.Evidence, func(e Evidence) bool {
		return e.DebtID == "TD-467" && e.EvidenceKind == "code_ref" && e.Repo == "bifrost-platform" && e.Path == "api/internal/agentrt/landingci/s5.go" && e.LineFrom == 51
	}) {
		t.Fatal("real file location lost next to the host")
	}
	for _, tc := range []struct {
		token string
		want  bool
	}{{"registry.cicd.svc.cluster.local", false}, {"cluster.local", false}, {"bifrost-postgres-rw.data.svc", false}, {"ops.bifrost.lan", false},
		{"jquery.min.js", true}, {"s5-run-template.json", true}, {"Dockerfile.dev", true}, {"k8s/cicd/tekton", true}} {
		if got := fileLike(tc.token); got != tc.want {
			t.Errorf("%s: %v", tc.token, got)
		}
	}
}

func TestWorkspacePathKeepsLineAndRepo(t *testing.T) {
	r, report := parseFixture(t, "locations.md")
	lines := []int{}
	for _, e := range r.Evidence {
		if e.DebtID != "TD-183" || (e.EvidenceKind != "code_ref" && e.EvidenceKind != "doc_ref") {
			continue
		}
		if e.EvidenceKind != "doc_ref" || e.Repo != "" || e.Path != "/stocks/REQUEST-w3-archive-before-delete-2026-10-05.md" {
			t.Fatalf("workspace document mapped into the debt's repository: %+v", e)
		}
		lines = append(lines, e.LineFrom)
	}
	if !slices.Equal(lines, []int{20, 21}) || report.Counts["workspace_path_outside_repos"] != 2 {
		t.Fatalf("lines %v, report %v", lines, report.Counts)
	}
	// A workspace path naming a repository maps into it, legacy or free form.
	source := []byte("## 条目\n### TD-1\n**P2 · test · Workspace paths**\n- **证据**：见 `/stocks/bifrost-trade-infra/k8s/a.yaml:3`，http://127.0.0.1:8780\n  - `/stocks/bifrost-platform/api/x.go:12-14` — `quote`\n- 审批 无 · 代价 S · 风险 low · repos: bifrost-research\n")
	got, _, err := Parse(source, nil, testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	refs := []string{}
	for _, e := range got.Evidence {
		if e.EvidenceKind == "code_ref" || e.EvidenceKind == "doc_ref" {
			refs = append(refs, fmt.Sprintf("%s %s %s:%d-%d", e.EvidenceKind, e.Repo, e.Path, e.LineFrom, e.LineTo))
		}
	}
	if !slices.Equal(refs, []string{"code_ref bifrost-platform api/x.go:12-14", "code_ref bifrost-trade-infra k8s/a.yaml:3-3"}) {
		t.Fatalf("workspace refs: %q", refs)
	}
}

func TestTemplateLabelsAreLayoutNotText(t *testing.T) {
	for _, s := range []string{"**\n\n- **状态**：", "\n- 审批 ", " · 风险 ", " · repos: ", "· 代价 ", "\n- 验收结果：", "\n- 审批：", "验收：", "防线：", "** — "} {
		if !layoutOnly(s) {
			t.Errorf("%q is template layout", s)
		}
	}
	for _, s := range []string{"有风险", "状态 changed", "（STG 只）· repos: ", "hidden narrative", "TD-155"} {
		if layoutOnly(s) {
			t.Errorf("%q is prose", s)
		}
	}
	r, _ := parseFixture(t, "annotated-approvals.md")
	for _, v := range r.Texts {
		if v.Heading == "Unmapped source" && !slices.Contains([]string{"（漏一个名字会让 platform 写记录 403）", "（W-56 的剩余门槛）", "（STG 只）", "（额度、注入）"}, v.Body) {
			t.Errorf("layout stored as text: %q", v.Body)
		}
	}
	r, _ = parseFixture(t, "orphan-prose.md")
	if got := textOf(r, "TD-445", "import_raw"); !strings.HasPrefix(got, "（platform `3aa76022`") || !strings.HasSuffix(got, "撞号后改为 TD-445。）") {
		t.Fatalf("renumbering paragraph: %q", got)
	}
}

func TestUnmappedValuesAreOmittedNotEmpty(t *testing.T) {
	r, _ := parseFixture(t, "states.md")
	data, _ := r.JSON()
	var decoded struct {
		Debt []map[string]any `json:"debt"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, d := range decoded.Debt {
		if v, ok := d["state"]; ok && v == "" {
			t.Fatalf("%v: empty state emitted", d["debt_id"])
		}
	}
	data, _ = json.Marshal(Ratchet{RatchetID: "R-1", Name: "n", Location: "l", Guards: "g", StrengthClass: "other", State: "active"})
	if strings.Contains(string(data), "strength\"") || strings.Contains(string(data), "scope_gaps") {
		t.Fatalf("empty ratchet cells emitted: %s", data)
	}
}
