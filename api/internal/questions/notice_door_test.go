package questions

import (
	"context"
	"net/http"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

func TestQuestionNoticePassesTheDoorAndARefusalLeavesTheRecord(t *testing.T) {
	t.Setenv("APPROVAL_NOTIFY_URL", "")
	t.Setenv("APPROVAL_NOTIFY_TOKEN", "")
	s := testSvc(t)
	var sent notice.Message
	s.post = func(ctx context.Context, msg notice.Message) ([]notice.Delivery, error) {
		sent = msg
		return notice.Send(ctx, msg)
	}
	created := mustCreate(t, s, "thread-a", "0123456789abcdef", nil)
	id := created["id"].(string)
	if _, err := notice.Send(context.Background(), sent); err != nil {
		t.Fatalf("question notice was refused: %v\ntitle %q\nbody %q", err, sent.Title, sent.Message)
	}
	if s.Get(context.Background(), "thread-a", "reporter", "reporter", id).Code != http.StatusOK {
		t.Fatal("clean question was not stored")
	}

	s.post = func(ctx context.Context, msg notice.Message) ([]notice.Delivery, error) {
		msg.Title += "\u202e"
		return notice.Send(ctx, msg)
	}
	again := mustCreate(t, s, "thread-a", "fedcba9876543210", map[string]any{"title": "A second question"})
	refusedID := again["id"].(string)
	if s.Get(context.Background(), "thread-a", "reporter", "reporter", refusedID).Code != http.StatusOK {
		t.Fatal("a refused push uncreated the question")
	}
}
