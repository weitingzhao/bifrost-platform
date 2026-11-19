package actionrecord

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// The record is built from what the caller sent and nothing else: a call that
// gave a reason, a rollback and an unchecked list carries them; one that gave
// none says so rather than making them up. (Moved from the server's
// TestDirectRecordSaysWhatTheCallerLeftOut, decoupling item 5, A5.)
func TestFromDirectSaysWhatTheCallerLeftOut(t *testing.T) {
	d := Direct{SessionHeader: "local_s", Action: "rollout_restart_deployment", Law: "action catalog: tier B",
		Params: map[string]any{"namespace": "cicd", "kind": "Deployment", "name": "x"}, Status: 200}
	bare := FromDirect(d)
	if bare.Reason != NoReason || bare.RegretState != "undeclared" || bare.Outcome != "done" || bare.Seat != "local_s" {
		t.Fatalf("bare = %+v", bare)
	}
	d.Params = map[string]any{"reason": "reload cert", "rollback": "restart again", "unchecked": "none"}
	d.Status = 403
	full := FromDirect(d)
	if full.Reason != "reload cert" || full.RegretState != "declared" || full.Regret != "restart again" || full.NotVerified != "none" || full.Outcome != "refused" {
		t.Fatalf("full = %+v", full)
	}
	d.Params, d.Status = map[string]any{}, http.StatusInternalServerError
	if got := FromDirect(d); got.Outcome != "failed" {
		t.Fatalf("a 500 = %s", got.Outcome)
	}
	// Origin: the seat falls back to the authenticated principal, then to unknown.
	if got := FromDirect(Direct{Principal: "operator"}); got.Seat != "operator" {
		t.Fatalf("seat = %q", got.Seat)
	}
	if got := FromDirect(Direct{}); got.Seat != "unknown" {
		t.Fatalf("seat = %q", got.Seat)
	}
}

var bearer = regexp.MustCompile(`Bearer \S+`)

// A stand-in for the server's redactor: it hides bearer tokens and the value
// of a key named token.
func testRedact(s string) string {
	s = bearer.ReplaceAllString(s, "Bearer [redacted]")
	if strings.HasPrefix(s, "token=") {
		return "token=[redacted]"
	}
	return s
}

// Moved from the server's TestTraceRedactsWithoutDamagingJSON: redaction keeps
// the JSON valid and leaves the caller's input untouched where nothing is secret.
func TestTraceRedactsWithoutDamagingJSON(t *testing.T) {
	value := map[string]any{"reason": "line one\nBearer abcdefghijklmnop\nline three", "params": map[string]any{"token": "private-value"}, "rollback": "revert commit"}
	raw, err := json.Marshal(RedactTrace(value, testRedact))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "abcdefghijklmnop") || strings.Contains(string(raw), "private-value") || !json.Valid(raw) {
		t.Fatal(string(raw))
	}
	if value["rollback"] != "revert commit" {
		t.Fatal(value)
	}
}
