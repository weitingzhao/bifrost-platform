package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

func TestAssessmentChecksApprovalStateAndLeavesMaterialUntouched(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "approvals")
	s := &Server{approvals: approvals.New(path, nil)}
	for _, status := range []string{"pending", "approved", "running", "unknown", "executed", "failed", "rejected", "expired"} {
		a := approvals.Approval{ID: "a463", Number: 463, Status: status, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
			Params: map[string]any{"material_digest": "unchanged"}, ParamsHash: "unchanged"}
		raw, _ := json.Marshal(map[string]any{"approvals": []approvals.Approval{a}})
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		err := s.checkAssessment(ctx, store.AssessApproval, "463")
		if (err == nil) != (status == "pending") {
			t.Fatalf("%s: %v", status, err)
		}
		if status != "pending" && !errors.Is(err, store.ErrState) {
			t.Fatalf("wrong rejection: %v", err)
		}
		// HTTP enrichment adds only a response field. No request or material is rewritten.
		s.approvals.Assessment = func(context.Context, string) map[string]any { return map[string]any{"events": []any{}} }
		router := chi.NewRouter()
		router.Get("/approvals/{id}", s.approvals.HandleGet)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/approvals/463", nil))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"assessment":{"events":[]}`) {
			t.Fatalf("view: %d %s", rec.Code, rec.Body.String())
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(raw, after) {
			t.Fatalf("%s view changed material: %v", status, err)
		}
	}
	a := approvals.Approval{ID: "a463", Number: 463, Status: "pending", ExpiresAt: time.Now().Add(-time.Second)}
	raw, _ := json.Marshal(map[string]any{"approvals": []approvals.Approval{a}})
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.checkAssessment(ctx, store.AssessApproval, "463"); !errors.Is(err, store.ErrState) {
		t.Fatalf("expiry: %v", err)
	}
}

func TestAssessmentChecksCurrentCardState(t *testing.T) {
	ctx := context.Background()
	rt := newQuestionRuntime(filepath.Join(t.TempDir(), "questions"), nil)
	s := &Server{questionRuntime: rt}
	id, err := rt.agentRuntimeAsk()(ctx, store.Card{Key: strings.Repeat("ef", 16), Title: "Choose a direction", Body: "Advice only",
		Options: []string{"Continue", "Stop"}, Recommended: 1, RecommendedWhy: "ready", Meanwhile: "waiting", OwnerDuty: "goal", Urgency: "when you can"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.checkAssessment(ctx, store.AssessCard, id); err != nil {
		t.Fatalf("open card: %v", err)
	}
	if err := rt.agentRuntimeWithdraw()(ctx, id, "the work ended"); err != nil {
		t.Fatal(err)
	}
	if err := s.checkAssessment(ctx, store.AssessCard, id); !errors.Is(err, store.ErrState) || !strings.Contains(err.Error(), "withdrawn") {
		t.Fatalf("closed card: %v", err)
	}
}
