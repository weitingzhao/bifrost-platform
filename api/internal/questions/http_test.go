package questions

import (
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
