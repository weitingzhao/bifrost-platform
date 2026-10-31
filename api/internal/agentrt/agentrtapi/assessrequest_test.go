package agentrtapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestAssessmentRequestIsOwnerOnlyEvenWithoutDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.yaml")
	must(t, os.WriteFile(path, []byte(`tokens:
  - {name: viewer, role: viewer, token: fixture-viewer-463}
  - {name: operator, role: operator, token: fixture-operator-463}
  - {name: reporter, role: reporter, token: fixture-reporter-463}
  - {name: agentd:h1, role: agentd, token: fixture-agentd-463}
  - {name: owner, role: admin, token: fixture-owner-463}
`), 0600))
	auth, err := actuation.LoadAuth(path)
	must(t, err)
	router := chi.NewRouter()
	agentrtapi.Mount(router, auth, &agentrtapi.API{})
	for _, tc := range []struct {
		token string
		code  int
	}{
		{"", 401}, {"viewer", 401}, {"operator", 401}, {"reporter", 401}, {"agentd", 401}, {"owner", 503},
	} {
		r := httptest.NewRequest("POST", "/agentrt/assessments/approval/463", nil)
		if tc.token != "" {
			r.Header.Set("Authorization", "Bearer fixture-"+tc.token+"-463")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Errorf("%s: %d %s", tc.token, w.Code, w.Body.String())
		}
	}
}

func TestAssessmentRequestRouteRecordsOnceAndRejectsClosed(t *testing.T) {
	pool, _ := world(t)
	closed := false
	s := server(t, pool, func(a *agentrtapi.API) {
		a.CheckAssessment = func(_ context.Context, kind, id string) error {
			if closed {
				return fmt.Errorf("%w: item is closed", store.ErrState)
			}
			return nil
		}
	})
	for _, kind := range []string{store.AssessCard, store.AssessApproval} {
		for range 2 {
			r, err := http.NewRequest("POST", s.URL+"/api/v1/agentrt/assessments/"+kind+"/463", nil)
			must(t, err)
			r.Header.Set("Authorization", "Bearer fixture-admin-1")
			res, err := s.Client().Do(r)
			must(t, err)
			raw, err := io.ReadAll(res.Body)
			res.Body.Close()
			must(t, err)
			if res.StatusCode != http.StatusOK {
				t.Fatalf("%s: %d %s", kind, res.StatusCode, raw)
			}
			// Successful POSTs use the same event array as GET /events and
			// Console's parseAssessmentStream; errors are objects with message.
			var events []struct {
				Type string         `json:"event_type"`
				Body map[string]any `json:"body_json"`
				At   string         `json:"at"`
			}
			must(t, json.Unmarshal(raw, &events))
			if len(events) != 1 || events[0].Type != "note" || events[0].At == "" {
				t.Fatalf("%s: %d %v", kind, res.StatusCode, events)
			}
			request, _ := events[0].Body["assessment_requested"].(map[string]any)
			if request["kind"] != kind || request["id"] != "463" {
				t.Fatalf("%s: wrong request record %v", kind, request)
			}
		}
	}
	closed = true
	if code, body := call(t, s, "admin", "POST", "assessments/approval/463", nil); code != 409 || !strings.Contains(fmt.Sprint(body), "item is closed") {
		t.Fatalf("closed: %d %v", code, body)
	}
}
