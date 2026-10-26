package server

import (
	"context"
	"testing"
)

func TestPlanDeploymentCannotBorrowAnotherApplication(t *testing.T) {
	s := &Server{}
	calls := 0
	src := s.agentRuntimePlanSources(func(_ context.Context, env string) (string, map[string]any, bool) {
		calls++
		if env != "prod" {
			t.Fatal(env)
		}
		return "sha", map[string]any{"images": "actual digests"}, true
	})
	if _, _, ok := src.Deployed(context.Background(), "another-app", "prod"); ok || calls != 0 {
		t.Fatal("wrong application's deployment was used")
	}
	if _, _, ok := src.Deployed(context.Background(), "bifrost-platform", "prod"); !ok || calls != 1 {
		t.Fatal("platform deployment was not read")
	}
	if src.History != nil {
		t.Fatal("missing git reader granted history")
	}
}
