package store_test

import (
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/questioncards"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

func b5hFillQuota(t *testing.T, svc *questions.Service) []string {
	t.Helper()
	var ids []string
	for i := 0; i < 100; i++ {
		res := svc.Create(ctx, questions.RuntimeSession, questions.RuntimeSession, "operator", []byte(fmt.Sprintf(`{"title":"Quota %d","body":"Quota probe","urgency":"now","meanwhile":"Wait for the quota probe","options":[{"label":"Yes"},{"label":"No"}],"idempotency_key":"b5h-quota-fill-%04d"}`, i, i)))
		if res.Code == http.StatusTooManyRequests {
			return ids
		}
		if res.Code != http.StatusCreated {
			t.Fatal(res)
		}
		ids = append(ids, res.Body.(map[string]any)["id"].(string))
	}
	t.Fatal("question quota was never enforced")
	return nil
}

func b5hReleaseQuota(t *testing.T, svc *questions.Service, ids []string) {
	t.Helper()
	for _, id := range ids {
		res := svc.Withdraw(ctx, questions.RuntimeSession, questions.RuntimeSession, "operator", id, []byte(`{"reason":"fixture quota released"}`))
		if res.Code != http.StatusOK {
			t.Fatal(res)
		}
	}
}

// Serial: production question authority reads OPS_VIEWER_ENV.
func TestB5HQuestionQuotaFixtureAndRenamedAdmin(t *testing.T) {
	t.Setenv("APPROVAL_NOTIFY_URL", "")
	t.Setenv("OPS_VIEWER_ENV", "prod")
	svc := questions.New(filepath.Join(t.TempDir(), "questions"), nil)
	svc.SetClock(func() time.Time { return time.Date(2026, 10, 29, 16, 0, 0, 0, time.UTC) })
	ids := b5hFillQuota(t, svc)
	if len(ids) != 3 {
		t.Fatal("expected requester open quota", len(ids))
	}
	b5hReleaseQuota(t, svc, ids)
	res := svc.Create(ctx, questions.RuntimeSession, questions.RuntimeSession, "operator", []byte(`{"title":"Actual budget question","urgency":"now","meanwhile":"Wait for the budget answer","options":[{"label":"Grant"},{"label":"Withhold"}],"idempotency_key":"b5h-actual-budget-card"}`))
	if res.Code != http.StatusCreated {
		t.Fatal("quota did not release", res)
	}
	id := res.Body.(map[string]any)["id"].(string)
	res = svc.Answer(ctx, "console-session", "renamed-admin", "admin", id, []byte(`{"option":"a","channel":"console"}`))
	if res.Code != http.StatusOK {
		t.Fatal(res)
	}
	q, found, err := svc.Lookup(id)
	ok(t, err)
	if !found || q.Answer == nil || q.Answer.By != "renamed-admin" || q.Answer.Channel != "console" || q.Answer.Approval {
		t.Fatal("real adapter identity contract", q)
	}
	fact, found, err := questioncards.New(svc).Lookup(ctx, id)
	ok(t, err)
	if !found || !fact.OwnerAnswer || fact.AnsweredBy != "renamed-admin" || fact.Option != "a" {
		t.Fatal("question source lost authenticated identity", fact)
	}
}
