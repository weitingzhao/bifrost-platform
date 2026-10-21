package agentrtapi_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// A path the client escaped names the same row as the one it did not
// (2026-10-21, PROD: the Console's encodeURIComponent sent the plan's import
// request as sha256%3A…, the artifacts route looked for that text, answered
// 404, and Work showed no plan). Every route of Mount unescapes its
// parameters once, and only when the path was escaped: %253A stays %3A.
func TestEscapedPathsNameTheSameRows(t *testing.T) {
	pool := seeded(t)
	s := server(t, pool)
	if code, out := call(t, s, "reporter", "POST", "plan/imports", importBody("r2", ledger(t, fixtureLedger)), session, "s1"); code != 200 || out["question_id"] == nil {
		t.Fatalf("import = %d %v", code, out)
	}
	if n, err := store.DecidePlanChanges(ctx, pool, answer(store.CardApplyKey), nil); err != nil || n != 1 {
		t.Fatalf("decide the import: %d %v", n, err)
	}
	escape := func(id string) string { return strings.ReplaceAll(id, ":", "%3A") }
	raw := func(caller, method, path string, body []byte, header ...string) (int, string) {
		t.Helper()
		var r io.Reader
		if body != nil {
			r = strings.NewReader(string(body))
		}
		req, err := http.NewRequest(method, s.URL+"/api/v1/agentrt/"+path, r)
		must(t, err)
		req.Header.Set("Authorization", "Bearer "+tokens[caller])
		for i := 0; i+1 < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		resp, err := http.DefaultClient.Do(req)
		must(t, err)
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		must(t, err)
		return resp.StatusCode, string(b)
	}

	// An artifact read by its escaped id, as Work read the plan's import request (PROD's 404), then put by it.
	report := []byte("# report\nall tests pass\n")
	id := artifactID(report)
	if code, body := raw("reporter", "PUT", "artifacts/"+id, report, "X-Artifact-Kind", "report", session, "s1"); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("PUT artifacts/%s = %d %s", id, code, body)
	}
	if code, body := raw("viewer", "GET", "artifacts/"+escape(id), nil); code != http.StatusOK || body != string(report) {
		t.Fatalf("GET artifacts/%s = %d %q", escape(id), code, body)
	}
	if code, body := raw("reporter", "PUT", "artifacts/"+escape(id), report, "X-Artifact-Kind", "report", session, "s1"); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("PUT artifacts/%s = %d %s", escape(id), code, body)
	}
	if code, body := raw("viewer", "GET", "artifacts/"+id, nil); code != http.StatusOK || body != string(report) {
		t.Fatalf("GET artifacts/%s = %d %q", id, code, body)
	}

	// A plan node, its notes and its sign-off, by an escaped id: the same row as unescaped.
	for _, path := range []string{"plan/" + escape("R2:2.0-1"), "plan/R2:2.0-1"} {
		if code, body := raw("viewer", "GET", path, nil); code != http.StatusOK || !strings.Contains(body, `"plan_node_id": "R2:2.0-1"`) && !strings.Contains(body, `"plan_node_id":"R2:2.0-1"`) {
			t.Fatalf("GET %s = %d %.200s", path, code, body)
		}
	}
	// No owner_signoff in its rule: refused for that (422), not as no such step (404).
	if code, out := call(t, s, "admin", "POST", "plan/"+escape("R2:2.0-1")+"/signoff", map[string]any{}); code != http.StatusUnprocessableEntity ||
		!strings.Contains(out["message"].(string), "R2:2.0-1") {
		t.Fatalf("sign off by an escaped id = %d %v", code, out)
	}
	if code, out := call(t, s, "reporter", "POST", "plan/"+escape("R2:2.0-1")+"/notes", map[string]any{"idempotency_key": "n1", "text": "x"}, session, "s1"); code != http.StatusUnprocessableEntity ||
		!strings.Contains(out["message"].(string), "only an architecture row takes a note") {
		t.Fatalf("a note by an escaped id = %d %v", code, out)
	}

	// Unescaped once, not twice: %253A is the text %3A, which names nothing.
	if code, _ := raw("viewer", "GET", "artifacts/"+strings.ReplaceAll(id, ":", "%253A"), nil); code != http.StatusNotFound {
		t.Fatalf("a twice-escaped artifact id = %d; want 404", code)
	}
	if code, _ := raw("viewer", "GET", "plan/R2%253A2.0-1", nil); code != http.StatusNotFound {
		t.Fatalf("a twice-escaped plan node = %d; want 404", code)
	}
}
