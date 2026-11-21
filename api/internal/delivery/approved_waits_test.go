package delivery

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"k8s.io/client-go/kubernetes/fake"
)

// #524, 2026-11-14 00:32Z: the Owner approved a PROD release of platform
// while the STG run bifrost-deliver-platform-1794616265 held the release
// window. The approved start answered "REFUSED: missing who; pass who
// matching the release window holder (who=bifrost-deliver-platform-1794616265
// ...)", which the approval's executor does not retry, and the request failed
// in 28 ms. The same start now answers with the refusal that executor retries
// ("REFUSED: release window held by ..."), names the holder, creates nothing,
// and starts once the window is free.
func TestApprovedRunWaitsForAWindowAnotherRunHolds524(t *testing.T) {
	const name = "bifrost-deliver-platform-prod"
	const holder = "bifrost-deliver-platform-1794616265"
	ctx := context.Background()
	pipe := baselinePipelines(t)[name].DeepCopy()
	pipe.SetNamespace("cicd")
	s, dyn, _ := contractService(t, pipe, "unknown")
	cs := fake.NewSimpleClientset(readyAmd64Node())
	s.SetClientsetForTest(cs)
	if _, err := putReleaseWindow(ctx, cs, "cicd", windowInput{Who: holder, What: "bifrost-platform,bifrost-ui", Reason: "PipelineRun/" + holder}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	extra := map[string]string{"uiRevision": strings.Repeat("b", 40)}
	approved := actions.WithCreateAttempt(ctx, "approval-524", 1)
	resp, _, err := s.StartPipelineRun(approved, name, factSHA1, "", "", extra)
	if err == nil || !strings.HasPrefix(resp.Message, "REFUSED: release window held by ") || !strings.Contains(resp.Message, "who="+holder) || strings.Contains(resp.Message, "missing who") {
		t.Fatalf("approved start under a run's window: %+v %v", resp, err)
	}
	assertNoContractCreate(t, dyn)

	// A caller that is not an approved run and passes no who is still told
	// to pass one: it can.
	resp, _, err = s.StartPipelineRun(ctx, name, factSHA1, "", "", extra)
	if err == nil || !strings.HasPrefix(resp.Message, missingWhoRefusal) {
		t.Fatalf("a direct start with no who: %+v %v", resp, err)
	}

	// The holder's run ends; its window is over. The retry starts.
	if _, err := putReleaseWindow(ctx, cs, "cicd", windowInput{Who: holder, What: "bifrost-platform,bifrost-ui", Reason: "PipelineRun/" + holder}, time.Now().UTC().Add(-2*windowLifetime)); err != nil {
		t.Fatal(err)
	}
	resp, _, err = s.StartPipelineRun(actions.WithCreateAttempt(ctx, "approval-524", 2), name, factSHA1, "", "", extra)
	if err != nil || !resp.OK {
		t.Fatalf("the retry after the window is free: %+v %v", resp, err)
	}
	createdContractRun(t, dyn)
}
