package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// One open agentrt_rule_approve request per rule, whatever version it
// names; landing grants keep their own key.
func TestRuleApproveOpenKey(t *testing.T) {
	a := approvalOpenKey(RuleApproveAction, map[string]any{"rule_id": "concurrency", "rule_version": 2})
	b := approvalOpenKey(RuleApproveAction, map[string]any{"rule_id": "concurrency", "rule_version": 3})
	if a == "" || a != b {
		t.Fatalf("keys %q and %q", a, b)
	}
	if c := approvalOpenKey(RuleApproveAction, map[string]any{"rule_id": "budget:dev"}); c == a {
		t.Fatal("two rules share a key")
	}
	if k := approvalOpenKey("mark_missions_no_merge", map[string]any{"rule_id": "concurrency"}); k != "" {
		t.Fatalf("another action has key %q", k)
	}
}

// The request binds to this platform: PROD only, a PROD runtime, exactly
// its four parameters, a whole version.
func TestRuleApproveRequestRefuses(t *testing.T) {
	s := &Server{}
	good := map[string]any{"env": "prod", "rule_id": "concurrency", "rule_version": 2, "text_artifact_id": "sha256:" + strings.Repeat("a", 64)}
	with := func(k string, v any) map[string]any {
		p := map[string]any{}
		for key, val := range good {
			p[key] = val
		}
		if v == nil {
			delete(p, k)
		} else {
			p[k] = v
		}
		return p
	}
	for name, p := range map[string]map[string]any{
		"no runtime":     good,
		"stg":            with("env", "stg"),
		"another field":  with("reason", "x"),
		"a half version": with("rule_version", 2.5),
		"no version":     with("rule_version", nil),
	} {
		if err := s.ruleApproveCreateCheck(context.Background(), p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := s.ruleApprove(context.Background(), good); err == nil {
		t.Fatal("the executor ran without an approval")
	}
}

// Q1 A, Q2 A on the real path: a seat proposes the next tuning
// version; a draft that also moves its validity is refused when the card is
// requested; the capacity-only draft makes a tier D card whose summary shows
// the old and new value, a second request for the same rule is refused while
// it is open, and the Owner's Console approval writes the version with the
// approval's id and rule_approved.
func TestRuleApproveOnTheRealPath(t *testing.T) {
	ctx := context.Background()
	cfg := newTestConfig(t)
	authFile := filepath.Join(t.TempDir(), "platform-auth.yaml")
	if err := os.WriteFile(authFile, []byte("tokens:\n  - name: viewer\n    role: viewer\n    token: viewer-test-token\n"+
		"  - name: operator\n    role: operator\n    token: operator-test-token\n  - name: admin\n    role: admin\n    token: admin-test-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.PlatformAuthPath = authFile
	t.Setenv("AGENTRT_ENV", "prod")
	t.Setenv("AGENTRT_DSN", "")
	srv := newTestServer(t, cfg)
	db := agentrttest.Migrated(t)
	if _, err := actor.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	conn := db.Connect(t, db.Runtime)
	if _, err := conn.Exec(ctx, agentrttest.TuningRule); err != nil {
		t.Fatal(err)
	}
	srv.agentRuntime.api.DB = conn
	draft := func(key, expires string, perHost int) string {
		t.Helper()
		text := fmt.Sprintf("rule_id: tuning\nrule_version: 2\ndirection: loosen\neffective_at: 2026-01-01T00:00:00Z\nexpires_at: %s\nvalues: {failures_to_alert: %d}\n", expires, perHost)
		out, err := store.ProposeRule(ctx, conn, actor.Writer{Actor: "thread:s1", Token: "operator"}, key, "tuning", []byte(text), "alert later")
		if err != nil {
			t.Fatal(err)
		}
		return out["text_artifact_id"].(string)
	}
	h := srv.Router()
	do := func(method, path, body, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	request := func(art string) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"action": RuleApproveAction, "reason": "alert after more failures",
			"params": map[string]any{"env": "prod", "rule_id": "tuning", "rule_version": 2, "text_artifact_id": art}})
		return do(http.MethodPost, "/api/v1/approvals", string(body), "operator-test-token")
	}

	if rec := request(draft("later", "2100-02-01T00:00:00Z", 5)); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "expires_at") {
		t.Fatalf("a draft that moves its validity: %d %s", rec.Code, rec.Body.String())
	}
	art := draft("up", "2100-01-01T00:00:00Z", 5)
	rec := request(art)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"tier":"D"`) || !strings.Contains(rec.Body.String(), "failures_to_alert 2 → 5") {
		t.Fatalf("request = %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	// Origin: #562 (2026-11-14). A second request for the same rule from the
	// same requester replaces its unrun card: the old one ends rejected as
	// superseded, and the rule still has one open card.
	again := request(art)
	if again.Code != http.StatusCreated {
		t.Fatalf("a replacing request for the rule: %d %s", again.Code, again.Body.String())
	}
	old := do(http.MethodGet, "/api/v1/approvals/"+created.ID, "", "viewer-test-token")
	if old.Code != http.StatusOK || !strings.Contains(old.Body.String(), `"status":"rejected"`) {
		t.Fatalf("the replaced card: %d %s", old.Code, old.Body.String())
	}
	if err := json.Unmarshal(again.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if _, err := actions.Execute(ctx, RuleApproveAction, map[string]any{"env": "prod", "rule_id": "tuning", "rule_version": 2, "text_artifact_id": art}); err == nil {
		t.Fatal("the executor ran without an approval")
	}
	get := do(http.MethodGet, "/api/v1/approvals/"+created.ID, "", "viewer-test-token")
	var row struct {
		ApprovalLine string `json:"approval_line"`
		ParamsHash   string `json:"params_hash"`
		Number       int    `json:"number"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &row); err != nil || row.ApprovalLine == "" {
		t.Fatalf("get = %d %s", get.Code, get.Body.String())
	}
	body, _ := json.Marshal(map[string]any{"channel": "console", "approval_line": row.ApprovalLine, "params_hash": row.ParamsHash,
		"confirm_number": row.Number})
	out := do(http.MethodPost, "/api/v1/approvals/"+created.ID+"/approve", string(body), "admin-test-token").Body.String()
	if !strings.Contains(out, `"status":"executed"`) || !strings.Contains(out, `"approval_id":"`+created.ID+`"`) {
		t.Fatalf("approve = %s", out)
	}
	var perHost, events int
	if err := conn.QueryRow(ctx, `SELECT (body_json->'values'->>'failures_to_alert')::int, (SELECT count(*) FROM agentrt.event WHERE event_type = 'rule_approved'
	    AND body_json->>'approval_id' = $1) FROM agentrt.rule WHERE rule_id = 'tuning' AND rule_version = 2 AND approval_id = $1`,
		created.ID).Scan(&perHost, &events); err != nil || perHost != 5 || events != 1 {
		t.Fatalf("version 2: failures_to_alert %d, %d events (%v)", perHost, events, err)
	}
}
