package questions

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnerQuestionHome(t *testing.T) {
	for _, env := range []string{"stg", "dev", "", "unknown", "prod"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv("OPS_VIEWER_ENV", env)
			svc := New(filepath.Join(t.TempDir(), "questions"), nil)
			svc.post = acceptPost()
			raw := []byte(`{"idempotency_key":"home-test-00000001","title":"Sign off","urgency":"when you can","meanwhile":"wait","options":[{"label":"Yes"},{"label":"No"}],"recommended":1}`)
			got := svc.Create(context.Background(), "thread-test", "reporter", "reporter", raw)
			if env == "prod" {
				if got.Code != 201 {
					t.Fatalf("PROD: %+v", got)
				}
			} else if got.Code != 403 || !strings.Contains(fmt.Sprint(got.Body), "ops.bifrost.lan") {
				t.Fatalf("Owner request: %+v", got)
			}
			// Internal rule cards are not human approvals; the HTTP path cannot opt in.
			rule := svc.CreateForRule(context.Background(), RuntimeSession, "runtime", "reporter", raw)
			if rule.Code != 201 {
				t.Fatalf("rule: %+v", rule)
			}
			if bad := svc.CreateForRule(context.Background(), "thread-test", "reporter", "reporter", raw); bad.Code != 403 {
				t.Fatalf("rule spoof: %+v", bad)
			}
		})
	}
}
