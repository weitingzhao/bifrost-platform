//bifrost:owner foundation

package server

import "testing"

// 2026-11-19 (LANE-ci-w1): the caller's reason, rollback and unchecked are not
// params of an action, and the catalog dropped them before the record was written
// (all 823 PROD records said "(the caller gave no reason)"). Without a database: the words come from the body,
// only strings count, and the action's own params win a clash.
func TestCallerWordsAreReadFromTheBodyAndNeverOverrideParams(t *testing.T) {
	said := callerWords([]byte(`{"plan_id":"p1","reason":"r","rollback":"b","unchecked":"  ","extra":"x","reason2":1}`))
	if len(said) != 2 || said["reason"] != "r" || said["rollback"] != "b" {
		t.Fatalf("said = %v", said)
	}
	got := withCallerWords(map[string]any{"plan_id": "p1", "reason": "the action's"}, said)
	if got["reason"] != "the action's" || got["rollback"] != "b" || got["plan_id"] != "p1" {
		t.Fatalf("merged = %v", got)
	}
	if callerWords([]byte("not json")) != nil {
		t.Fatal("a body that is not JSON gives no words")
	}
}
