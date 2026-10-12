package agentprogress

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

// fixturesDir is RECORDER_FIXTURES. go test runs in this package directory,
// while the acceptance command sets a path relative to api/.
func fixturesDir() string {
	dir := os.Getenv("RECORDER_FIXTURES")
	if dir == "" {
		return ""
	}
	if _, err := os.Stat(dir); err == nil {
		return dir
	}
	alt := filepath.Join("..", "..", dir)
	if _, err := os.Stat(alt); err == nil {
		return alt
	}
	return dir
}

// TestRecorderFixturesPassHandlePut runs each published fixture through the
// real PUT path: validate, redact, re-encode, and the stored-envelope size.
// RECORDER_FIXTURES unset means this test does not run, so a package-wide
// go test without that directory still passes.
func TestRecorderFixturesPassHandlePut(t *testing.T) {
	dir := fixturesDir()
	if dir == "" {
		fmt.Println("SKIP TestRecorderFixturesPassHandlePut: RECORDER_FIXTURES is unset")
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

// TestRecorderRedactionBoundCoversEveryRule fails when a redaction rule can
// add more bytes than the recorder's RULE_MAX_GROWTH, or when a document the
// recorder would accept at that bound is refused by HandlePut.
func TestRecorderRedactionBoundCoversEveryRule(t *testing.T) {
	dir := fixturesDir()
	if dir == "" {
		fmt.Println("SKIP TestRecorderRedactionBoundCoversEveryRule: RECORDER_FIXTURES is unset")
		t.Skip("RECORDER_FIXTURES is unset")
	}
	noticeSrc, err := os.ReadFile(filepath.Join("..", "notice", "redact.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(noticeSrc), "regexp.MustCompile(") != 7 {
		t.Fatalf("redaction rules %d, recorder bound has 7", strings.Count(string(noticeSrc), "regexp.MustCompile("))
	}
	py, err := os.ReadFile(filepath.Join(filepath.Dir(dir), "redact_bound.py"))
	if err != nil {
		t.Fatal(err)
	}
	growth := ruleGrowth(t, string(py))
	model, err := os.ReadFile(filepath.Join(filepath.Dir(dir), "model.py"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(model), "PLATFORM_ENVELOPE_BYTES = 134") {
		t.Fatal("recorder envelope is not 134")
	}
	samples := []struct {
		in  string
		max int
	}{
		{"-----BEGIN PRIVATE KEY-----", growth[0]},
		{"Basic aaaaaaaa", growth[1]},
		{"pwd=x", growth[2]},
		{"pwd=x,", growth[2]},
		{"xoxa-" + strings.Repeat("a", 10), growth[5]},
		{"://a:b@", growth[2]*2 + growth[6]},
	}
	for _, sample := range samples {
		got := len(notice.Redact(sample.in)) - len(sample.in)
		if got > sample.max {
			t.Fatalf("%q grew %d, bound %d", sample.in, got, sample.max)
		}
	}
	const envelope = 134
	room := int(MaxDocumentBytes) - envelope
	for _, pattern := range []string{"pwd=x,", "Basic aaaaaaaa ", "xoxa-" + strings.Repeat("a", 10) + " ", "://a:b@"} {
		body := documentAtBound(t, pattern, room)
		h, _, _ := newRig(t, "")
		rec := put(t, h, body)
		if rec.Code != 202 {
			t.Fatalf("%q status %d body %s", pattern, rec.Code, rec.Body.String())
		}
	}
}

func ruleGrowth(t *testing.T, source string) []int {
	t.Helper()
	const marker = "RULE_MAX_GROWTH = ("
	start := strings.Index(source, marker)
	if start < 0 {
		t.Fatal("RULE_MAX_GROWTH missing")
	}
	rest := source[start+len(marker):]
	end := strings.Index(rest, ")")
	if end < 0 {
		t.Fatal("RULE_MAX_GROWTH unclosed")
	}
	var out []int
	for _, part := range strings.Split(rest[:end], ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	if len(out) != 7 {
		t.Fatalf("growth entries %d", len(out))
	}
	return out
}

func documentAtBound(t *testing.T, pattern string, room int) string {
	t.Helper()
	per := boundOf(pattern)
	if per < 1 {
		t.Fatalf("pattern %q does not grow", pattern)
	}
	prefix := `{"at":1,"pad":"`
	suffix := `"}`
	// Largest repeat whose encoded length plus the string bound still fits.
	// Structural JSON punctuation is not part of the bound.
	n := 0
	for {
		nextLen := len(prefix) + len(pattern)*(n+1) + len(suffix)
		if nextLen+(n+1)*per > room {
			break
		}
		n++
		if n > 200000 {
			t.Fatal("unbounded pattern")
		}
	}
	if n < 100 {
		t.Fatalf("pattern %q only reached %d repeats", pattern, n)
	}
	body := prefix + strings.Repeat(pattern, n) + suffix
	if len(body)+n*per > room {
		t.Fatalf("overshot room with %q", pattern)
	}
	return body
}

func boundOf(text string) int {
	eq := strings.Count(text, "=") + strings.Count(text, ":")
	at := strings.Count(text, "@")
	slack := strings.Count(text, "xoxa-") + strings.Count(text, "xoxb-") + strings.Count(text, "xoxp-") + strings.Count(text, "xoxr-")
	basic := strings.Count(strings.ToLower(text), "basic") + strings.Count(strings.ToLower(text), "bearer")
	return 9*eq + 9*at + slack + 2*basic
}
