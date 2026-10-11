package agentprogress

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRecorderFixturesPassHandlePut runs each published fixture through the
// real PUT path: validate, redact, re-encode, and the stored-envelope size.
// RECORDER_FIXTURES unset means this test does not run, so a package-wide
// go test without that directory still passes.
func TestRecorderFixturesPassHandlePut(t *testing.T) {
	dir := os.Getenv("RECORDER_FIXTURES")
	if dir == "" {
		t.Skip("RECORDER_FIXTURES is unset")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	saw := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		saw++
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			var sent map[string]any
			if err := json.Unmarshal(raw, &sent); err != nil {
				t.Fatal(err)
			}
			h, _, _ := newRig(t, "")
			rec := put(t, h, string(raw))
			if rec.Code != 202 {
				t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
			}
			got := decodeGet(t, get(t, h))
			var back map[string]any
			if err := json.Unmarshal(got.Document, &back); err != nil {
				t.Fatalf("read back: %v body %s", err, got.Document)
			}
			if ownerCount(back) != ownerCount(sent) {
				t.Fatalf("owner_all %d, sent %d", ownerCount(back), ownerCount(sent))
			}
			if strings.Join(stepStates(back), ",") != strings.Join(stepStates(sent), ",") {
				t.Fatalf("states %v, sent %v", stepStates(back), stepStates(sent))
			}
		})
	}
	if saw == 0 {
		t.Fatal("no fixtures")
	}
}

func ownerCount(doc map[string]any) int {
	items, _ := doc["owner_all"].([]any)
	return len(items)
}

func stepStates(doc map[string]any) []string {
	var out []string
	works, _ := doc["works"].([]any)
	for _, work := range works {
		obj, _ := work.(map[string]any)
		steps, _ := obj["steps"].([]any)
		for _, step := range steps {
			entry, _ := step.(map[string]any)
			state, _ := entry["state"].(string)
			out = append(out, state)
		}
	}
	return out
}
