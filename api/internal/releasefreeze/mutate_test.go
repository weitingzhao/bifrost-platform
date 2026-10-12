package releasefreeze

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"sort"
	"testing"
	"unicode"
	"unicode/utf8"
)

// Status codes used as the "200 → anything else" mutants. The set covers the
// table's non-200 cell (403, 404, 5xx) and other non-200 classes.
var mutantStatuses = []int{201, 204, 301, 302, 400, 401, 403, 404, 409, 429, 500, 502, 503}

func TestMutation(t *testing.T) {
	file, _ := loadVectors(t)
	var mutants, escaped, disagree int
	for _, c := range file.Cases {
		if c.Verdict != VerdictClear || c.Transport != "" {
			continue
		}
		body := []byte(c.Body)
		var tree any
		if err := json.Unmarshal(body, &tree); err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		for _, m := range mutateTree(tree) {
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			mutants++
			if !agreeClear(t, c.Status, raw, &escaped, &disagree) {
				t.Fatalf("%s body mutant escaped or disagreed", c.Name)
			}
		}
		for _, status := range mutantStatuses {
			mutants++
			if !agreeClear(t, status, body, &escaped, &disagree) {
				t.Fatalf("%s status %d escaped or disagreed", c.Name, status)
			}
		}
	}
	t.Logf("mutants %d escaped %d", mutants, escaped)
	if escaped != 0 {
		t.Fatalf("%d mutants escaped", escaped)
	}
	if disagree != 0 {
		t.Fatalf("%d mutants disagreed with the oracle", disagree)
	}
}

func agreeClear(t *testing.T, status int, body []byte, escaped, disagree *int) bool {
	t.Helper()
	got := DecideHTTP(status, body).Verdict == VerdictClear
	want := oracleClear(status, body)
	if got == want {
		return true
	}
	*disagree++
	if got && !want {
		*escaped++
		t.Errorf("escaped: status %d body %s", status, body)
	} else {
		t.Errorf("oracle clear but decision refused: status %d body %s", status, body)
	}
	return false
}

// oracleClear is a separate reading of the rule. It does not call DecideHTTP.
func oracleClear(status int, body []byte) bool {
	if status != 200 || !utf8.Valid(body) || bytes.HasPrefix(body, []byte{0xEF, 0xBB, 0xBF}) {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	var v any
	if err := dec.Decode(&v); err != nil {
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return false
	}
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	av, ok := m["apiVersion"].(string)
	if !ok || av != "release.bifrost.io/v1" {
		return false
	}
	kind, ok := m["kind"].(string)
	if !ok || kind != "FreezeList" {
		return false
	}
	meta, ok := m["metadata"].(map[string]any)
	if !ok {
		return false
	}
	if raw, exists := meta["continue"]; exists {
		s, isStr := raw.(string)
		if !isStr || s != "" {
			return false
		}
	}
	items, ok := m["items"].([]any)
	return ok && len(items) == 0
}

func mutateTree(v any) []any {
	var out []any
	out = append(out, changeType(v))
	switch n := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(n))
		for k := range n {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			deleted := copyMap(n)
			delete(deleted, k)
			out = append(out, deleted)
			renamed := copyMap(n)
			val := renamed[k]
			delete(renamed, k)
			renamed[flipKey(k)] = val
			out = append(out, renamed)
			for _, child := range mutateTree(n[k]) {
				cp := copyMap(n)
				cp[k] = child
				out = append(out, cp)
			}
		}
	case []any:
		appended := append(append([]any{}, n...), map[string]any{"added": true})
		out = append(out, appended)
		for i := range n {
			for _, child := range mutateTree(n[i]) {
				cp := append([]any{}, n...)
				cp[i] = child
				out = append(out, cp)
			}
		}
	}
	return out
}

func changeType(v any) any {
	switch v.(type) {
	case map[string]any:
		return []any{}
	case []any:
		return map[string]any{}
	case string:
		return 0.0
	case float64:
		return "x"
	case bool:
		return "x"
	default:
		return "x"
	}
}

func copyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func flipKey(k string) string {
	if k == "" {
		return "X"
	}
	r := []rune(k)
	if unicode.IsUpper(r[0]) {
		r[0] = unicode.ToLower(r[0])
	} else {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}

func TestMutationOracleDoesNotCallDecide(t *testing.T) {
	// The oracle is a different function. A direct empty list is clear; a 404 of
	// that same body is not, which is the status half of the mutation.
	body := []byte(`{"apiVersion":"release.bifrost.io/v1","kind":"FreezeList","metadata":{},"items":[]}`)
	if !oracleClear(200, body) || oracleClear(404, body) {
		t.Fatal("oracle")
	}
	if _, err := os.Stat("decision.go"); err != nil {
		t.Fatal(err)
	}
}
