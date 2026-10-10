package approvals

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
)

func withLine(rec Approval, in approveInput) approveInput {
	in.ApprovalLine = CanonicalApprovalLine(rec)
	in.ParamsHash = rec.ParamsHash
	return in
}

func echoApproveBody(rec Approval, channel string, extra map[string]any) []byte {
	m := map[string]any{
		"channel":       channel,
		"approval_line": CanonicalApprovalLine(rec),
		"params_hash":   rec.ParamsHash,
	}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return b
}

func TestCanonicalLineEscapesControlsAndBindsTheHash(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	base := Approval{
		Number: 3, Tier: "C", Action: "start_pipeline_run", Env: "cicd",
		Summary: "pipe", ParamsHash: hash,
		KeyParams: map[string]string{"tag": "release candidate"},
	}
	space := CanonicalApprovalLine(base)
	newline := base
	newline.KeyParams = map[string]string{"tag": "release\ncandidate"}
	withNL := CanonicalApprovalLine(newline)
	if space == withNL {
		t.Fatalf("newline collapsed into the same line:\n%s", space)
	}
	if strings.Contains(withNL, "\n") || !strings.Contains(withNL, `\n`) {
		t.Fatalf("newline line = %q", withNL)
	}
	if !strings.HasSuffix(space, hash[:12]) || !strings.HasSuffix(withNL, hash[:12]) {
		t.Fatalf("hash suffix missing: %q %q", space, withNL)
	}
	rtl := base
	rtl.KeyParams = map[string]string{"tag": "release\u202ecandidate"}
	shown := CanonicalApprovalLine(rtl)
	if strings.ContainsRune(shown, '\u202e') || !strings.Contains(shown, `\u202e`) {
		t.Fatalf("U+202E line = %q", shown)
	}
}

func TestMismatchedApprovalLineOrHashIsRefused(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	actions.RegisterExecutor("cordon_node", func(context.Context, map[string]any) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	c := svc.create(context.Background(), "s", "cordon_node", "patch", "", map[string]any{"name": "n1"})
	line := CanonicalApprovalLine(c.Approval)
	r := chi.NewRouter()
	r.Post("/approvals/{id}/approve", svc.HandleApprove)
	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/approvals/"+c.Approval.ID+"/approve", strings.NewReader(body))
		r.ServeHTTP(rec, req)
		return rec
	}
	badLine, _ := json.Marshal(map[string]any{"channel": "chat", "approval_line": line + " ", "params_hash": c.Approval.ParamsHash})
	if rec := post(string(badLine)); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "approval_line") {
		t.Fatalf("bad line = %d %s", rec.Code, rec.Body.String())
	}
	badHash, _ := json.Marshal(map[string]any{"channel": "chat", "approval_line": line, "params_hash": strings.Repeat("cd", 32)})
	if rec := post(string(badHash)); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "params_hash") {
		t.Fatalf("bad hash = %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := svc.find(c.Approval.ID); got.Status != StatusPending || got.ApprovedLine != "" {
		t.Fatalf("mismatch stored = %s approved_line=%q", got.Status, got.ApprovedLine)
	}
	ok := post(string(echoApproveBody(c.Approval, "chat", nil)))
	if ok.Code != http.StatusOK || !strings.Contains(ok.Body.String(), `"status":"executed"`) {
		t.Fatalf("matching line = %d %s", ok.Code, ok.Body.String())
	}
	got, _ := svc.find(c.Approval.ID)
	if got.ApprovedLine != line {
		t.Fatalf("approved line = %q, want %q", got.ApprovedLine, line)
	}
}
