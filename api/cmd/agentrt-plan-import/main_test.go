package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/recorderimport"
)

const ledger = "../../internal/agentrt/planimport/testdata/ledger.md"

func runIt(args ...string) (int, string, string) {
	var out, errs bytes.Buffer
	code := run(args, &out, &errs)
	return code, out.String(), errs.String()
}

// planOf is a GET /plan answer holding exactly the request's nodes.
func planOf(t *testing.T, r planimport.Request) []byte {
	t.Helper()
	var nodes []map[string]any
	for _, n := range append([]planimport.Node{r.Root}, r.Nodes...) {
		row := map[string]any{"plan_node_id": n.ID, "node_kind": n.Kind, "label": n.Label, "title": n.Title,
			"owner_step": n.Owner, "effect_step": n.Effect}
		if n.Parent != "" {
			row["parent_plan_node_id"] = n.Parent
		}
		if n.State != "" {
			row["state"] = n.State
		}
		nodes = append(nodes, row)
	}
	doc, err := json.Marshal(map[string]any{"root": r.Root.ID, "nodes": nodes})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestGenerateAndDiff(t *testing.T) {
	dir := t.TempDir()
	reqFile := filepath.Join(dir, "r2.json")
	commit := strings.Repeat("a", 40)
	code, _, errs := runIt("generate", "-ledger", ledger, "-commit", commit, "-o", reqFile)
	if code != 0 || !strings.Contains(errs, "R2: 5 items, 18 steps (9 done, 4 active, 5 planned)") {
		t.Fatalf("generate = %d, %s", code, errs)
	}
	raw, err := os.ReadFile(reqFile)
	if err != nil {
		t.Fatal(err)
	}
	var req planimport.Request
	if err = json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	if err = req.Validate(); err != nil || req.Source.Commit != commit || req.Source.File != defaultFile {
		t.Fatalf("written request: %v, source %+v", err, req.Source)
	}

	planFile := filepath.Join(dir, "plan.json")
	if err = os.WriteFile(planFile, planOf(t, req), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runIt("diff", "-ledger", ledger, "-plan", planFile)
	if code != 0 || out != "" || !strings.Contains(errs, "no differences") {
		t.Fatalf("diff of the same plan = %d %q %q", code, out, errs)
	}
	// Against the board's own reading too: equal but for the ★ it misses.
	board := "../../internal/agentrt/planimport/testdata/ledger-board.json"
	code, out, errs = runIt("diff", "-ledger", ledger, "-plan", planFile, "-board", board)
	if code != 0 || strings.Count(out, "note: step ") != 4 || strings.Contains(out, "against the board") {
		t.Fatalf("diff against the board = %d %q %q", code, out, errs)
	}
	raw, err = os.ReadFile(board)
	if err != nil {
		t.Fatal(err)
	}
	changed := filepath.Join(dir, "board.json")
	if err = os.WriteFile(changed, []byte(strings.Replace(string(raw), "迁移器：pgx 接入", "迁移器", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := runIt("diff", "-ledger", ledger, "-plan", planFile, "-board", changed); code != 1 ||
		!strings.Contains(out, "against the board: step 2.1-1: title") {
		t.Fatalf("diff against a board that reads otherwise = %d %q", code, out)
	}
	req.Nodes[1].State = planimport.StatePlanned
	if err = os.WriteFile(planFile, planOf(t, req), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := runIt("diff", "-ledger", ledger, "-plan", planFile); code != 1 || !strings.Contains(out, "step 2.0-1: state todo, want done") {
		t.Fatalf("diff of a changed plan = %d %q", code, out)
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"generate"}, {"diff", "-ledger", ledger}, {"send", "-ledger", ledger}} {
		if code, _, _ := runIt(args...); code != 2 {
			t.Errorf("%v = %d, want 2", args, code)
		}
	}
	if code, _, errs := runIt("generate", "-ledger", ledger, "-round", "5"); code != 1 || !strings.Contains(errs, "第 5 轮逐项") {
		t.Errorf("a round the ledger does not have = %d %s", code, errs)
	}
}

// recorder: a log on disk (with a torn tail, as an interrupted append
// leaves it) becomes a request that reads; a broken log exits 1 with the
// line; flags are required.
func TestRecorder(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "td-339")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"at":1791936767.5,"by":"coordinator","prev_sha256":"","seq":1,"type":"work_opened","v":2,"work_id":"td-339"}`
	sum := sha256.Sum256([]byte(body))
	line := hex.EncodeToString(sum[:]) + " " + body
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(line+"\n"+`{"torn`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runIt("recorder", "-root", root, "-work", "td-339")
	if code != 0 || !strings.Contains(errs, "1 events, chain holds") {
		t.Fatalf("recorder = %d %s", code, errs)
	}
	var req recorderimport.Request
	if err := json.Unmarshal([]byte(out), &req); err != nil || len(req.Lines) != 1 || req.Lines[0] != line || req.Source.File != "td-339/events.jsonl" {
		t.Fatalf("request %s: %v", out, err)
	}
	if _, err := recorderimport.Read(req); err != nil {
		t.Fatalf("the request does not read: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(strings.Replace(line, `"seq":1`, `"seq":2`, 1)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errs = runIt("recorder", "-root", root, "-work", "td-339"); code != 1 || !strings.Contains(errs, "line 1") {
		t.Fatalf("a broken log = %d %s", code, errs)
	}
	if code, _, _ = runIt("recorder", "-root", root); code != 2 {
		t.Fatalf("no -work = %d", code)
	}
}

// sealedLog is a recorder log of n events, each with a detail of size bytes,
// chained as the recorder's events.py chains them.
func sealedLog(n, size int) string {
	var b strings.Builder
	prev := ""
	for i := 1; i <= n; i++ {
		ev := map[string]any{"at": 1791936767.5 + float64(i), "by": "coordinator", "detail": strings.Repeat("x", size),
			"prev_sha256": prev, "seq": i, "type": "note", "v": 2, "work_id": "td-big"}
		body, err := json.Marshal(ev) // keys sorted, no spaces: the recorder's canonical form here
		if err != nil {
			panic(err)
		}
		sum := sha256.Sum256(body)
		prev = hex.EncodeToString(sum[:])
		b.WriteString(prev + " " + string(body) + "\n")
	}
	return b.String()
}

// Review 489 B2: a log whose chain holds but whose request would not pass a
// layer of the import (the 1 MiB the MCP tool reads, the route takes, the
// store keeps) is refused here, naming the layer, and nothing is written; one
// that fits is written.
func TestRecorderTooLongForTheImport(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "td-big")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "req.json")
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(sealedLog(25, 50000)), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs := runIt("recorder", "-root", root, "-work", "td-big", "-o", out)
	if code != 1 || !strings.Contains(errs, "chain holds") || !strings.Contains(errs, "over the 1048576 the MCP tool reads") ||
		!strings.Contains(errs, "nothing written") {
		t.Fatalf("a log too long = %d %s", code, errs)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("the request was written: %v", err)
	}
	if why := recorderFits(recorderimport.Request{}, recorderimport.Log{}, make([]byte, 10)); why != "" {
		t.Fatalf("an empty request does not fit: %s", why)
	}
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(sealedLog(5, 1000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errs = runIt("recorder", "-root", root, "-work", "td-big", "-o", out); code != 0 {
		t.Fatalf("a log that fits = %d %s", code, errs)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("the request that fits was not written: %v", err)
	}
}

// arch: the ledger's architecture table and the rows file become a request
// that validates; the rows must match the table; flags are required.
func TestArch(t *testing.T) {
	dir := t.TempDir()
	rows := filepath.Join(dir, "rows.json")
	spec := strings.Repeat("ab", 32)
	if err := os.WriteFile(rows, []byte(`[{"label":"release-guard","rule":{"gates_passed":[{"test":"T05","spec_sha256":"`+spec+`"}],"env":"prod"}},`+
		`{"label":"state-db"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runIt("arch", "-ledger", ledger, "-rows", rows)
	if code != 0 || !strings.Contains(errs, "ARCH: 2 rows (1 with gate rules, 1 with none yet), lines 11-13") {
		t.Fatalf("arch = %d, %s", code, errs)
	}
	var req planimport.Request
	if err := json.Unmarshal([]byte(out), &req); err != nil {
		t.Fatal(err)
	}
	if err := req.Validate(); err != nil || req.Root.ID != "ARCH" || req.Nodes[1].ID != "ARCH:state-db" || req.Nodes[1].Rule != nil {
		t.Fatalf("request %+v: %v", req, err)
	}
	if err := os.WriteFile(rows, []byte(`[{"label":"state-db"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errs = runIt("arch", "-ledger", ledger, "-rows", rows); code != 1 || !strings.Contains(errs, "more rows than the 1 given") {
		t.Fatalf("too few rows = %d %s", code, errs)
	}
	if err := os.WriteFile(rows, []byte(`[{"label":"state-db","tests":["T19"]}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errs = runIt("arch", "-ledger", ledger, "-rows", rows); code != 1 || !strings.Contains(errs, "unknown field") {
		t.Fatalf("an unknown field = %d %s", code, errs)
	}
	if code, _, _ = runIt("arch", "-ledger", ledger); code != 2 {
		t.Fatalf("no -rows = %d", code)
	}
}
