package questions

import (
	"path/filepath"
	"testing"
	"time"
)

func TestObservationsRetainExpiredAndClosedWithoutWriting(t *testing.T) {
	t.Parallel()
	s := testSvc(t)
	mustCreate(t, s, "thread-a", "0123456789abcdef", nil)
	second := mustCreate(t, s, "thread-b", "fedcba9876543210", nil)
	if res := answer(t, s, second["id"].(string), nil); res.Code >= 300 {
		t.Fatalf("answer: %v", res)
	}
	s.SetClock(func() time.Time { return time.Now().Add(60 * 24 * time.Hour) })
	root := filepath.Dir(s.store.path)
	before := treeSum(t, root)
	items, err := s.Observations()
	if err != nil || len(items) != 2 {
		t.Fatalf("observations: %+v %v", items, err)
	}
	if items[0].Status != StatusOpen || items[1].Status != StatusAnswered {
		t.Fatalf("states: %+v", items)
	}
	if items[0].IdempotencyKey != "" || items[0].RequestFP != "" {
		t.Fatal("private fields retained")
	}
	if before != treeSum(t, root) {
		t.Fatal("observations wrote to the store")
	}
}

// Origin: the foundation line, 2026-11-21: delivery's release context reads only
// the times people answered, so this package gives exactly that and delivery
// can drop its import. A rule's answer is not a person's; an open question
// has no answer time.
func TestHumanAnswerTimesAreOnlyPeoplesAnswers(t *testing.T) {
	t.Parallel()
	s := testSvc(t)
	mustCreate(t, s, "thread-a", "0123456789abcdef", nil)
	byPerson := mustCreate(t, s, "thread-b", "fedcba9876543210", nil)
	byRule := mustCreate(t, s, "thread-c", "abcdef0123456789", nil)
	if res := answer(t, s, byPerson["id"].(string), nil); res.Code >= 300 {
		t.Fatalf("answer: %v", res)
	}
	ruleAt := time.Now().Add(time.Hour).UTC()
	if err := s.store.update(func(doc *document) error {
		for i := range doc.Questions {
			if doc.Questions[i].ID == byRule["id"].(string) {
				doc.Questions[i].Status = StatusAnswered
				doc.Questions[i].Answer = &Answer{Option: "a", By: "rule", At: ruleAt, Channel: RuleChannel}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	times, err := s.HumanAnswerTimes()
	if err != nil || len(times) != 1 {
		t.Fatalf("human answer times = %v %v", times, err)
	}
	if times[0].Equal(ruleAt) {
		t.Fatal("a rule's answer was counted as a person's")
	}
}
