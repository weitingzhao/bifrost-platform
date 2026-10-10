package approvals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

type push struct{ Title, Message string }

type fakeRelay struct {
	mu     sync.Mutex
	pushes []push
	fail   bool
}

func (f *fakeRelay) got() []push {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]push(nil), f.pushes...)
}

// relay answers like the operator-plane relay and points the notifier at it.
func relay(t *testing.T) *fakeRelay {
	t.Helper()
	f := &fakeRelay{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p push
		_ = json.NewDecoder(r.Body).Decode(&p)
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.fail {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"ntfy: status 500","deliveries":[{"channel":"ntfy","target":"ntfy:0a1b2c3d4e5f","result":"failed","error":"ntfy: status 500"}]}`))
			return
		}
		f.pushes = append(f.pushes, p)
		_, _ = w.Write([]byte(`{"status":"sent","deliveries":[{"channel":"ntfy","target":"ntfy:0a1b2c3d4e5f","result":"accepted"}]}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("APPROVAL_NOTIFY_URL", srv.URL)
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "relay-token")
	return f
}

func deliveriesOf(t *testing.T, svc *Service, id string) []Delivery {
	t.Helper()
	svc.notices.Wait()
	rec, ok := svc.find(id)
	if !ok {
		t.Fatalf("%s not found", id)
	}
	return rec.Deliveries
}

func TestFailedRunPushesAndRecordsTheDelivery(t *testing.T) {
	f := relay(t)
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	actions.RegisterExecutor("sweep_failed_backups", func(context.Context, map[string]any) (any, error) {
		return nil, errors.New("invalid params")
	})
	c := svc.create(context.Background(), "cursor-w49", "sweep_failed_backups", "sweep", "", nil)
	r := chi.NewRouter()
	r.Post("/approvals/{id}/approve", svc.HandleApprove)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/approvals/"+c.Approval.ID+"/approve", bytes.NewReader(echoApproveBody(c.Approval, "chat", nil))))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"failed"`) {
		t.Fatalf("approve = %d %s", rec.Code, rec.Body.String())
	}
	ds := deliveriesOf(t, svc, c.Approval.ID)
	got := f.got()
	if len(got) != 1 || got[0].Title != "#1 failed · sweep_failed_backups · data" || !strings.Contains(got[0].Message, "error: invalid params") {
		t.Fatalf("pushes = %+v", got)
	}
	if len(ds) != 1 || ds[0].Kind != "failed" || ds[0].Result != "accepted" || ds[0].Target != "ntfy:0a1b2c3d4e5f" || ds[0].At.IsZero() {
		t.Fatalf("deliveries = %+v", ds)
	}
}

// TD-286 ratchet: a push the relay could not deliver is on the record with
// its reason, not only in the log.
func TestUnknownPushFailureIsRecorded(t *testing.T) {
	f := relay(t)
	f.fail = true
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	rec, _ := claimed(t, svc)
	base := time.Now().UTC()
	svc.SetClock(func() time.Time { return base.Add(leaseFor + unknownGrace + time.Minute) })
	if got, _ := svc.get(rec.ID); got.Status != StatusUnknown {
		t.Fatalf("status = %s", got.Status)
	}
	ds := deliveriesOf(t, svc, rec.ID)
	if len(ds) != 1 || ds[0].Kind != "unknown" || ds[0].Result != "failed" || ds[0].Error != "ntfy: status 500" {
		t.Fatalf("deliveries = %+v", ds)
	}

	f.fail = false
	exit := 1
	lease := ""
	if r, ok := svc.find(rec.ID); ok {
		lease = r.Execution.LeaseID
	}
	out := svc.result(rec.ID, resultInput{LeaseID: lease, ExitCode: &exit})
	if out.Body["status"] != StatusFailed {
		t.Fatalf("late result = %v", out.Body)
	}
	svc.record(nil, out.events)
	ds = deliveriesOf(t, svc, rec.ID)
	got := f.got()
	if len(ds) != 2 || ds[1].Kind != "failed" || ds[1].Result != "accepted" || len(got) != 1 {
		t.Fatalf("after the late failure: deliveries %+v pushes %+v", ds, got)
	}
	if strings.Contains(got[0].Title+got[0].Message, "kubectl") {
		t.Fatalf("the command text reached the push: %+v", got[0])
	}
}

func TestNotExecutedBeforeTheDeadlinePushes(t *testing.T) {
	f := relay(t)
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	actions.RegisterExecutor("trigger_cnpg_backup", func(context.Context, map[string]any) (any, error) {
		return nil, actions.Transient("REFUSED: no release window for x")
	})
	c := svc.create(context.Background(), "s", "trigger_cnpg_backup", "backup", "", nil)
	svc.approve(context.Background(), c.Approval.ID, "chat")
	got, _ := svc.get(c.Approval.ID)
	after := got.Execution.Deadline.Add(time.Second)
	svc.SetClock(func() time.Time { return after })
	if got, _ = svc.get(c.Approval.ID); got.Status != StatusExpired {
		t.Fatalf("status = %s", got.Status)
	}
	ds := deliveriesOf(t, svc, c.Approval.ID)
	pushes := f.got()
	if len(ds) != 1 || ds[0].Kind != "not_executed" || len(pushes) != 1 ||
		!strings.HasPrefix(pushes[0].Title, "#1 not executed · trigger_cnpg_backup") || !strings.Contains(pushes[0].Message, "no release window") {
		t.Fatalf("deliveries %+v pushes %+v", ds, pushes)
	}
}

