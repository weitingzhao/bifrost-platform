package approvals

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
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

func unknownApproval(t *testing.T, svc *Service, base time.Time) (Approval, string) {
	t.Helper()
	svc.SetClock(func() time.Time { return base })
	rec, lease := claimed(t, svc)
	svc.SetClock(func() time.Time { return base.Add(leaseFor + unknownGrace + time.Second) })
	got, ok := svc.get(rec.ID)
	if !ok || got.Status != StatusUnknown || got.Execution == nil || got.Execution.LeaseID != lease {
		t.Fatalf("unknown = %s ok=%v lease=%q", got.Status, ok, lease)
	}
	return got, lease
}

func TestResolveEachOutcome(t *testing.T) {
	f := relay(t)
	for _, tc := range []struct {
		outcome, status, errText string
	}{
		{"succeeded", StatusExecuted, ""},
		{"failed", StatusFailed, "executor lost: lease lapsed with no result"},
		{"not_run", StatusFailed, "did not run (closed by the Owner)"},
	} {
		t.Run(tc.outcome, func(t *testing.T) {
			svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
			base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
			rec, _ := unknownApproval(t, svc, base)
			_ = deliveriesOf(t, svc, rec.ID)
			pushes := len(f.got())
			closedAt := base.Add(2 * time.Hour)
			svc.SetClock(func() time.Time { return closedAt })
			out := svc.resolve(rec.ID, tc.outcome, "checked on the host")
			if out.Status != http.StatusOK || out.Body["status"] != tc.status {
				t.Fatalf("resolve = %d %v", out.Status, out.Body)
			}
			got, ok := svc.find(rec.ID)
			if !ok || got.Status != tc.status || got.Error != tc.errText {
				t.Fatalf("stored status=%s error=%q", got.Status, got.Error)
			}
			if got.Resolution == nil || got.Resolution.Outcome != tc.outcome || got.Resolution.Note != "checked on the host" ||
				got.Resolution.By != "owner" || !got.Resolution.At.Equal(closedAt) {
				t.Fatalf("resolution = %#v", got.Resolution)
			}
			if got.Execution == nil || got.Execution.LeaseID != "" || !got.Execution.LeaseExpiresAt.IsZero() {
				t.Fatalf("lease not cleared: %#v", got.Execution)
			}
			if n := len(f.got()); n != pushes {
				t.Fatalf("resolve pushed: before %d after %d %+v", pushes, n, f.got())
			}
		})
	}
}

