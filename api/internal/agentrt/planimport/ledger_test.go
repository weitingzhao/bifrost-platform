package planimport_test

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
)

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) planimport.Request {
	t.Helper()
	text, err := os.ReadFile("testdata/ledger.md")
	ok(t, err)
	req, err := planimport.ParseLedger(string(text), 2, planimport.LedgerOptions{File: "WORK.md"})
	ok(t, err)
	return req
}

// The board's own reading of the fixture: testdata/ledger-board.json was
// written by roadmap.round_of (~/agent-work/status/roadmap.py), the parser the
// board's plan panel uses today, through a script that prints its items and
// steps in View's shape. The request reads the same items, ids, titles,
// states and Owner marks; it reads more ★ marks than the board (a ★ after
// 「（补强）」), and only those.
func TestParseMatchesBoard(t *testing.T) {
	compareBoard(t, fixture(t), "testdata/ledger-board.json", []string{"2.1-4", "2.3-1", "2.3-2", "2.4-2"})
}

// The same against the real ledger, when it is named: AGENTRT_PLAN_LEDGER is
// WORK.md, AGENTRT_PLAN_BOARD the board's reading of it (as above), and
// AGENTRT_PLAN_EXTRA_STARS the step ids the board misses a ★ on, comma
// separated.
func TestParseRealLedgerMatchesBoard(t *testing.T) {
	path, board := os.Getenv("AGENTRT_PLAN_LEDGER"), os.Getenv("AGENTRT_PLAN_BOARD")
	if path == "" || board == "" {
		t.Skip("set AGENTRT_PLAN_LEDGER and AGENTRT_PLAN_BOARD to compare with the real ledger")
	}
	text, err := os.ReadFile(path)
	ok(t, err)
	req, err := planimport.ParseLedger(string(text), 2, planimport.LedgerOptions{File: "WORK.md"})
	ok(t, err)
	var extra []string
	if s := os.Getenv("AGENTRT_PLAN_EXTRA_STARS"); s != "" {
		extra = strings.Split(s, ",")
	}
	compareBoard(t, req, board, extra)
}

func compareBoard(t *testing.T, req planimport.Request, board string, extraStars []string) {
	t.Helper()
	raw, err := os.ReadFile(board)
	ok(t, err)
	var want planimport.View
	ok(t, json.Unmarshal(raw, &want))
	got := planimport.FromRequest(req)
	stars := planimport.BoardMissesStars(want, &got)
	if d := planimport.Diff(want, got); len(d) > 0 {
		t.Fatalf("the request differs from the board's reading:\n%s", strings.Join(d, "\n"))
	}
	if !same(stars, extraStars) {
		t.Fatalf("★ the board misses: %v, want %v", stars, extraStars)
	}
}

