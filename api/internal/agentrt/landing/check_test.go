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

// main_lost (CUT §10.6 1A, review 658 B3): a failed v1.1 record from
// main_result{ok} only; refused from every phase a general failure may end,
// as another kind, and as v1.
func TestMainLostOnlyFromOk(t *testing.T) {
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	const task, attempt, a, b = "t_01ARZ3NDEKTSV4RRFFQ69G5FAV", "a_01ARZ3NDEKTSV4RRFFQ69G5FAV", "2333e00753f91c74c8b2f4d693a803f8e89e7892", "b7c3f7e2a4d1f0e9c8b7a6958473625140312a0b"
	chain := []map[string]any{
		{"kind": "claimed", "op": "m:1", "task": task, "attempt": attempt, "A": a, "rule_version": 1},
		{"kind": "prepared", "op": "m:1", "R": b, "M0": a, "committer_date": at.Format(time.RFC3339Nano), "dry_run": false},
		{"kind": "branch_intent", "op": "m:1", "ref": "refs/heads/land/prod/" + task + "/1", "R": b},
		{"kind": "branch_pushed", "op": "m:1", "ref": "refs/heads/land/prod/" + task + "/1", "R": b},
		{"kind": "ci_seen", "op": "m:1", "run": "green", "status": "Succeeded", "R": b, "created_at": at.Add(5 * time.Second).Format(time.RFC3339Nano)},
		{"kind": "main_intent", "op": "m:1", "R": b, "M0": a},
		{"kind": "main_result", "op": "m:1", "outcome": "ok"},
	}
	lost := func(v any) map[string]any { return map[string]any{"v": v, "kind": "failed", "op": "m:1", "why": "main_lost"} }
	var s State
	for i, m := range chain {
		m["v"] = Version
		r, err := Parse(m)
		if err != nil {
			t.Fatal(m["kind"], err)
		}
		if m["kind"] != "ci_seen" && m["kind"] != "main_intent" {
			// A general failure may end these; main_lost may not.
			if r, err := Parse(lost(Version)); err == nil && m["kind"] != "main_result" {
				if err := s.Check(Entry{At: at.Add(time.Duration(i) * time.Second), Record: r}); err == nil && i > 0 {
					t.Fatalf("main_lost accepted after %s", chain[i-1]["kind"])
				}
			}
		}
		if err := s.Apply(Entry{At: at.Add(time.Duration(i+2) * time.Second), Record: r}); err != nil {
			t.Fatal(m["kind"], err)
		}
	}
	if _, err := Parse(lost(1)); err == nil {
		t.Fatal("main_lost parsed as v1")
	}
	if _, err := Parse(map[string]any{"v": Version, "kind": "parked", "op": "m:1", "why": "main_lost", "retry_at": at.Format(time.RFC3339Nano)}); err == nil {
		t.Fatal("parked{main_lost} parsed")
	}
	r, err := Parse(lost(Version))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(Entry{At: at.Add(time.Minute), Record: r}); err != nil {
		t.Fatal("main_lost refused from main_result{ok}", err)
	}
	if o := s.Operations["m:1"]; o.Phase != "failed" || o.Outcome != "main_lost" || s.RootPending(task) || !o.BlocksClaim() {
		t.Fatalf("%+v", o)
	}
}
