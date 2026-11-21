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

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/recorderimport"
)

func runIt(args ...string) (int, string, string) {
	var out, errs bytes.Buffer
	code := recorder(args, &out, &errs)
	return code, out.String(), errs.String()
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
	code, out, errs := runIt("-root", root, "-work", "td-339")
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
	if code, _, errs = runIt("-root", root, "-work", "td-339"); code != 1 || !strings.Contains(errs, "line 1") {
		t.Fatalf("a broken log = %d %s", code, errs)
	}
	if code, _, _ = runIt("-root", root); code != 2 {
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
	code, _, errs := runIt("-root", root, "-work", "td-big", "-o", out)
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
	if code, _, errs = runIt("-root", root, "-work", "td-big", "-o", out); code != 0 {
		t.Fatalf("a log that fits = %d %s", code, errs)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("the request that fits was not written: %v", err)
	}
}
