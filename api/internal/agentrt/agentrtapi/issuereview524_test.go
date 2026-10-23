package agentrtapi_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
)

// Review 524 B2 over HTTP: get_updates refuses a cursor past how far the
// client was served (400) and changes nothing; later replies are still
// delivered from the cursor it was handed.
func TestIntakeUpdatesRefuseAFutureCursor(t *testing.T) {
	pool, _ := world(t)
	issueFixture(t, pool)
	s := server(t, pool)
	do := func(caller, method, path string, body any, want int) map[string]any {
		t.Helper()
		code, out := call(t, s, caller, method, path, body, agentrtapi.SessionHeader, "s-"+strings.ReplaceAll(caller, ":", "-"))
		if code != want {
			t.Fatalf("%s %s as %q: %d %v, want %d", method, path, caller, code, out, want)
		}
		return out
	}
	out := do("intake:dots-p2", "POST", "intake/issues", map[string]any{"idempotency_key": "k-1", "product": "screening", "kind": "complaint",
		"on_behalf_of": "person:p2", "title": "cursors", "person_words": []any{map[string]any{"text": "words"}}}, http.StatusOK)
	id := out["issue_id"].(string)
	served := do("intake:dots-p2", "POST", "intake/updates", map[string]any{"limit": 50}, http.StatusOK)["next_cursor"].(string)
	state := func() string {
		var s string
		must(t, pool.QueryRow(ctx, `SELECT COALESCE(served_txid || '.' || served_event, '') || '|' || COALESCE(acked_txid || '.' || acked_event, '')
		  FROM agentrt.intake_client WHERE intake_client_id = 'agent:dots-p2'`).Scan(&s))
		return s
	}
	before := state()
	far := "9223372036854775807.0"
	for _, body := range []map[string]any{{"cursor": far, "limit": 5}, {"cursor": far, "ack_cursor": far, "limit": 0}} {
		if out := do("intake:dots-p2", "POST", "intake/updates", body, http.StatusBadRequest); out["error"] != "bad_cursor" {
			t.Fatalf("a future cursor: %v", out)
		}
	}
	do("intake:dots-p2", "POST", "intake/updates", map[string]any{"ack_cursor": far, "limit": 0}, http.StatusUnprocessableEntity)
	if after := state(); after != before {
		t.Fatalf("served|acked moved: %s to %s", before, after)
	}
	do("operator", "POST", "issues/"+id+"/replies", map[string]any{"idempotency_key": "c-1", "expected_version": 0, "purpose": "reply",
		"text": "REPLY-524"}, http.StatusOK)
	cursor, found := served, false
	for deadline := time.Now().Add(60 * time.Second); !found && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		out := do("intake:dots-p2", "POST", "intake/updates", map[string]any{"cursor": cursor, "limit": 50}, http.StatusOK)
		for _, u := range out["updates"].([]any) {
			found = found || u.(map[string]any)["text"] == "REPLY-524"
		}
		cursor = out["next_cursor"].(string)
	}
	if !found {
		t.Fatal("the reply after the refused cursor was never delivered")
	}
}
