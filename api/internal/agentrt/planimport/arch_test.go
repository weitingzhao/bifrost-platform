package planimport_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
)

// The architecture table as the ledger writes it: a header, rows with what
// each actually is now after the first ｜ (more ｜ after it are part of it),
// a row with nothing after its title, then the next section.
const archLedger = "# ledger\n\n### W-31\n\n- **状态**：在做\n" +
	"    - **架构对照（设计 v4 → 现在，核对于 2026-10-19 01:20）**。原则：先做好工具。\n" +
	"      - [x] 发布护栏：CI 门禁｜在正式环境和集群（第 1 轮）\n" +
	"\n" +
	"      - [~] 状态库：事件只追加｜两个环境都在用｜**10-19 核对**：T19 PROD 部分没做\n" +
	"      - [ ] 推理网关\n" +
	"    - **第 2 轮逐项（核对于 2026-10-19 01:20）**。\n" +
	"      - 2.0 设计对齐\n"

const archSpec = "8c5b6d589310a295878350a310b92a13f43a7de31b159dcfacc533169eb8de78"

func gateRule(env string, tests ...string) json.RawMessage {
	var gates []string
	for _, t := range tests {
		gates = append(gates, `{"test":"`+t+`","spec_sha256":"`+archSpec+`"}`)
	}
	return json.RawMessage(`{"gates_passed":[` + strings.Join(gates, ",") + `],"env":"` + env + `"}`)
}

func archRows() []planimport.ArchRow {
	return []planimport.ArchRow{
		{Label: "release-guard", Rule: gateRule("prod", "T05")},
		{Label: "state-db", Rule: json.RawMessage(`{"all":[` + string(gateRule("prod", "T19", "T20")) + `,` + string(gateRule("stg", "T23a")) + `]}`)},
		{Label: "inference-gateway"},
	}
}

func archFixture(t *testing.T) planimport.Request {
	t.Helper()
	req, err := planimport.ParseArch(archLedger, archRows(), planimport.LedgerOptions{File: "WORK.md"})
	ok(t, err)
	return req
}

// The table read into a request: the ARCH root on its header line, each row
// titled from its line with the label and rule given beside it, its mark as
// evidence, and what it is now readable from its line.
func TestParseArch(t *testing.T) {
	req := archFixture(t)
	root := req.Root
	if root.ID != "ARCH" || root.Kind != "arch_root" || root.Label != "ARCH" || root.Title != "架构对照" || root.Ordinal != 0 ||
		root.WorkID != "W-31" || root.Evidence.LineNo != 6 {
		t.Fatalf("root = %+v", root)
	}
	s := req.Source
	if s.Section != "架构对照" || s.CheckedAt != "2026-10-19 01:20" || s.FirstLine != 6 || s.LastLine != 10 || len(s.SHA256) != 64 {
		t.Fatalf("source = %+v", s)
	}
	want := []struct{ id, title, mark, now string }{
		{"ARCH:release-guard", "发布护栏：CI 门禁", "x", "在正式环境和集群（第 1 轮）"},
		{"ARCH:state-db", "状态库：事件只追加", "~", "两个环境都在用｜**10-19 核对**：T19 PROD 部分没做"},
		{"ARCH:inference-gateway", "推理网关", " ", ""},
	}
	if len(req.Nodes) != len(want) {
		t.Fatalf("%d rows", len(req.Nodes))
	}
	for i, w := range want {
		n := req.Nodes[i]
		if n.ID != w.id || n.Kind != "arch_row" || n.Parent != "ARCH" || n.Ordinal != i || n.Title != w.title || n.Evidence.Mark != w.mark ||
			n.State != "" || n.StepKind != "" || n.WorkID != "W-31" {
			t.Errorf("row %d = %+v", i, n)
		}
		if got := planimport.ArchNow(n.Evidence.Line); got != w.now {
			t.Errorf("row %d now = %q, want %q", i, got, w.now)
		}
	}
	if string(req.Nodes[0].Rule) != string(gateRule("prod", "T05")) || req.Nodes[2].Rule != nil {
		t.Fatalf("rules = %s, %s", req.Nodes[0].Rule, req.Nodes[2].Rule)
	}
	// A rule-less row is written without one, so the request's digest is
	// what it was before rules existed for rows that have none.
	doc, err := json.Marshal(req.Nodes[2])
	ok(t, err)
	if strings.Contains(string(doc), `"rule"`) {
		t.Fatalf("a row with no rule marshals %s", doc)
	}
}

// The table and the rows given must agree, and there is one table.
func TestParseArchRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		text string
		rows []planimport.ArchRow
		want string
	}{
		"fewer rows given": {archLedger, archRows()[:2], "more rows than the 2 given"},
		"more rows given":  {archLedger, append(archRows(), planimport.ArchRow{Label: "extra"}), "the table has 3 rows, 4 given"},
		"no table":         {"# ledger\n- [x] a｜b\n", archRows(), "no 「架构对照"},
		"two tables": {strings.Replace(archLedger, "    - **第 2 轮逐项", "    - **架构对照（核对于 2026-10-19 01:21）**\n    - **第 2 轮逐项", 1),
			archRows(), "a second 「架构对照」 header"},
		"not a row":     {strings.Replace(archLedger, "      - [ ] 推理网关", "      - 推理网关", 1), archRows(), "not a row of the table"},
		"a bad label":   {archLedger, []planimport.ArchRow{{Label: "Release"}, {Label: "state-db"}, {Label: "gw"}}, "a row's label is lower-case"},
		"a manual rule": {archLedger, []planimport.ArchRow{{Label: "a", Rule: json.RawMessage(`{"manual":"by hand"}`)}, {Label: "b"}, {Label: "c"}}, "gates_passed only"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := planimport.ParseArch(c.text, c.rows, planimport.LedgerOptions{File: "WORK.md"})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v; want it to contain %q", err, c.want)
			}
		})
	}
}

