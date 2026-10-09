package workactions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
)

// TD-279: a direct B-tier call has no approval row, so the handler's audit
// record is its only trace. Every call that reaches the service records once,
// on success and on refusal.

func auditedHandler(t *testing.T) (*Handler, *actuation.AuditLog) {
	t.Helper()
	cs := fake.NewSimpleClientset(&batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "reconcile", Namespace: "monitoring"},
	})
	log := actuation.NewAuditLog(filepath.Join(t.TempDir(), "audit.json"))
	svc := &Service{
		Policy: &actuationpolicy.Policy{
			Jobs:    actuationpolicy.Jobs{Namespaces: map[string]string{"monitoring": "B"}},
			Probe:   actuationpolicy.Probe{Namespaces: map[string]string{"monitoring": "B"}, Images: []string{"example/curl:1"}},
			Cleanup: actuationpolicy.Cleanup{Namespaces: []string{"monitoring"}},
		},
		Clients: Clients{
			Kube:    func() (kubernetes.Interface, error) { return cs, nil },
			Dynamic: func() (dynamic.Interface, error) { return dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()), nil },
		},
		Now: func() time.Time { return time.Date(2026, 10, 9, 21, 5, 31, 0, time.UTC) },
	}
	return &Handler{Svc: svc, Audit: log}, log
}

func callAs(h http.HandlerFunc, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/actuation", strings.NewReader(body))
	req = req.WithContext(actuation.WithPrincipal(req.Context(), actuation.Principal{Name: "operator", Role: actuation.RoleOperator}))
	req.Header.Set("X-Bifrost-Session", "local_test")
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func auditRecords(t *testing.T, log *actuation.AuditLog) []actuation.AuditRecord {
	t.Helper()
	rec := httptest.NewRecorder()
	log.HandleList(rec, httptest.NewRequest(http.MethodGet, "/audit", nil))
	var out struct {
		Records []actuation.AuditRecord `json:"records"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("audit list: %v (%s)", err, rec.Body.String())
	}
	return out.Records
}

func requireRecord(t *testing.T, got actuation.AuditRecord, action, target, status string, detail ...string) {
	t.Helper()
	if got.Action != action || got.Target != target || got.Status != status {
		t.Fatalf("record = %s %s %s, want %s %s %s", got.Action, got.Target, got.Status, action, target, status)
	}
	if got.Actor != "operator" || got.Role != actuation.RoleOperator {
		t.Fatalf("record actor = %s/%s, want the caller's principal", got.Actor, got.Role)
	}
	for _, d := range append(detail, "requester=local_test") {
		if !strings.Contains(got.Detail, d) {
			t.Fatalf("record detail %q lacks %q", got.Detail, d)
		}
	}
}

func TestAuditCreateJobThenDeleteFinished(t *testing.T) {
	h, log := auditedHandler(t)
	if rec := callAs(h.HandleCreateJob, `{"namespace":"monitoring","cronjob":"reconcile"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("create job = %d %s", rec.Code, rec.Body.String())
	}
	if rec := callAs(h.HandleDeleteFinished, `{"namespace":"monitoring","names":["reconcile-manual-20261009210531"]}`); rec.Code != http.StatusOK {
		t.Fatalf("delete finished = %d %s", rec.Code, rec.Body.String())
	}
	got := auditRecords(t, log)
	if len(got) != 2 {
		t.Fatalf("want 2 audit records, got %d: %+v", len(got), got)
	}
	// Newest first.
	requireRecord(t, got[1], "create_job_from_cronjob", "monitoring/reconcile", "ok", "job=reconcile-manual-20261009210531")
	requireRecord(t, got[0], "delete_finished_jobs", "monitoring/reconcile-manual-20261009210531", "ok", "jobs=")
}

func TestAuditRefusals(t *testing.T) {
	h, log := auditedHandler(t)
	if rec := callAs(h.HandleCreateJob, `{"namespace":"kube-system","cronjob":"x"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("create job in kube-system = %d", rec.Code)
	}
	if rec := callAs(h.HandleProbe, `{"namespace":"monitoring","image":"evil:1","command":["sh","-c"],"args":["id"]}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("probe with an unlisted image = %d", rec.Code)
	}
	if rec := callAs(h.HandleDeleteFinished, `{"namespace":"data","label_selector":"app=x"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("cleanup in data = %d", rec.Code)
	}
	// No Sync wired: Plan refuses.
	if rec := callAs(h.HandlePlan, `{"repo":"example-repo","path":"k8s/app.yaml","commit":"abc"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("plan without sync = %d", rec.Code)
	}
	if rec := callAs(h.HandleApply, `{"plan_id":"plan-missing"}`); rec.Code != http.StatusConflict {
		t.Fatalf("apply of an unknown plan = %d", rec.Code)
	}
	got := auditRecords(t, log)
	if len(got) != 5 {
		t.Fatalf("want 5 audit records, got %d: %+v", len(got), got)
	}
	requireRecord(t, got[4], "create_job_from_cronjob", "kube-system/x", "failed", "error=")
	requireRecord(t, got[3], "run_probe_pod", "monitoring/evil:1", "failed", "error=", "command=sh -c id")
	requireRecord(t, got[2], "delete_finished_jobs", "data/app=x", "failed", "error=")
	requireRecord(t, got[1], "plan_manifest", "example-repo:k8s/app.yaml@abc", "failed", "error=")
	requireRecord(t, got[0], "apply_manifest", "plan-missing", "failed", "error=")
}

func TestAuditSkipsUnreadableBodies(t *testing.T) {
	h, log := auditedHandler(t)
	if rec := callAs(h.HandleCreateJob, `{`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body = %d", rec.Code)
	}
	if got := auditRecords(t, log); len(got) != 0 {
		t.Fatalf("a body that never reached the service was audited: %+v", got)
	}
}

func TestAuditDetailIsBounded(t *testing.T) {
	h, log := auditedHandler(t)
	long := strings.Repeat("x", 2000)
	callAs(h.HandleProbe, `{"namespace":"monitoring","image":"evil:1","command":["`+long+`"]}`)
	got := auditRecords(t, log)
	if len(got) != 1 {
		t.Fatalf("want 1 record, got %d", len(got))
	}
	if strings.Count(got[0].Detail, "x") > maxAuditDetail {
		t.Fatalf("caller text not bounded: %d bytes", len(got[0].Detail))
	}
	if !strings.Contains(got[0].Detail, "error=") || !strings.Contains(got[0].Detail, "requester=local_test") {
		t.Fatalf("bounding dropped the error or the requester: %q", got[0].Detail[len(got[0].Detail)-120:])
	}
}
