package lineage

import (
	"context"
	"encoding/json"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPlanHistoryPinsHeadAndRefusesTruncation(t *testing.T) {
	a, b, c := sha40("a"), sha40("b"), sha40("c")
	msg := "change\n\nChange-Id: I" + a
	truncated := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/repos/bifrost/repo/branches/main" {
			_ = json.NewEncoder(w).Encode(map[string]any{"commit": map[string]any{"id": b}})
			return
		}
		ref := r.URL.Query().Get("sha")
		if ref == "main" {
			t.Error("pagination did not pin head")
		}
		if ref != a && ref != b {
			http.Error(w, "unknown", 404)
			return
		}
		sha, message := a, msg
		parents := []map[string]any{}
		if ref == b {
			sha = b
			message = msg + "\n(cherry picked from commit " + a + ")"
			if truncated {
				parents = append(parents, map[string]any{"sha": c})
			}
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"sha": sha, "commit": map[string]any{"message": message, "committer": map[string]any{"date": time.Now()}}, "parents": parents}})
	}))
	defer srv.Close()
	acc := Access{Base: srv.URL, Org: "bifrost"}
	h, err := PlanHistory(context.Background(), acc, srv.Client(), "repo", "", a, "", 2)
	if err != nil || h.State != landing.Effective || h.Commit != b {
		t.Fatalf("%+v %v", h, err)
	}
	truncated = true
	h, err = PlanHistory(context.Background(), acc, srv.Client(), "repo", "", a, "", 2)
	if err == nil || h.State != landing.Unknown {
		t.Fatalf("partial scan became truth: %+v %v", h, err)
	}
}
