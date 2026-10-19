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
