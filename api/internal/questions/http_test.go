package questions

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

func newHTTP(t *testing.T) (http.Handler, *Service) {
	t.Helper()
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.yaml")
	const yaml = `tokens:
  - {name: viewer, role: viewer, token: viewer-test-token}
  - {name: reporter, role: reporter, token: reporter-test-token}
  - {name: operator, role: operator, token: operator-test-token}
  - {name: admin, role: admin, token: admin-test-token}
`
	if err := os.WriteFile(authPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	auth, err := actuation.LoadAuth(authPath)
	if err != nil {
		t.Fatal(err)
	}
	svc := testSvc(t)
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		Mount(r, auth, svc)
	})
	return r, svc
}

func call(t *testing.T, h http.Handler, method, path, body, token, session string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if session != "" {
		req.Header.Set("X-Bifrost-Session", session)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func createBodyJSON(t *testing.T, key string) string {
	t.Helper()
	return string(mustJSON(t, map[string]any{
		"idempotency_key": key,
		"title":           "Where is the quota?",
		"urgency":         "Before tonight",
		"meanwhile":       "The thread waits",
	}))
}

func TestRolesSessionAndBodyLimit(t *testing.T) {
	h, _ := newHTTP(t)
	viewer := call(t, h, http.MethodPost, "/api/v1/questions", createBodyJSON(t, "0123456789abcdef"), "viewer-test-token", "thread-a")
	if viewer.Code != http.StatusUnauthorized {
		t.Fatalf("viewer create = %d %s", viewer.Code, viewer.Body)
	}
	missing := call(t, h, http.MethodPost, "/api/v1/questions", createBodyJSON(t, "0123456789abcdef"), "reporter-test-token", "")
	if missing.Code != http.StatusBadRequest || !strings.Contains(missing.Body.String(), "X-Bifrost-Session") {
		t.Fatalf("missing session = %d %s", missing.Code, missing.Body)
	}
	huge := `{"idempotency_key":"0123456789abcdef","title":"` + repeat('a', maxBodyBytes) + `"}`
	tooBig := call(t, h, http.MethodPost, "/api/v1/questions", huge, "reporter-test-token", "thread-a")
	if tooBig.Code != http.StatusRequestEntityTooLarge || !strings.Contains(tooBig.Body.String(), "16 KiB") {
		t.Fatalf("body = %d %s", tooBig.Code, tooBig.Body)
	}
	created := call(t, h, http.MethodPost, "/api/v1/questions", createBodyJSON(t, "0123456789abcdef"), "reporter-test-token", "thread-a")
	if created.Code != http.StatusCreated {
		t.Fatal(created.Body.String())
	}
	id := created.Body.String()
	var qid string
	for _, line := range strings.Split(id, `"id":"`) {
		if strings.HasPrefix(line, "q_") {
			qid = line[:18]
			break
		}
	}
	wrong := call(t, h, http.MethodPost, "/api/v1/questions/"+qid+"/withdraw", `{"reason":"no"}`, "operator-test-token", "thread-b")
	if wrong.Code != http.StatusForbidden || !strings.Contains(wrong.Body.String(), "session is not the requester") {
		t.Fatalf("withdraw = %d %s", wrong.Code, wrong.Body)
	}
	ack := call(t, h, http.MethodPost, "/api/v1/questions/"+qid+"/ack", `{}`, "reporter-test-token", "thread-a")
	if ack.Code != http.StatusConflict || !strings.Contains(ack.Body.String(), "not answered") {
		t.Fatalf("ack = %d %s", ack.Code, ack.Body)
	}
}

func TestTrailingJSONIsRejected(t *testing.T) {
	h, svc := newHTTP(t)
	created := call(t, h, http.MethodPost, "/api/v1/questions", createBodyJSON(t, "0123456789abcdef"), "reporter-test-token", "thread-a")
	if created.Code != http.StatusCreated {
		t.Fatal(created.Body.String())
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil || body.ID == "" {
		t.Fatal(created.Body.String())
	}
	id := body.ID
	before := readRaw(t, svc)
	answerURL := "/api/v1/questions/" + id + "/answer"
	withdrawURL := "/api/v1/questions/" + id + "/withdraw"
	ackURL := "/api/v1/questions/" + id + "/ack"
	create := createBodyJSON(t, "abcdef0123456789")
	rejects := []struct {
		name, path, raw, token, session, want string
	}{
		{"create extra value", "/api/v1/questions", create + `{"action":"release"}`, "reporter-test-token", "thread-a", "invalid json body"},
		{"create garbage", "/api/v1/questions", `{}garbage`, "reporter-test-token", "thread-a", "invalid json body"},
		{"create unknown field", "/api/v1/questions", `{"idempotency_key":"abcdef0123456789","title":"t","urgency":"u","meanwhile":"m","action":"release"}`, "reporter-test-token", "thread-a", "unknown field action"},
		{"answer extra value", answerURL, `{"text":"yes"}{"action":"release"}`, "admin-test-token", "owner-console", "invalid json body"},
		{"answer garbage", answerURL, `{}garbage`, "admin-test-token", "owner-console", "invalid json body"},
		{"answer trailing brace", answerURL, `{"text":"yes"}}`, "admin-test-token", "owner-console", "invalid json body"},
		{"answer unknown field", answerURL, `{"text":"yes","action":"release"}`, "admin-test-token", "owner-console", "unknown field action"},
		{"withdraw extra value", withdrawURL, `{"reason":"done"}{"action":"release"}`, "reporter-test-token", "thread-a", "invalid json body"},
		{"withdraw garbage", withdrawURL, `{"reason":"done"}garbage`, "reporter-test-token", "thread-a", "invalid json body"},
		{"withdraw unknown field", withdrawURL, `{"reason":"done","action":"release"}`, "reporter-test-token", "thread-a", "unknown field action"},
		{"ack extra value", ackURL, `{}{"action":"release"}`, "reporter-test-token", "thread-a", "invalid json body"},
		{"ack garbage", ackURL, `{}garbage`, "reporter-test-token", "thread-a", "invalid json body"},
		{"ack unknown field", ackURL, `{"action":"release"}`, "reporter-test-token", "thread-a", "unknown field action"},
	}
	for _, tc := range rejects {
		got := call(t, h, http.MethodPost, tc.path, tc.raw, tc.token, tc.session)
		if got.Code != http.StatusBadRequest || !strings.Contains(got.Body.String(), tc.want) {
			t.Fatalf("%s = %d %s", tc.name, got.Code, got.Body.String())
		}
	}
	if after := readRaw(t, svc); after != before {
		t.Fatal("a rejected body changed the document")
	}
	okAnswer := call(t, h, http.MethodPost, answerURL, "{\"option\":\"none\",\"text\":\"yes\",\"channel\":\"console\"}\n  ", "admin-test-token", "owner-console")
	if okAnswer.Code != http.StatusOK {
		t.Fatalf("answer trailing space = %d %s", okAnswer.Code, okAnswer.Body.String())
	}
	okAck := call(t, h, http.MethodPost, ackURL, "{}\n", "reporter-test-token", "thread-a")
	if okAck.Code != http.StatusOK {
		t.Fatalf("ack trailing space = %d %s", okAck.Code, okAck.Body.String())
	}
	okCreate := call(t, h, http.MethodPost, "/api/v1/questions", createBodyJSON(t, "fedcba9876543210")+"\n", "reporter-test-token", "thread-b")
	if okCreate.Code != http.StatusCreated {
		t.Fatalf("create trailing space = %d %s", okCreate.Code, okCreate.Body.String())
	}
	var second struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(okCreate.Body.Bytes(), &second); err != nil || second.ID == "" {
		t.Fatal(okCreate.Body.String())
	}
	okWithdraw := call(t, h, http.MethodPost, "/api/v1/questions/"+second.ID+"/withdraw", "{\"reason\":\"done\"}\n", "reporter-test-token", "thread-b")
	if okWithdraw.Code != http.StatusOK {
		t.Fatalf("withdraw trailing space = %d %s", okWithdraw.Code, okWithdraw.Body.String())
	}
}