func TestResolveKeepsAnOldUnknownAmongNewerTerminals(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	rec, _ := unknownApproval(t, svc, base)
	err := svc.store.update(func(doc *file) error {
		for i := 0; i < keepClosed; i++ {
			doc.Approvals = append(doc.Approvals, Approval{
				ID:        fmt.Sprintf("newer-%d", i),
				Status:    StatusExecuted,
				CreatedAt: base,
				DecidedAt: base.Add(time.Duration(i+1) * time.Second),
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	const note = "the host was asleep"
	svc.SetClock(func() time.Time { return base.Add(2 * time.Hour) })
	out := svc.resolve(rec.ID, "not_run", note)
	if out.Status != http.StatusOK {
		t.Fatalf("resolve = %d %v", out.Status, out.Body)
	}
	got, ok := svc.find(rec.ID)
	if !ok {
		t.Fatal("resolved record was pruned in the same update")
	}
	if got.Resolution == nil || got.Resolution.Note != note || got.Resolution.Outcome != "not_run" {
		t.Fatalf("resolution = %#v", got.Resolution)
	}
	doc, err := svc.store.read()
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Approvals) != keepClosed {
		t.Fatalf("kept %d approvals, want %d", len(doc.Approvals), keepClosed)
	}
	if _, still := doc.find("newer-0"); still {
		t.Fatal("the oldest terminal was kept in place of the record just closed")
	}
}

func TestResolveRejectsWrongStatusAndBadNotes(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	rec, _ := unknownApproval(t, svc, base)
	pending := svc.create(context.Background(), "s", "cordon_node", "patch", "", map[string]any{"name": "n"})
	if pending.Status != http.StatusCreated {
		t.Fatalf("create = %d", pending.Status)
	}
	out := svc.resolve(pending.Approval.ID, "not_run", "no")
	if out.Status != http.StatusConflict || out.Body["status"] != StatusPending {
		t.Fatalf("pending = %d %v", out.Status, out.Body)
	}
	if again, _ := svc.find(rec.ID); again.Status != StatusUnknown || again.Resolution != nil {
		t.Fatalf("unknown record changed: %s %#v", again.Status, again.Resolution)
	}
	for _, note := range []string{"", "  ", "\n"} {
		if out := svc.resolve(rec.ID, "not_run", note); out.Status != http.StatusBadRequest {
			t.Fatalf("note %q = %d %v", note, out.Status, out.Body)
		}
	}
	if out := svc.resolve(rec.ID, "not_run", strings.Repeat("n", 501)); out.Status != http.StatusBadRequest {
		t.Fatalf("oversized = %d %v", out.Status, out.Body)
	}
	if out := svc.resolve(rec.ID, "maybe", "a note"); out.Status != http.StatusBadRequest {
		t.Fatalf("outcome = %d %v", out.Status, out.Body)
	}
	if out := svc.resolve("missing", "not_run", "a note"); out.Status != http.StatusNotFound {
		t.Fatalf("missing = %d", out.Status)
	}
	if still, _ := svc.find(rec.ID); still.Status != StatusUnknown || still.Resolution != nil {
		t.Fatal("refusals changed the record")
	}
}

func TestResolveLateResultDoesNotChangeTheRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approvals")
	svc := New(path, nil)
	base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	rec, lease := unknownApproval(t, svc, base)
	const keep = `{"z":1,"a":2}`
	stampUnknownField(t, path, rec.ID, keep)
	out := svc.resolve("#1", "failed", "token=SYNTHETIC_SECRET\nignored tail")
	if out.Status != http.StatusOK || out.Body["note"] != "token=[redacted]" {
		t.Fatalf("resolve = %d %v", out.Status, out.Body)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"legacy_flag":`+keep) {
		t.Fatalf("unknown field dropped: %s", raw)
	}
	if strings.Contains(string(raw), "SYNTHETIC_SECRET") || !strings.Contains(string(raw), "token=[redacted]") {
		t.Fatalf("stored note = %s", raw)
	}
	zero := 0
	late := svc.result(rec.ID, resultInput{LeaseID: lease, ExitCode: &zero})
	if late.Status != http.StatusConflict {
		t.Fatalf("late result = %d %v", late.Status, late.Body)
	}
	hb := svc.heartbeat(rec.ID, lease)
	if hb.Status != http.StatusConflict {
		t.Fatalf("late heartbeat = %d %v", hb.Status, hb.Body)
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(raw) {
		t.Fatalf("late result changed the file\nbefore %s\nafter  %s", raw, again)
	}
}

func TestResolveHTTPAdminOnlyByIDOrNumber(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.yaml")
	if err := os.WriteFile(authPath, []byte(`
tokens:
  - {name: viewer, role: viewer, token: viewer-test-token}
  - {name: operator, role: operator, token: operator-test-token}
  - {name: admin, role: admin, token: admin-test-token}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	auth, err := actuation.LoadAuth(authPath)
	if err != nil {
		t.Fatal(err)
	}
	auditPath := filepath.Join(dir, "audit.json")
	svc := New(filepath.Join(dir, "approvals"), actuation.NewAuditLog(auditPath))
	base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	rec, _ := unknownApproval(t, svc, base)
	r := chi.NewRouter()
	Mount(r, auth, svc)
	post := func(path, body, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr
	}
	body := `{"outcome":"succeeded","note":"it finished token=SYNTHETIC_SECRET"}`
	for _, tok := range []string{"viewer-test-token", "operator-test-token"} {
		if rec := post("/approvals/"+rec.ID+"/resolve", body, tok); rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s = %d %s", tok, rec.Code, rec.Body.String())
		}
	}
	if still, _ := svc.find(rec.ID); still.Status != StatusUnknown || still.Resolution != nil {
		t.Fatal("a refused token closed the record")
	}
	short := post("/approvals/1/resolve", `{"outcome":"succeeded","note":"`+strings.Repeat("ok", 250)+`"}`, "admin-test-token")
	if short.Code != http.StatusOK || !strings.Contains(short.Body.String(), `"status":"executed"`) {
		t.Fatalf("by number = %d %s", short.Code, short.Body.String())
	}
	got, _ := svc.find(rec.ID)
	if got.Status != StatusExecuted || got.Resolution == nil || got.Resolution.Outcome != "succeeded" || len([]rune(got.Resolution.Note)) != 500 {
		t.Fatalf("stored %#v note len %d", got.Resolution, len([]rune(got.Resolution.Note)))
	}
	audit, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(audit), "approval.resolve") || !strings.Contains(string(audit), "outcome=succeeded") {
		t.Fatalf("audit = %s", audit)
	}
	secret := post("/approvals/"+rec.ID+"/resolve", body, "admin-test-token")
	if secret.Code != http.StatusConflict || !strings.Contains(secret.Body.String(), `"status":"executed"`) {
		t.Fatalf("second = %d %s", secret.Code, secret.Body.String())
	}
}

func stampUnknownField(t *testing.T, path, id, keep string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(top["approvals"], &rows); err != nil {
		t.Fatal(err)
	}
	found := false
	for i, row := range rows {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(row, &obj); err != nil {
			t.Fatal(err)
		}
		if string(obj["id"]) != `"`+id+`"` {
			continue
		}
		obj["legacy_flag"] = json.RawMessage(keep)
		stamped, err := json.Marshal(obj)
		if err != nil {
			t.Fatal(err)
		}
		rows[i] = stamped
		found = true
	}
	if !found {
		t.Fatal("record to stamp was not stored")
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	top["approvals"] = encoded
	out, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}
