//bifrost:owner owner

package store_test

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// question_seen keeps the option the card recommended when the reader gave
// it, open or answered, and leaves it out when it did not.
func TestQuestionSeenRecommendedBody(t *testing.T) {
	t.Parallel()
	q := answered("Q1", "a")
	q.Recommended = "b"
	body, err := store.QuestionSeenBodyForTest("Q1", q)
	ok(t, err)
	if body["recommended"] != "b" || body["option"] != "a" {
		t.Fatalf("body %v", body)
	}
	body, err = store.QuestionSeenBodyForTest("Q1", store.Question{ID: "Q1", Status: "open", Recommended: "a"})
	ok(t, err)
	if body["recommended"] != "a" {
		t.Fatalf("open body %v", body)
	}
	body, err = store.QuestionSeenBodyForTest("Q1", answered("Q1", "a"))
	ok(t, err)
	if _, has := body["recommended"]; has {
		t.Fatalf("a card read without its recommendation kept one: %v", body)
	}
}

// What a writer decides does not depend on the recommendation: SeeQuestion
// keeps it, and records again only when the card read gives another.
func TestQuestionSeenRecommendedSeen(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	q := answered("Q7", "a")
	q.Recommended = "b"
	if did, err := store.SeeQuestion(ctx, w.rt, "Q7", questionIs(q)); err != nil || !did {
		t.Fatalf("seen %v %v", did, err)
	}
	if did, err := store.SeeQuestion(ctx, w.rt, "Q7", questionIs(q)); err != nil || did {
		t.Fatalf("seen again %v %v", did, err)
	}
	var rec string
	ok(t, w.rt.QueryRow(ctx, `SELECT COALESCE(body_json->>'recommended', '') FROM agentrt.event WHERE event_type = 'question_seen'
	  AND body_json->>'question' = 'Q7' ORDER BY event_id DESC LIMIT 1`).Scan(&rec))
	if rec != "b" {
		t.Fatalf("recommended kept as %q", rec)
	}
}
