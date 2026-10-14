package questions

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestPrintVectors(t *testing.T) {
	if os.Getenv("PRINT_VECTORS") != "1" {
		t.Skip()
	}
	type in struct {
		Name    string
		Input   any
		Session string
	}
	cases := []in{
		{"trim", map[string]any{"title": "  Hello  ", "urgency": "  now  ", "meanwhile": " wait "}, ""},
		{"missing body", map[string]any{"title": "Hello", "urgency": "now", "meanwhile": "wait"}, ""},
		{"null body", map[string]any{"title": "Hello", "body": nil, "urgency": "now", "meanwhile": "wait"}, ""},
		{"empty body", map[string]any{"title": "Hello", "body": "  ", "urgency": "now", "meanwhile": "wait"}, ""},
		{"crlf", map[string]any{"title": "Hello", "body": "a\r\nb\r", "urgency": "now", "meanwhile": "wait"}, ""},
		{"zero width", map[string]any{"title": "Hello\u200b", "urgency": "now", "meanwhile": "wait"}, ""},
		{"defaults", map[string]any{"title": "Hello", "urgency": "now", "meanwhile": "wait"}, ""},
		{"recommended", map[string]any{"title": "Hello", "urgency": "now", "meanwhile": "wait", "options": []any{map[string]any{"label": " Yes "}, map[string]any{"label": "No", "detail": ""}}, "recommended": 2}, ""},
		{"recommended zero", map[string]any{"title": "Hello", "urgency": "now", "meanwhile": "wait", "recommended": 0}, ""},
		{"recommended omitted", map[string]any{"title": "Hello", "urgency": "now", "meanwhile": "wait"}, ""},
		{"chinese", map[string]any{"title": "额度在哪里", "urgency": "今天", "meanwhile": "等待"}, ""},
		{"script", map[string]any{"title": "<script>", "urgency": "u", "meanwhile": "m"}, ""},
		{"default key", map[string]any{"title": "Hello", "urgency": "now", "meanwhile": "wait", "work_id": "W-65"}, "thread-a"},
		{"out of range", map[string]any{"title": "Hello", "urgency": "now", "meanwhile": "wait", "options": []any{map[string]any{"label": "Yes"}, map[string]any{"label": "No"}}, "recommended": 9}, ""},
		{"nbsp bearer", map[string]any{"title": "bearer\u00a0abcdefgh", "urgency": "now", "meanwhile": "wait"}, "thread-a"},
		{"unicode spaces", map[string]any{"title": "bearer\u2003abcdefgh\u3000ijklmno\u1680pqrstuvw\u202fxyz", "urgency": "now", "meanwhile": "wait"}, ""},
		{"zero width joins token", map[string]any{"title": "token=\u200bs3cr3tvalue", "urgency": "now", "meanwhile": "wait"}, ""},
		{"zero width is not space", map[string]any{"title": "bearer\u200babcdefgh", "urgency": "now", "meanwhile": "wait"}, ""},
		{"mixed case", map[string]any{"title": "BeArEr AbCdEfGh", "urgency": "now", "meanwhile": "wait"}, ""},
		{"secret at start", map[string]any{"title": "bearer abcdefgh", "urgency": "now", "meanwhile": "wait"}, "thread-a"},
		{"secret at end", map[string]any{"title": "note token=s3cr3tvalue", "urgency": "now", "meanwhile": "wait"}, ""},
		{"nbsp inside value", map[string]any{"title": "token=s3cr3t\u00a0value", "urgency": "now", "meanwhile": "wait"}, ""},
		{"word edge", map[string]any{"title": "mybearer abcdefgh", "urgency": "now", "meanwhile": "wait"}, ""},
		// decision cards (design v5 §7 item 3; review 172 A1, B3)
		{"card", map[string]any{"title": "Hello", "urgency": "now", "meanwhile": "wait", "owner_duty": "rule", "task_id": "t_01M4XW5Z7AC7CTA9P0YDGACGPG"}, "thread-a"},
		{"card other task", map[string]any{"title": "Hello", "urgency": "now", "meanwhile": "wait", "owner_duty": "rule", "task_id": "t_01M4XW5Z7SC6YN6KEN3JXF8CFV"}, "thread-a"},
		{"card goal", map[string]any{"title": "Hello", "urgency": "now", "meanwhile": "wait", "owner_duty": " goal "}, ""},
		{"card irreversible", map[string]any{"title": "Hello", "urgency": "now", "meanwhile": "wait", "owner_duty": "irreversible"}, ""},
		{"card task only", map[string]any{"title": "Hello", "urgency": "now", "meanwhile": "wait", "task_id": "t_71M4XW5Z7AC7CTA9P0YDGACGPG"}, ""},
		{"card empty", map[string]any{"title": "Hello", "urgency": "now", "meanwhile": "wait", "owner_duty": "  ", "task_id": nil}, ""},
		{"card bad duty", map[string]any{"title": "Hello", "urgency": "now", "meanwhile": "wait", "owner_duty": "approve"}, ""},
		{"card duty not a string", map[string]any{"title": "Hello", "urgency": "now", "meanwhile": "wait", "owner_duty": 3}, ""},
		{"card task over 128 bits", map[string]any{"title": "Hello", "urgency": "now", "meanwhile": "wait", "task_id": "t_81M4XW5Z7AC7CTA9P0YDGACGPG"}, ""},
		{"card task lower case", map[string]any{"title": "Hello", "urgency": "now", "meanwhile": "wait", "task_id": "t_01m4xw5z7ac7cta9p0ydgacgpg"}, ""},
		{"card task short", map[string]any{"title": "Hello", "urgency": "now", "meanwhile": "wait", "task_id": "t_01M4XW5Z7AC7CTA9P0YDGACGP"}, ""},
		{"card task with I", map[string]any{"title": "Hello", "urgency": "now", "meanwhile": "wait", "task_id": "t_01M4XW5Z7AC7CTA9P0YDGACGPI"}, ""},
	}
	var out []map[string]any
	for _, c := range cases {
		raw := mustJSON(t, c.Input)
		got, err := CanonicalizeObjectJSON(raw)
		row := map[string]any{"name": c.Name, "input": c.Input}
		if err != nil {
			fe := err.(*fieldError)
			row["error_field"] = fe.Field
		} else {
			row["canonical"] = got.JSON
			row["sha256"] = got.RequestFP
			if c.Session != "" {
				row["session"] = c.Session
				row["idempotency_key"] = DefaultKey(c.Session, got.JSON)
			}
		}
		out = append(out, row)
	}
	// The redaction section of the file is kept as it is.
	path := filepath.Join("testdata", "canonical", "vectors.json")
	var existing struct {
		Redact json.RawMessage `json:"redact"`
	}
	if prev, err := os.ReadFile(path); err == nil {
		if err = json.Unmarshal(prev, &existing); err != nil {
			t.Fatal(err)
		}
	}
	doc := map[string]any{"vectors": out}
	if len(existing.Redact) > 0 {
		doc["redact"] = existing.Redact
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Log("wrote", path)
}

func TestCanonicalVectors(t *testing.T) {
	path := filepath.Join("testdata", "canonical", "vectors.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var file struct {
		Vectors []struct {
			Name      string          `json:"name"`
			Input     json.RawMessage `json:"input"`
			Session   string          `json:"session,omitempty"`
			Canonical string          `json:"canonical,omitempty"`
			SHA256    string          `json:"sha256,omitempty"`
			Key       string          `json:"idempotency_key,omitempty"`
			Error     string          `json:"error_field,omitempty"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Vectors) < 8 {
		t.Fatalf("vectors = %d, want the cases from the design", len(file.Vectors))
	}
	hexKey := regexp.MustCompile(`^[0-9a-f]{64}$`)
	seen := map[string]struct {
		canonical string
		key       string
	}{}
	for _, v := range file.Vectors {
		t.Run(v.Name, func(t *testing.T) {
			got, err := CanonicalizeObjectJSON(v.Input)
			if v.Error != "" {
				fe, ok := err.(*fieldError)
				if !ok || fe.Field != v.Error {
					t.Fatalf("error = %v, want field %s", err, v.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.JSON != v.Canonical {
				t.Fatalf("canonical\n got %s\nwant %s", got.JSON, v.Canonical)
			}
			if got.RequestFP != v.SHA256 {
				t.Fatalf("sha256 = %s, want %s", got.RequestFP, v.SHA256)
			}
			if v.Session != "" {
				key := DefaultKey(v.Session, got.JSON)
				if !hexKey.MatchString(key) {
					t.Fatalf("default key %q is not 64 lowercase hex", key)
				}
				if key != v.Key {
					t.Fatalf("default key = %s, want %s", key, v.Key)
				}
			}
		})
		if v.Error == "" {
			got, err := CanonicalizeObjectJSON(v.Input)
			if err != nil {
				t.Fatal(err)
			}
			key := ""
			if v.Session != "" {
				key = DefaultKey(v.Session, got.JSON)
			}
			seen[v.Name] = struct {
				canonical string
				key       string
			}{got.JSON, key}
		}
	}
	nbsp := seen["nbsp bearer"]
	space := seen["secret at start"]
	if nbsp.canonical == "" || space.canonical == "" {
		t.Fatal("redaction boundary vectors are missing")
	}
	if !contains(nbsp.canonical, "bearer\u00a0abcdefgh") || contains(nbsp.canonical, "[redacted]") {
		t.Fatalf("nbsp bearer canonical = %s", nbsp.canonical)
	}
	if !contains(space.canonical, "bearer [redacted]") {
		t.Fatalf("ascii bearer canonical = %s", space.canonical)
	}
	if nbsp.key == "" || nbsp.key == space.key {
		t.Fatalf("nbsp key %s and ascii-space key %s must both exist and differ", nbsp.key, space.key)
	}
	for _, name := range []string{"unicode spaces", "zero width joins token", "zero width is not space", "mixed case", "secret at end", "nbsp inside value", "word edge"} {
		if seen[name].canonical == "" {
			t.Fatalf("missing redaction vector %s", name)
		}
	}
	if contains(seen["unicode spaces"].canonical, "[redacted]") || !contains(seen["unicode spaces"].canonical, "\u2003") {
		t.Fatalf("unicode spaces canonical = %s", seen["unicode spaces"].canonical)
	}
	if !contains(seen["zero width joins token"].canonical, "token=[redacted]") || contains(seen["zero width joins token"].canonical, "s3cr3tvalue") {
		t.Fatalf("zero width canonical = %s", seen["zero width joins token"].canonical)
	}
	if !contains(seen["zero width is not space"].canonical, "bearerabcdefgh") {
		t.Fatalf("zero width space canonical = %s", seen["zero width is not space"].canonical)
	}
	if !contains(seen["mixed case"].canonical, "BeArEr [redacted]") {
		t.Fatalf("mixed case canonical = %s", seen["mixed case"].canonical)
	}
	if !contains(seen["secret at end"].canonical, "note token=[redacted]") || contains(seen["secret at end"].canonical, "s3cr3tvalue") {
		t.Fatalf("secret at end canonical = %s", seen["secret at end"].canonical)
	}
	if !contains(seen["nbsp inside value"].canonical, "token=[redacted]") || contains(seen["nbsp inside value"].canonical, "s3cr3t") {
		t.Fatalf("nbsp inside value canonical = %s", seen["nbsp inside value"].canonical)
	}
	if !contains(seen["word edge"].canonical, "mybearer abcdefgh") {
		t.Fatalf("word edge canonical = %s", seen["word edge"].canonical)
	}
}

func TestTitleByteLimitIsJSONEncoded(t *testing.T) {
	over := repeat('a', 299)
	if encodedBytes(over) != 301 {
		t.Fatalf("encoded = %d, want 301", encodedBytes(over))
	}
	ok := repeat('a', 298)
	if encodedBytes(ok) != 300 {
		t.Fatalf("encoded = %d, want 300", encodedBytes(ok))
	}
	_, err := CanonicalizeObjectJSON(mustJSON(t, map[string]any{
		"title": over, "urgency": "u", "meanwhile": "m",
	}))
	fe, okCast := err.(*fieldError)
	if !okCast || fe.Field != "title" {
		t.Fatalf("error = %v, want title", err)
	}
	if _, err := CanonicalizeObjectJSON(mustJSON(t, map[string]any{
		"title": ok, "urgency": "u", "meanwhile": "m",
	})); err != nil {
		t.Fatal(err)
	}
}

func TestScriptIsNotHTMLEscaped(t *testing.T) {
	got, err := CanonicalizeObjectJSON(mustJSON(t, map[string]any{
		"title": "<script>", "urgency": "u", "meanwhile": "m",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "<script>" || contains(got.JSON, `\u003c`) {
		t.Fatalf("canonical = %s title = %q", got.JSON, got.Title)
	}
}

func repeat(c byte, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := marshalJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})())
}
