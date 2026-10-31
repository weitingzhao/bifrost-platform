package store

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

func TestS5LatestReviewReaderDoesNotResurrectPassingRow(t *testing.T) {
	t.Parallel()
	for _, which := range []string{"pass", "fail", "unbound", "missing artifact", "corrupt artifact", "wrong A", "missing reviewer"} {
		t.Run(which, func(t *testing.T) {
			f := landing.Facts{Repo: "bifrost-platform", Task: "t_subject", Attempt: "a_subject", A: strings.Repeat("a", 40), Base: strings.Repeat("b", 40), Mission: "M-1"}
			body := []byte("Independent review of accepted A.")
			tx := &rowTx{row: func(q string, d ...any) error {
				if strings.Contains(q, "v.accepts_attempt") {
					values := []string{"v_accept", f.Attempt, f.A, f.Attempt, "exec:codex", "openai", "host", "dev"}
					for i, v := range values {
						*d[i].(*string) = v
					}
					return nil
				}
				if strings.Contains(q, "verdict='pass'") || strings.Contains(q, "JOIN agentrt.attempt a ON") && !strings.Contains(q, "LEFT JOIN agentrt.attempt") || !strings.Contains(q, "ORDER BY v.created_at DESC,v.verification_id DESC") {
					t.Fatal("review query hides latest refusal", q)
				}
				if which == "missing reviewer" {
					return pgx.ErrNoRows
				}
				values := []string{"v_latest", f.Attempt, f.A, "pass", "sha256:" + landing.S5Hash(body), "t_review", "a_review", "exec:claude", "anthropic", f.Task, "review", f.Mission, "dev"}
				switch which {
				case "fail":
					values[3] = "fail"
				case "unbound":
					values[1] = ""
				case "missing artifact":
					body = nil
				case "corrupt artifact":
					body = []byte("replaced artifact")
				case "wrong A":
					values[2] = strings.Repeat("c", 40)
				}
				for i, v := range values {
					*d[i].(*string) = v
				}
				*d[len(values)].(*[]byte) = body
				return nil
			}}
			b, err := readS5Review(context.Background(), tx, f)
			if err != nil {
				t.Fatal(err)
			}
			want := "S5_REVIEW_BINDING"
			if which == "pass" {
				want = ""
			}
			if which == "missing artifact" || which == "missing reviewer" {
				want = "S5_REVIEW_MISSING"
			}
			if b.Problem != want {
				t.Fatalf("want %s got %+v", want, b)
			}
		})
	}
}
