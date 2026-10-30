package questions_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

func TestAnsweredQuestionThreadInboxPG(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	w := store.Writer{Actor: "thread:local_question", Token: "fixture"}
	if _, _, err := store.UploadArtifact(ctx, pool, w, "message", "text/plain", []byte("register")); err != nil {
		t.Fatal(err)
	}
	var now time.Time
	if err := pool.QueryRow(ctx, `SELECT now()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	for i, ruled := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "questions")
		id := "q_" + strings.Repeat(string(rune('a'+i)), 16)
		q := questions.Question{ID: id, Number: i + 1, Status: questions.StatusOpen, Requester: "agentrt",
			RequesterThread: "local_question", CreatedAt: now, ExpiresAt: now.Add(time.Hour), Recommended: "a",
			Options: []questions.Option{{Key: "a", Label: "Proceed"}, {Key: "b", Label: "Wait"}}}
		raw, _ := json.Marshal(map[string]any{"questions": []questions.Question{q}})
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		svc := questions.New(path, nil)
		svc.SetClock(func() time.Time { return now })
		var r questions.Result
		if ruled {
			r = svc.AnswerAsRule(ctx, "agentrt", "rule card-auto-answer v1", id, "a", now.Add(time.Hour), "")
		} else {
			r = svc.Answer(ctx, "console", "owner", "admin", id, []byte(`{"option":"a","channel":"console"}`))
		}
		if r.Code != 200 {
			t.Fatalf("answer=%+v", r)
		}
		answeredRaw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		svc.SetThreadInbox(func(ctx context.Context, key, to, body string) error {
			return store.PostConclusion(ctx, pool, key, to, body)
		})
		svc.RetryThreadInbox(ctx)
		svc.RetryThreadInbox(ctx)
		mail, err := store.Unread(ctx, pool, w, 100)
		if err != nil || len(mail) != 1 || !strings.Contains(mail[0].Body, id) ||
			!strings.Contains(mail[0].Body, "An answer is not an approval.") ||
			!strings.Contains(mail[0].Body, "Option: a — Proceed") || strings.Contains(mail[0].Body, "rule card-auto-answer") != ruled {
			t.Fatalf("mail=%+v err=%v", mail, err)
		}
		if _, err := store.AckMessages(ctx, pool, w, []string{mail[0].ID}); err != nil {
			t.Fatal(err)
		}
		// Restore the same business bytes and recreate the service; only PG has a receipt.
		if err := os.WriteFile(path, answeredRaw, 0o600); err != nil {
			t.Fatal(err)
		}
		svc = questions.New(path, nil)
		svc.SetThreadInbox(func(ctx context.Context, key, to, body string) error {
			return store.PostConclusion(ctx, pool, key, to, body)
		})
		svc.RetryThreadInbox(ctx)
		history, err := store.Thread(ctx, pool, w.Actor, "controller", 100)
		if err != nil || len(history) != i+1 {
			t.Fatalf("business replay duplicated history: %d %v", len(history), err)
		}
		for _, m := range history {
			if m.AckedAt == nil || !strings.Contains(m.Body, "An answer is not an approval.") {
				t.Fatal("replay lost acknowledgement or authorization boundary")
			}
		}
	}
}
