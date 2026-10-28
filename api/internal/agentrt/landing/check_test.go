package landing

import (
	"reflect"
	"testing"
	"time"
)

// Check tries a record without changing the state it is given (review 656
// A-R4): accepted or refused, the maps and the operations are as before. The
// twin is folded from the same entries and never checked against.
func TestCheckLeavesTheStateAlone(t *testing.T) {
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	rec := func(m map[string]any) Record {
		t.Helper()
		m["v"] = Version
		r, err := Parse(m)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	const task, attempt, a = "t_01ARZ3NDEKTSV4RRFFQ69G5FAV", "a_01ARZ3NDEKTSV4RRFFQ69G5FAV", "2333e00753f91c74c8b2f4d693a803f8e89e7892"
	entries := []Entry{
		{At: at, Record: rec(map[string]any{"kind": "claimed", "op": "a:1", "task": task, "attempt": attempt, "A": a, "rule_version": 1})},
		{At: at, Record: rec(map[string]any{"kind": "failed", "op": "a:1", "why": "bound"})},
	}
	s, err := Fold(entries)
	if err != nil {
		t.Fatal(err)
	}
	twin, err := Fold(entries)
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		r  Record
		ok bool
	}{
		"retry":   {rec(map[string]any{"kind": "retry", "op": "a:1", "root": task, "after_op": "a:1", "reason": "again", "by": "owner"}), true},
		"decline": {rec(map[string]any{"kind": "failed", "op": "a:1", "why": "declined"}), true},
		"blocked": {rec(map[string]any{"kind": "claimed", "op": "a:2", "task": task, "attempt": attempt, "A": a, "rule_version": 1}), false},
		"illegal": {rec(map[string]any{"kind": "prepared", "op": "a:1", "R": a, "M0": a, "committer_date": at.Format(time.RFC3339Nano), "dry_run": true}), false},
	} {
		if err := s.Check(Entry{At: at.Add(time.Minute), Record: c.r}); (err == nil) != c.ok {
			t.Fatalf("%s: %v", name, err)
		}
		if !reflect.DeepEqual(s, twin) {
			t.Fatalf("%s: Check changed the state", name)
		}
	}
}
