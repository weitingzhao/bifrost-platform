package agentrtapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestMaterialResponseReadsMainEveryTime(t *testing.T) {
	t.Parallel()
	m0 := strings.Repeat("a", 40)
	m := &store.LandingGrantMaterial{Candidate: store.GrantCandidate{Repo: "bifrost-platform", M0: m0}}
	head, readErr, calls := m0, error(nil), 0
	a := &API{MainHead: func(context.Context, string) (string, error) { calls++; return head, readErr }}
	for i := range 3 {
		if i == 1 {
			head = strings.Repeat("b", 40)
		}
		if i == 2 {
			readErr = errors.New("unreadable")
		}
		w := httptest.NewRecorder()
		a.writeLandingMaterial(w, httptest.NewRequest(http.MethodGet, "/", nil), m)
		var got struct {
			Known   bool `json:"main_known"`
			Matches bool `json:"main_is_M0"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Matches != (i == 0) || got.Known != (i != 2) {
			t.Fatalf("read %d reused main: %+v", i, got)
		}
	}
	if calls != 3 {
		t.Fatalf("main read %d times", calls)
	}
}

func TestMaterialCapacityResponses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  error
		code string
	}{{store.ErrMaterialPending, "material_pending"}, {store.ErrMaterialBusy, "material_busy"}} {
		w := httptest.NewRecorder()
		if !materialRetry(w, tc.err) || w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "2" {
			t.Fatalf("unbounded/unrecognized wait: %v", w)
		}
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["error"] != tc.code {
			t.Fatalf("%v %v", body, err)
		}
	}
	if materialRetry(httptest.NewRecorder(), errors.New("Git failure")) {
		t.Fatal("real evidence failure treated as capacity")
	}
}
