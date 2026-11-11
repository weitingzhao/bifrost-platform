package controller_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestExternalRecordsBackfillAndUnreadableKind(t *testing.T) {
	t.Parallel()
	pool := setup(t)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	unreadable := false
	c := controller.Controller{DB: pool, Sources: controller.Sources{
		Approvals: func(context.Context) ([]store.ApprovalFact, error) {
			if unreadable {
				return nil, errors.New("unreadable")
			}
			return []store.ApprovalFact{{Number: 16, Status: "pending", Action: "deploy", Tier: "T2", CreatedAt: at}}, nil
		},
		Questions: func(context.Context) ([]store.Question, error) {
			id := "q_external"
			if unreadable {
				id = "q_other"
			}
			return []store.Question{{ID: id, Status: "open", Number: 1, CreatedAt: at.Format(time.RFC3339)}}, nil
		},
		PipelineRuns: func(context.Context) ([]store.PipelineRun, error) {
			return []store.PipelineRun{{Run: "ci-1", UID: "uid-1", Pipeline: "bifrost-ci-platform", Kind: "ci", Result: "Succeeded", CreatedAt: at, StartedAt: at, CompletedAt: at}}, nil
		},
	}}
	for i, want := range []int{2, 0} {
		n, err := c.ExternalRecords(ctx)
		if err != nil || n != want {
			t.Fatalf("card pass %d: %d %v", i, n, err)
		}
		n, err = c.PipelineRecords(ctx)
		runWant := 1
		if i > 0 {
			runWant = 0
		}
		if err != nil || n != runWant {
			t.Fatalf("run pass %d: %d %v", i, n, err)
		}
	}
	unreadable = true
	if n, err := c.ExternalRecords(ctx); n != 1 || err == nil {
		t.Fatalf("unreadable approval stopped question: %d %v", n, err)
	}
}
