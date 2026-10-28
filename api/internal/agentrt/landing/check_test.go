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

// main_lost (CUT §10.6 1A, review 658 B3, 659 R659-3): a failed v1.1 record
// from main_result{ok} only. Checked against the state after every record of
// two chains (to main, and to parked), and after unknown and denied: refused
// everywhere but ok; refused as another kind and as v1.
func TestMainLostOnlyFromOk(t *testing.T) {
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	const task, attempt, a, b = "t_01ARZ3NDEKTSV4RRFFQ69G5FAV", "a_01ARZ3NDEKTSV4RRFFQ69G5FAV", "2333e00753f91c74c8b2f4d693a803f8e89e7892", "b7c3f7e2a4d1f0e9c8b7a6958473625140312a0b"
	ref := "refs/heads/land/prod/" + task + "/1"
	head := []map[string]any{
		{"kind": "claimed", "op": "m:1", "task": task, "attempt": attempt, "A": a, "rule_version": 1},
		{"kind": "prepared", "op": "m:1", "R": b, "M0": a, "committer_date": at.Format(time.RFC3339Nano), "dry_run": false},
		{"kind": "branch_intent", "op": "m:1", "ref": ref, "R": b},
		{"kind": "branch_pushed", "op": "m:1", "ref": ref, "R": b},
	}
	toMain := append(append([]map[string]any{}, head...),
		map[string]any{"kind": "ci_seen", "op": "m:1", "run": "green", "status": "Succeeded", "R": b, "created_at": at.Add(5 * time.Second).Format(time.RFC3339Nano)},
		map[string]any{"kind": "main_intent", "op": "m:1", "R": b, "M0": a})
	parse := func(m map[string]any) Record {
		t.Helper()
		m["v"] = Version
		r, err := Parse(m)
		if err != nil {
			t.Fatal(m["kind"], err)
		}
		return r
	}
	lost := parse(map[string]any{"kind": "failed", "op": "m:1", "why": "main_lost"})
	// fold returns the state after each record of a chain.
	fold := func(chain []map[string]any) []State {
		t.Helper()
		var out []State
		for i, m := range chain {
			s, err := Fold(nil)
			if err != nil {
				t.Fatal(err)
			}
			for j, n := range chain[:i+1] {
				if err := s.Apply(Entry{At: at.Add(time.Duration(j+2) * time.Second), Record: parse(n)}); err != nil {
					t.Fatal(n["kind"], err)
				}
			}
			_ = m
			out = append(out, s)
		}
		return out
	}
	check := func(name string, s State, want bool) {
		t.Helper()
		if err := s.Check(Entry{At: at.Add(time.Minute), Record: lost}); (err == nil) != want {
			t.Fatalf("main_lost after %s: accepted=%v", name, err == nil)
		}
	}
	for i, s := range fold(toMain) {
		check(toMain[i]["kind"].(string), s, false)
	}
	parked := fold(append(append([]map[string]any{}, head...), map[string]any{"kind": "parked", "op": "m:1", "why": "window", "retry_at": at.Add(time.Minute).Format(time.RFC3339Nano)}))
	check("parked", parked[len(parked)-1], false)
	for _, outcome := range []string{"unknown", "denied", "ok"} {
		chain := append(append([]map[string]any{}, toMain...), map[string]any{"kind": "main_result", "op": "m:1", "outcome": outcome})
		states := fold(chain)
		check("main_result{"+outcome+"}", states[len(states)-1], outcome == "ok")
	}
	if _, err := Parse(map[string]any{"v": 1, "kind": "failed", "op": "m:1", "why": "main_lost"}); err == nil {
		t.Fatal("main_lost parsed as v1")
	}
	for _, kind := range []string{"parked", "stg_failed"} {
		m := map[string]any{"v": Version, "kind": kind, "op": "m:1", "why": "main_lost"}
		if kind == "parked" {
			m["retry_at"] = at.Format(time.RFC3339Nano)
		}
		if _, err := Parse(m); err == nil {
			t.Fatalf("%s{main_lost} parsed", kind)
		}
	}
	ok := fold(append(append([]map[string]any{}, toMain...), map[string]any{"kind": "main_result", "op": "m:1", "outcome": "ok"}))
	s := ok[len(ok)-1]
	if err := s.Apply(Entry{At: at.Add(time.Minute), Record: lost}); err != nil {
		t.Fatal(err)
	}
	if o := s.Operations["m:1"]; o.Phase != "failed" || o.Outcome != "main_lost" || s.RootPending(task) || !o.BlocksClaim() {
		t.Fatalf("%+v", o)
	}
}