// Each change breaks one rule of the table's request; Validate names it.
func TestValidateArchRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		change func(*planimport.Request)
		want   string
	}{
		"section":         {func(r *planimport.Request) { r.Source.Section = "第 2 轮逐项" }, "section is 架构对照"},
		"root id":         {func(r *planimport.Request) { r.Root.ID = "ARCH2" }, "the architecture root is ARCH"},
		"root ordinal":    {func(r *planimport.Request) { r.Root.Ordinal = 1 }, "at ordinal 0"},
		"root rule":       {func(r *planimport.Request) { r.Root.Rule = gateRule("prod", "T05") }, "root has no parent, step kind, state, step marks or rule"},
		"root line":       {func(r *planimport.Request) { r.Root.Evidence.Line = "    - **第 2 轮逐项（核对于 2026-10-19 01:20）**" }, "not the 「架构对照"},
		"root checked":    {func(r *planimport.Request) { r.Source.CheckedAt = "2026-10-19 01:21" }, "checked at"},
		"no rows":         {func(r *planimport.Request) { r.Nodes = nil }, "a request holds 1 to 500 nodes"},
		"a step":          {func(r *planimport.Request) { r.Nodes[0].Kind = "step" }, "is an architecture row"},
		"under another":   {func(r *planimport.Request) { r.Nodes[0].Parent = "R2" }, "is an architecture row"},
		"id not label":    {func(r *planimport.Request) { r.Nodes[0].ID = "ARCH:other" }, "id is ARCH:release-guard"},
		"label too long":  {func(r *planimport.Request) { r.Nodes[0].Label = strings.Repeat("a", 41) }, "at most 40 bytes"},
		"label with a :":  {func(r *planimport.Request) { r.Nodes[0].Label = "a:b" }, "lower-case words"},
		"twice":           {func(r *planimport.Request) { r.Nodes[1] = r.Nodes[0] }, "id used twice"},
		"out of order":    {func(r *planimport.Request) { r.Nodes[1].Ordinal = 2 }, "rows are numbered 0, 1"},
		"a state":         {func(r *planimport.Request) { r.Nodes[0].State = "done" }, "no step kind, state or step marks"},
		"work id":         {func(r *planimport.Request) { r.Nodes[0].WorkID = "W-30" }, "every node has the root's work id"},
		"no evidence":     {func(r *planimport.Request) { r.Nodes[0].Evidence = nil }, "carries the line"},
		"title not line":  {func(r *planimport.Request) { r.Nodes[0].Title = "别的" }, "does not say what its line says"},
		"mark not line":   {func(r *planimport.Request) { r.Nodes[0].Evidence.Mark = "~" }, "does not say what its line says"},
		"evidence added":  {func(r *planimport.Request) { r.Nodes[0].Evidence.Commits = []string{"abcdef1"} }, "does not say what its line says"},
		"lines reordered": {func(r *planimport.Request) { r.Nodes[1].Evidence.LineNo = 7 }, "in the order of their lines"},
		"line outside": {func(r *planimport.Request) {
			r.Nodes[2].Evidence.Line = strings.TrimLeft(r.Nodes[2].Evidence.Line, " ")
		}, "not inside the table's section"},
		"rule unparsed": {func(r *planimport.Request) { r.Nodes[0].Rule = json.RawMessage(`{"gates_passed":[],"env":"prod"}`) }, "invalid done rule"},
		"rule empty":    {func(r *planimport.Request) { r.Nodes[0].Rule = json.RawMessage(`{}`) }, "gates_passed only"},
		"rule null":     {func(r *planimport.Request) { r.Nodes[0].Rule = json.RawMessage(`null`) }, "invalid done rule"},
		"rule a task": {func(r *planimport.Request) {
			r.Nodes[0].Rule = json.RawMessage(`{"any":[` + string(gateRule("prod", "T05")) + `,{"task_accepted":"t_01JABCDEFGHJKMNPQRSTVWXYZ0"}]}`)
		}, "gates_passed only"},
	} {
		t.Run(name, func(t *testing.T) {
			r := archFixture(t)
			c.change(&r)
			err := r.Validate()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v; want it to contain %q", err, c.want)
			}
		})
	}
}

// A round's request carries no rule: its steps get theirs from the ledger.
func TestRoundCarriesNoRule(t *testing.T) {
	r := fixture(t)
	r.Nodes[1].Rule = gateRule("prod", "T05")
	if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "only an architecture row carries a rule") {
		t.Fatalf("a rule on a round's step: %v", err)
	}
	r = fixture(t)
	r.Root.Rule = gateRule("prod", "T05")
	if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "only an architecture row carries a rule") {
		t.Fatalf("a rule on a round: %v", err)
	}
}

// The work ledger fixture's table imports beside its round 2.
func TestParseArchFixtureLedger(t *testing.T) {
	text, err := os.ReadFile("testdata/ledger.md")
	ok(t, err)
	req, err := planimport.ParseArch(string(text), []planimport.ArchRow{{Label: "release-guard", Rule: gateRule("stg", "T05")}, {Label: "state-db"}},
		planimport.LedgerOptions{File: "WORK.md"})
	ok(t, err)
	if len(req.Nodes) != 2 || req.Nodes[1].Title != "状态库" || planimport.ArchNow(req.Nodes[1].Evidence.Line) != "两个环境都在用" {
		t.Fatalf("request = %+v", req)
	}
}
