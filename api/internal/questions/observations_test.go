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
