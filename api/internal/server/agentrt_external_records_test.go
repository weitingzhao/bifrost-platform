package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func TestPipelineRunFact(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, pipeline, status, repo, url, wantRepo, kind string
		start                                             bool
	}{
		{"landing-ci-1", "bifrost-ci-platform", "True", "platform", "https://example/ignored.git", "platform", "ci", true},
		{"delivery-1", "bifrost-deliver-platform", "False", "", "https://example/platform.git", "platform", "delivery", false},
		{"other-1", "other", "True", "", "", "", "other", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := unstructured.Unstructured{Object: map[string]any{
				"spec": map[string]any{"pipelineRef": map[string]any{"name": tc.pipeline}, "params": []any{
					map[string]any{"name": "repo-url", "value": tc.url},
					map[string]any{"name": "revision", "value": commitForExternalRecord},
					map[string]any{"name": "secret", "value": "private"},
				}},
				"status": map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": tc.status, "reason": "two words", "message": "private"}}, "completionTime": at.Add(time.Minute).Format(time.RFC3339)},
			}}
			run.SetName(tc.name)
			run.SetUID(types.UID("uid"))
			run.SetCreationTimestamp(metav1.NewTime(at))
			run.SetLabels(map[string]string{"bifrost.io/repo": tc.repo})
			if tc.start {
				_ = unstructured.SetNestedField(run.Object, at.Format(time.RFC3339), "status", "startTime")
			}
			f, ok := pipelineRunFact(run)
			result := "Succeeded"
			if tc.status == "False" {
				result = "Failed"
			}
			if !ok || f.Result != result || f.Kind != tc.kind || f.Repo != tc.wantRepo || f.Revision != commitForExternalRecord || !f.StartedAt.Equal(at) || f.Reason != "" {
				t.Fatalf("fact: %+v %v", f, ok)
			}
			if _, exists := f.Body()["tree"]; exists {
				t.Fatal("legacy tree present")
			}
			if _, exists := f.Body()["tested_by"]; exists {
				t.Fatal("legacy tested_by present")
			}
			_ = unstructured.SetNestedSlice(run.Object, []any{map[string]any{"name": "platform-tree", "value": commitForExternalRecord}, map[string]any{"name": "tested-by", "value": "ci-original"}}, "status", "results")
			withResults, yes := pipelineRunFact(run)
			if !yes || withResults.Body()["tree"] != commitForExternalRecord || withResults.Body()["tested_by"] != "ci-original" {
				t.Fatalf("results: %+v", withResults)
			}
			raw, _ := json.Marshal(f.Body())
			if strings.Contains(string(raw), "private") {
				t.Fatal("private payload retained")
			}
			for _, reason := range []string{"Completed", strings.Repeat("A", 65)} {
				_ = unstructured.SetNestedField(run.Object, []any{map[string]any{"type": "Succeeded", "status": tc.status, "reason": reason}}, "status", "conditions")
				got, ok := pipelineRunFact(run)
				want := reason
				if len(reason) > 64 {
					want = ""
				}
				if !ok || got.Reason != want {
					t.Fatalf("reason: %+v %v", got, ok)
				}
			}
			_ = unstructured.SetNestedField(run.Object, []any{map[string]any{"type": "Succeeded", "status": "Unknown"}}, "status", "conditions")
			if _, ok := pipelineRunFact(run); ok {
				t.Fatal("unfinished run recorded")
			}
		})
	}
}

const commitForExternalRecord = "2333e00753f91c74c8b2f4d693a803f8e89e7892"

func TestExternalRecordQuestionObservationNeverReopensExpired(t *testing.T) {
	t.Parallel()
	rt := newQuestionRuntime(filepath.Join(t.TempDir(), "cards"), nil)
	raw := []byte(`{"idempotency_key":"0123456789abcdef","title":"Choose","urgency":"Now","meanwhile":"Wait"}`)
	res := rt.svc.Create(context.Background(), "thread-a", "reporter", "reporter", raw)
	if res.Code >= 300 {
		t.Fatalf("create: %+v", res)
	}
	items, err := rt.svc.Observations()
	if err != nil || len(items) != 1 {
		t.Fatalf("observations: %+v %v", items, err)
	}
	q := items[0]
	now := q.ExpiresAt.Add(time.Second)
	q.Status = "expired"
	terminal, ok := questionObservation(q, now)
	if !ok || terminal.Status != "expired" {
		t.Fatalf("terminal: %+v %v", terminal, ok)
	}
	q.Status = "open"
	if _, ok := questionObservation(q, now); ok {
		t.Fatal("stale open card could reverse terminal observation")
	}
	q.Status = "withdrawn"
	if f, ok := questionObservation(q, now); !ok || f.Status != "withdrawn" {
		t.Fatal("closed card lost")
	}
}

func TestExternalRecordApprovalObservationNeverReopensExpired(t *testing.T) {
	t.Parallel()
	at := time.Now().Add(-time.Hour)
	a := approvals.Approval{Number: 1, Status: approvals.StatusExpired, ExpiresAt: at}
	if _, ok := approvalObservation(a, time.Now()); !ok {
		t.Fatal("terminal card lost")
	}
	a.Status = approvals.StatusPending
	if _, ok := approvalObservation(a, time.Now()); ok {
		t.Fatal("expired pending card recorded")
	}
}
