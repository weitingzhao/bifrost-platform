package approvals

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
)

// A drill (W-31 2.5 gap 2, Owner 2026-10-21; v4 §20.3 rule 8, §20.5
// scenario 9): made through POST /approvals with drill, it waits as pending
// even for an action a release policy would approve; approve answers 409 on
// every channel and nothing runs; its notice says it is a drill and asks for
// no decision; it is sent no reminder; reject closes it.
func TestADrillIsNeverApproved(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.yaml")
	if err := os.WriteFile(authPath, []byte(`
tokens:
  - name: operator
    role: operator
    token: operator-test-token
  - name: admin
    role: admin
    token: admin-test-token
`), 0o600); err != nil {
		t.Fatal(err)
	}
	auth, err := actuation.LoadAuth(authPath)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(filepath.Join(dir, "approvals"), actuation.NewAuditLog(filepath.Join(dir, "audit.json")))
	calls := 0
	actions.RegisterExecutor("uncordon_node", func(context.Context, map[string]any) (any, error) {
		calls++
		return map[string]any{"ok": true}, nil
	})
	svc.SetAutoApprover(func(context.Context, string, actions.Tier, map[string]any) string { return "release-policy-test" })
	r := chi.NewRouter()
	Mount(r, auth, svc)
	send := func(token, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Bifrost-Session", "drill-session")
		r.ServeHTTP(rec, req)
		return rec
	}
	const create = `{"action":"uncordon_node","params":{"name":"n1"},"reason":"T24 scenario 9: what a drill looks like","rollback":"nothing runs"`

	// The positive control: the same request, not a drill, is one the policy approves at once.
	if rec := send("operator-test-token", "/approvals", create+`}`); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), `"auto_approved_by":"release-policy-test"`) {
		t.Fatalf("the same request, not a drill = %d %s", rec.Code, rec.Body.String())
	}
	rec := send("operator-test-token", "/approvals", create+`,"drill":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create a drill = %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID     string `json:"id"`
		Number int    `json:"number"`
		Status string `json:"status"`
		Drill  bool   `json:"drill"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Status != StatusPending || !created.Drill || created.Number == 0 {
		t.Fatalf("created = %+v", created)
	}
	stored, ok := svc.find(created.ID)
	if !ok || !stored.Drill {
		t.Fatalf("stored = %+v %v", stored, ok)
	}

	// No approval on any channel; nothing runs; it stays pending.
	for _, channel := range []string{"console", "phone", "chat"} {
		rec := send("admin-test-token", "/approvals/"+created.ID+"/approve", string(echoApproveBody(stored, channel, nil)))
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"error":"a drill cannot be approved"`) {
			t.Fatalf("approve a drill on %s = %d %s", channel, rec.Code, rec.Body.String())
		}
	}
	if got, _ := svc.get(created.ID); got.Status != StatusPending || calls != 0 {
		t.Fatalf("after the approvals: status %s, %d runs", got.Status, calls)
	}
	// Listed as it is: readers see drill.
	list := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/approvals?status=pending", nil)
	req.Header.Set("Authorization", "Bearer admin-test-token")
	r.ServeHTTP(list, req)
	if !strings.Contains(list.Body.String(), `"drill":true`) {
		t.Fatalf("the list does not say drill: %s", list.Body.String())
	}

	// Its notice asks for nothing; no reminder, where a request that is not a drill gets one.
	now := stored.CreatedAt.Add(5 * time.Hour)
	m := approvalnotify.Compose(approvalnotify.KindCreated, noticeItem(stored), now)
	if !strings.HasPrefix(m.Title, "Drill · #") || !strings.Contains(m.Message, approvalnotify.DrillAsk) || m.Priority != 3 ||
		strings.Contains(m.Message, "as soon as you approve") {
		t.Fatalf("the drill's notice = %+v", m)
	}
	if kind := reminderDue(stored, now); kind != "" {
		t.Fatalf("a drill is reminded: %s", kind)
	}
	plain := stored
	plain.Drill = false
	if kind := reminderDue(plain, now); kind != approvalnotify.KindWaiting {
		t.Fatalf("a request that is not a drill, 5 h old: reminder %q", kind)
	}

	// Reject closes it.
	if rec := send("admin-test-token", "/approvals/"+created.ID+"/reject", `{"reason":"drill seen"}`); rec.Code != http.StatusOK {
		t.Fatalf("reject a drill = %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := svc.get(created.ID); got.Status != StatusRejected || calls != 0 {
		t.Fatalf("after reject: %s, %d runs", got.Status, calls)
	}
}
