package approvals

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

func TestCreateTiersAndStoredExecution(t *testing.T) {
	dir := t.TempDir()
	audit := actuation.NewAuditLog(filepath.Join(dir, "audit.json"))
	svc := New(filepath.Join(dir, "approvals"), audit)

	var calls int
	var got map[string]any
	actions.RegisterExecutor("cordon_node", func(_ context.Context, params map[string]any) (any, error) {
		calls++
		got = params
		return map[string]any{"ok": true, "name": params["name"]}, nil
	})
	actions.RegisterExecutor("wake_compute_node", func(context.Context, map[string]any) (any, error) {
		t.Fatal("B-tier must not execute through approvals")
		return nil, nil
	})

	b := svc.create(context.Background(), "sess-1", "wake_compute_node", "because", "", map[string]any{"name": "n1"})
	if b.Status != http.StatusBadRequest || b.Body["error"] != "call directly" {
		t.Fatalf("B-tier create = %d %#v", b.Status, b.Body)
	}
	x := svc.create(context.Background(), "sess-1", "scale_deployment", "because", "", map[string]any{
		"namespace": "bifrost-dev", "kind": "Deployment", "name": "daemon", "replicas": 2,
	})
	if x.Status != http.StatusForbidden || x.Body["tier"] != "X" {
		t.Fatalf("X-tier create = %d %#v", x.Status, x.Body)
	}

	c := svc.create(context.Background(), "sess-b1", "cordon_node", "patch node", "uncordon", map[string]any{"name": "node-a"})
	if c.Status != http.StatusCreated {
		t.Fatalf("C create = %d %#v", c.Status, c.Body)
	}
	if c.Approval.Status != StatusPending || c.Approval.Requester != "sess-b1" || c.Approval.Tier != "C" || c.Approval.ParamsHash == "" {
		t.Fatalf("approval = %#v", c.Approval)
	}
	if !c.Approval.ExpiresAt.After(c.Approval.CreatedAt.Add(23 * time.Hour)) {
		t.Fatalf("expires_at = %s", c.Approval.ExpiresAt)
	}

	// Extra fields on the approve call are not params. The executor must see node-a.
	out := svc.approve(context.Background(), c.Approval.ID, "console")
	if out.Status != http.StatusOK || out.Body["status"] != StatusExecuted {
		t.Fatalf("approve = %d %#v", out.Status, out.Body)
	}
	if calls != 1 || got["name"] != "node-a" {
		t.Fatalf("executor calls=%d params=%#v", calls, got)
	}
	again := svc.approve(context.Background(), c.Approval.ID, "phone")
	if again.Status != http.StatusConflict || calls != 1 {
		t.Fatalf("second approve = %d calls=%d body=%#v", again.Status, calls, again.Body)
	}
}

func TestTamperedParamsDoNotRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "approvals")
	svc := New(path, nil)
	actions.RegisterExecutor("cordon_node", func(context.Context, map[string]any) (any, error) {
		t.Fatal("tampered approval must not execute")
		return nil, nil
	})
	c := svc.create(context.Background(), "s", "cordon_node", "because", "", map[string]any{"name": "node-a"})
	if c.Status != http.StatusCreated {
		t.Fatal(c.Body)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	swapped := bytes.ReplaceAll(raw, []byte("node-a"), []byte("node-b"))
	if bytes.Equal(raw, swapped) {
		t.Fatal("fixture did not contain node-a")
	}
	if err := os.WriteFile(path, swapped, 0o644); err != nil {
		t.Fatal(err)
	}
	out := svc.approve(context.Background(), c.Approval.ID, "chat")
	if out.Status != http.StatusConflict {
		t.Fatalf("tampered approve = %d %#v", out.Status, out.Body)
	}
}