func TestParseFixture(t *testing.T) {
	req := fixture(t)
	if req.Root.ID != "R2" || req.Root.Label != "2" || req.Root.Ordinal != 2 || req.Root.WorkID != "W-31" || req.Root.Title != "第 2 轮" {
		t.Fatalf("root = %+v", req.Root)
	}
	s := req.Source
	if s.FirstLine != 14 || s.LastLine != 39 || s.CheckedAt != "2026-10-19 01:20" || s.Section != "第 2 轮逐项" || len(s.SHA256) != 64 {
		t.Fatalf("source = %+v", s)
	}
	nodes := map[string]planimport.Node{}
	for _, n := range req.Nodes {
		nodes[n.ID] = n
	}
	if len(req.Nodes) != 5+18 {
		t.Fatalf("%d nodes, want 5 items and 18 steps", len(req.Nodes))
	}
	item := nodes["R2:2.1"]
	if item.Title != "数据层：状态库" || item.Evidence.Note != "v5 §5.1；Claude 写、Codex 复核" || item.Ordinal != 1 {
		t.Fatalf("item 2.1 = %+v %+v", item, item.Evidence)
	}
	if nodes["R2:2.2b"].Ordinal != 2 || nodes["R2:2.3"].Ordinal != 3 {
		t.Fatal("item ordinals are their places")
	}
	cases := []struct {
		id, kind, state, marked string
		owner, effect           bool
		commits, approvals      []string
		reviews                 []string
	}{
		{id: "R2:2.0-1", kind: "agent", state: "done", marked: "2026-10-14 00:30", commits: []string{"1de0ce5"}},
		{id: "R2:2.0-2", kind: "review", state: "done", commits: []string{"7ac856c", "0a42b1f"}, reviews: []string{"141", "142"}},
		{id: "R2:2.0-3", kind: "owner", state: "done", marked: "2026-10-14 00:30", owner: true},
		{id: "R2:2.1-1", kind: "agent", state: "done", marked: "2026-10-14 01:09"},
		{id: "R2:2.1-2", kind: "review", state: "planned", reviews: []string{"145"}},
		{id: "R2:2.1-3", kind: "owner", state: "active", marked: "2026-10-14 11:06", owner: true, approvals: []string{"#16", "#22"}},
		{id: "R2:2.1-4", kind: "agent", state: "planned", effect: true},
		{id: "R2:2.1-5", kind: "agent", state: "done", marked: "2026-10-13 22:58", effect: true},
		{id: "R2:2.2b-1", kind: "owner", state: "done", marked: "2026-10-18 11:59", owner: true, commits: []string{"9872f36"}},
		{id: "R2:2.2b-2", kind: "agent", state: "active"},
		{id: "R2:2.2b-3", kind: "agent", state: "active", reviews: []string{"263", "264", "265", "266", "267", "268", "269", "270"}},
		{id: "R2:2.2b-4", kind: "agent", state: "done"},
		{id: "R2:2.2b-5", kind: "owner", state: "done", marked: "2026-10-14 15:49", owner: true},
		{id: "R2:2.3-1", kind: "agent", state: "done", marked: "2026-10-18 12:09", effect: true},
		{id: "R2:2.3-2", kind: "agent", state: "planned", effect: true},
		// Indented with U+00A0 and U+3000, as the board reads it; 「（你）」
		// after another mark is not the Owner's step (the board's rule); a
		// number of the wrong length is left out, not cut short.
		{id: "R2:2.4-1", kind: "agent", state: "planned", reviews: []string{"235", "236"}},
		{id: "R2:2.4-2", kind: "agent", state: "active", effect: true, reviews: []string{"263", "264", "265", "266", "267", "268", "269", "270"}},
	}
	for _, c := range cases {
		n, found := nodes[c.id]
		if !found {
			t.Fatalf("no node %s", c.id)
		}
		e := n.Evidence
		if n.StepKind != c.kind || n.State != c.state || n.Owner != c.owner || n.Effect != c.effect || e.MarkedAt != c.marked ||
			!same(e.Commits, c.commits) || !same(e.Approvals, c.approvals) || !same(e.Reviews, c.reviews) {
			t.Errorf("%s = kind %s state %s owner %v effect %v marked %q commits %v approvals %v reviews %v; want %+v",
				c.id, n.StepKind, n.State, n.Owner, n.Effect, e.MarkedAt, e.Commits, e.Approvals, e.Reviews, c)
		}
		if !strings.Contains(e.Line, strings.TrimLeft(n.Title, "★")) || e.Mark == "" {
			t.Errorf("%s: evidence line %q does not hold its title", c.id, e.Line)
		}
	}
}

func same(a, b []string) bool { return len(a) == 0 && len(b) == 0 || reflect.DeepEqual(a, b) }

// The parse is the same every time, and the section's digest follows its
// text only.
func TestParseIsStable(t *testing.T) {
	a, b := fixture(t), fixture(t)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("two parses of one ledger differ")
	}
	text, err := os.ReadFile("testdata/ledger.md")
	ok(t, err)
	moved := strings.Replace(string(text), "- **状态**：已验收", "- **状态**：已验收（改了这一行）", 1)
	c, err := planimport.ParseLedger(moved, 2, planimport.LedgerOptions{File: "WORK.md"})
	ok(t, err)
	if c.Source.SHA256 != a.Source.SHA256 {
		t.Fatal("a line outside the section changed the section's digest")
	}
	changed := strings.Replace(string(text), "只有标题没有竖线的一步", "只有标题没有竖线的一步。", 1)
	d, err := planimport.ParseLedger(changed, 2, planimport.LedgerOptions{File: "WORK.md"})
	ok(t, err)
	if d.Source.SHA256 == a.Source.SHA256 {
		t.Fatal("a changed step left the section's digest as it was")
	}
}