func TestSuccessIsNotPushed(t *testing.T) {
	f := relay(t)
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	actions.RegisterExecutor("cordon_node", func(context.Context, map[string]any) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	c := svc.create(context.Background(), "s", "cordon_node", "patch", "", map[string]any{"name": "node-a"})
	if out := svc.approve(context.Background(), c.Approval.ID, "chat"); out.Body["status"] != StatusExecuted {
		t.Fatalf("approve = %v", out.Body)
	}
	if ds := deliveriesOf(t, svc, c.Approval.ID); len(ds) != 0 || len(f.got()) != 0 {
		t.Fatalf("success was pushed: %+v", ds)
	}
}

func TestRemindersOnceEach(t *testing.T) {
	f := relay(t)
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	base := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	at := base
	svc.SetClock(func() time.Time { return at })
	c := svc.create(context.Background(), "cursor-w49", "cordon_node", "patch", "", map[string]any{"name": "node-a"})
	if c.Status != http.StatusCreated {
		t.Fatalf("create = %d %v", c.Status, c.Body)
	}
	for _, step := range []struct {
		after  time.Duration
		pushes int
	}{
		{3 * time.Hour, 0},
		{4 * time.Hour, 1},
		{5 * time.Hour, 1},
		{21*time.Hour + 59*time.Minute, 1},
		{22 * time.Hour, 2},
		{23 * time.Hour, 2},
		{25 * time.Hour, 2},
	} {
		at = base.Add(step.after)
		svc.remindDue(context.Background())
		if n := len(f.got()); n != step.pushes {
			t.Fatalf("after %s: %d pushes, want %d: %+v", step.after, n, step.pushes, f.got())
		}
	}
	got := f.got()
	if got[0].Title != "Waiting 4h · #1 · C · cordon_node · node" || !strings.Contains(got[0].Message, "expires in 20h") ||
		got[1].Title != "Expires in 2h · #1 · C · cordon_node · node" {
		t.Fatalf("reminders = %+v", got)
	}
	ds := deliveriesOf(t, svc, c.Approval.ID)
	if len(ds) != 2 || ds[0].Kind != "reminder_waiting" || ds[1].Kind != "reminder_expiring" {
		t.Fatalf("deliveries = %+v", ds)
	}
}

func TestNoticeAuditLines(t *testing.T) {
	relay(t)
	dir := t.TempDir()
	audit := actuation.NewAuditLog(filepath.Join(dir, "audit.json"))
	svc := New(filepath.Join(dir, "approvals"), audit)
	c := svc.create(context.Background(), "s", "cordon_node", "patch", "", map[string]any{"name": "node-a"})
	svc.deliver(context.Background(), "created", c.Approval)
	rec := httptest.NewRecorder()
	audit.HandleList(rec, httptest.NewRequest(http.MethodGet, "/audit", nil))
	var out struct {
		Records []actuation.AuditRecord `json:"records"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range out.Records {
		if e.Action == "approval.notify" && e.Target == c.Approval.ID && e.Status == "accepted" && strings.Contains(e.Detail, "kind=created channel=ntfy") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no approval.notify audit line: %s", rec.Body.String())
	}
}

func TestPassthroughSecretsStayOutOfThePush(t *testing.T) {
	f := relay(t)
	dir := t.TempDir()
	audit := actuation.NewAuditLog(filepath.Join(dir, "audit.json"))
	svc := New(filepath.Join(dir, "approvals"), audit)
	const leaked = "sk-LEAKEDAPIKEY999"
	const tokenValue = "abc123tokenvalue"
	c := svc.create(context.Background(), "s", "ib_maintenance", "set maintenance", "", map[string]any{
		"account_id": "token=" + tokenValue,
		"enabled":    true,
		"api_key":    leaked,
	})
	if c.Status != http.StatusCreated {
		t.Fatalf("create = %d %v", c.Status, c.Body)
	}
	if c.Approval.Params["api_key"] != leaked {
		t.Fatal("passthrough dropped api_key from the stored params")
	}
	if _, shown := c.Approval.KeyParams["api_key"]; shown || c.Approval.KeyParams["account_id"] != "token="+tokenValue {
		t.Fatalf("key params = %#v", c.Approval.KeyParams)
	}
	svc.deliver(context.Background(), "created", c.Approval)
	pushes := f.got()
	if len(pushes) != 1 {
		t.Fatalf("pushes = %+v", pushes)
	}
	text := pushes[0].Title + "\n" + pushes[0].Message
	if strings.Contains(text, leaked) || strings.Contains(text, tokenValue) || !strings.Contains(text, "account_id=token=[redacted]") {
		t.Fatalf("push = %q", text)
	}
	raw, err := json.Marshal(deliveriesOf(t, svc, c.Approval.ID))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	audit.HandleList(rec, httptest.NewRequest(http.MethodGet, "/audit", nil))
	blob := string(raw) + rec.Body.String()
	if strings.Contains(blob, leaked) || strings.Contains(blob, tokenValue) {
		t.Fatalf("delivery or audit leaked the secret: %s", blob)
	}
}