func TestExpiredCannotBeApproved(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), actuation.NewAuditLog(filepath.Join(t.TempDir(), "audit.json")))
	var calls int
	actions.RegisterExecutor("drain_node", func(context.Context, map[string]any) (any, error) {
		calls++
		return map[string]any{"ok": true}, nil
	})
	c := svc.create(context.Background(), "s", "drain_node", "because", "uncordon", map[string]any{"name": "n1"})
	if c.Status != http.StatusCreated {
		t.Fatal(c.Body)
	}
	svc.SetClock(func() time.Time { return c.Approval.CreatedAt.Add(25 * time.Hour) })
	out := svc.approve(context.Background(), c.Approval.ID, "chat")
	if out.Status != http.StatusConflict || out.Body["error"] != "expired" || calls != 0 {
		t.Fatalf("expired approve = %d %#v calls=%d", out.Status, out.Body, calls)
	}
	rec, ok := svc.get(c.Approval.ID)
	if !ok || rec.Status != StatusExpired {
		t.Fatalf("stored status = %#v ok=%v", rec, ok)
	}
}

func TestApproveRejectAuthAndAudit(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.yaml")
	if err := os.WriteFile(authPath, []byte(`
tokens:
  - name: viewer
    role: viewer
    token: viewer-test-token
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
	audit := actuation.NewAuditLog(filepath.Join(dir, "audit.json"))
	svc := New(filepath.Join(dir, "approvals"), audit)
	actions.RegisterExecutor("uncordon_node", func(context.Context, map[string]any) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	r := chi.NewRouter()
	Mount(r, auth, svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/approvals", bytes.NewReader([]byte(`{"action":"uncordon_node","params":{"name":"n1"},"reason":"restore","rollback":"cordon"}`)))
	req.Header.Set("Authorization", "Bearer operator-test-token")
	req.Header.Set("X-Bifrost-Session", "session-42")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID        string `json:"id"`
		Requester string `json:"requester"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Requester != "session-42" || created.Status != StatusPending {
		t.Fatalf("created = %#v", created)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/approvals/"+created.ID+"/approve", bytes.NewReader([]byte(`{"channel":"phone","params":{"name":"other"}}`)))
	req.Header.Set("Authorization", "Bearer operator-test-token")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("operator approve = %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/approvals/"+created.ID+"/approve", bytes.NewReader([]byte(`{"channel":"phone","params":{"name":"other"}}`)))
	req.Header.Set("Authorization", "Bearer admin-test-token")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"status":"executed"`)) {
		t.Fatalf("admin approve = %d %s", rec.Code, rec.Body.String())
	}
	stored, _ := svc.get(created.ID)
	if stored.Params["name"] != "n1" || stored.Channel != "phone" {
		t.Fatalf("stored = %#v", stored)
	}

	body, _ := os.ReadFile(filepath.Join(dir, "audit.json"))
	for _, want := range []string{"approval.create", "approval.approve", "approval.execute", "channel=phone", "session-42"} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("audit missing %s\n%s", want, body)
		}
	}
}

func TestPruneKeepsOpenAndRecentClosed(t *testing.T) {
	s := &file{}
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		s.Approvals = append(s.Approvals, Approval{ID: "open", Status: StatusPending, CreatedAt: now})
	}
	s.Approvals[1].ID = "open-2"
	for i := 0; i < keepClosed+10; i++ {
		s.Approvals = append(s.Approvals, Approval{
			ID: "c", Status: StatusExecuted, DecidedAt: now.Add(time.Duration(i) * time.Second),
		})
	}
	s.prune()
	open, closed := 0, 0
	var oldest, newest time.Time
	for _, a := range s.Approvals {
		if a.open() {
			open++
			continue
		}
		closed++
		if oldest.IsZero() || a.DecidedAt.Before(oldest) {
			oldest = a.DecidedAt
		}
		if a.DecidedAt.After(newest) {
			newest = a.DecidedAt
		}
	}
	if open != 2 || closed != keepClosed {
		t.Fatalf("open=%d closed=%d", open, closed)
	}
	if !oldest.Equal(now.Add(10*time.Second)) || !newest.Equal(now.Add(time.Duration(keepClosed+9)*time.Second)) {
		t.Fatalf("kept window oldest=%s newest=%s", oldest, newest)
	}
}