func TestParseRefusals(t *testing.T) {
	head := "### W-31\n    - **第 2 轮逐项（核对于 2026-10-19 01:20）**。\n"
	for name, c := range map[string]struct{ text, want string }{
		"no header":        {"### W-31\n- nothing\n", "has no 「第 2 轮逐项"},
		"two headers":      {head + head, "a second 「第 2 轮逐项」 header"},
		"step before item": {head + "      - [x] 一步｜10-14\n", "a step before any item"},
		"not a list line":  {head + "      - 2.1 项\n        续行文字\n", "neither an item nor a step"},
		"item of another":  {head + "      - 3.1 别的轮\n        - [x] 一步\n", "an item's label is 2.<n>"},
		"empty item":       {head + "      - 2.1\n", "title is empty"},
		"no steps at all":  {head + "      - 剩余：没有\n", "a request holds 1 to 500 nodes"},
		"same item twice":  {head + "      - 2.1 项\n      - 2.1 项\n", "id used twice"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := planimport.ParseLedger(c.text, 2, planimport.LedgerOptions{File: "WORK.md"})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v; want it to contain %q", err, c.want)
			}
		})
	}
	// Another round's header is not this round's.
	if _, err := planimport.ParseLedger(head+"      - 2.1 项\n        - [x] 一步\n", 3, planimport.LedgerOptions{File: "WORK.md"}); err == nil {
		t.Fatal("round 3 read from a ledger that has only round 2")
	}
}

// References are whole numbers or nothing (review 310 A7), and a list or a
// range after 「复核」 is read through (review 310 B4).
func TestReferences(t *testing.T) {
	head := "### W-31\n    - **第 2 轮逐项（核对于 2026-10-19 01:20）**\n      - 2.1 项\n"
	for _, c := range []struct {
		text               string
		approvals, reviews []string
	}{
		{"审批单 #1234567 与 #41、#41", []string{"#41"}, nil},
		{"#123456 #0", []string{"#123456", "#0"}, nil},
		{"复核 12345 不是编号", nil, nil},
		{"复核 235、236 和复核 145", nil, []string{"235", "236", "145"}},
		{"复核 263–270，都收线", nil, []string{"263", "264", "265", "266", "267", "268", "269", "270"}},
		{"复核 286、289、291、292–294 等", nil, []string{"286", "289", "291", "292", "293", "294"}},
		{"复核 100–200 太长只记两头", nil, []string{"100", "200"}},
		{"复核 12–，后面没有", nil, []string{"12"}},
		{"复核 270–263 倒着写", nil, []string{"270", "263"}},
		{"复核（两轮）", nil, nil},
	} {
		req, err := planimport.ParseLedger(head+"        - [x] 一步｜"+c.text+"\n", 2, planimport.LedgerOptions{File: "WORK.md"})
		ok(t, err)
		e := req.Nodes[1].Evidence
		if !same(e.Approvals, c.approvals) || !same(e.Reviews, c.reviews) {
			t.Errorf("%q: approvals %v reviews %v; want %v %v", c.text, e.Approvals, e.Reviews, c.approvals, c.reviews)
		}
	}
}

// A reference list that is not complete says so (review 311 B2): more than
// 20 of a kind, or a range too long to count out.
func TestReferenceBudget(t *testing.T) {
	head := "### W-31\n    - **第 2 轮逐项（核对于 2026-10-19 01:20）**\n      - 2.1 项\n"
	hexes := func(n int) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, " `abcdef%x`", 0x10+i)
		}
		return b.String()
	}
	for _, c := range []struct {
		text    string
		commits int
		reviews []string
		cut     []string
	}{
		{"提交" + hexes(20), 20, nil, nil},
		{"提交" + hexes(21), 20, nil, []string{"commits"}},
		{"提交" + hexes(20) + " `abcdef10` 重复的不算", 20, nil, nil},
		{"复核 100–120", 0, []string{"100", "120"}, []string{"reviews"}},
		{"复核 100–119", 0, nil, nil},
		{"复核 10、11、12、13、14、15、16、17、18、19、20、21、22、23、24、25、26、27 和 30–35", 0, nil, []string{"reviews"}},
	} {
		req, err := planimport.ParseLedger(head+"        - [x] 一步｜"+c.text+"\n", 2, planimport.LedgerOptions{File: "WORK.md"})
		ok(t, err)
		e := req.Nodes[1].Evidence
		if len(e.Commits) != c.commits || c.reviews != nil && !same(e.Reviews, c.reviews) || !same(e.Cut, c.cut) {
			t.Errorf("%.40q: %d commits, reviews %v, cut %v; want %d, %v, %v", c.text, len(e.Commits), e.Reviews, e.Cut, c.commits, c.reviews, c.cut)
		}
		if c.text == "复核 100–119" && len(e.Reviews) != 20 {
			t.Errorf("a range of 20 is counted out: %v", e.Reviews)
		}
	}
}
