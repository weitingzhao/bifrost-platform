package landing

import (
	"testing"
	"time"
)

func record(kind string, kv ...any) Record {
	r := Record{"v": 1, "kind": kind, "op": "land:1"}
	for i := 0; i < len(kv); i += 2 {
		r[kv[i].(string)] = kv[i+1]
	}
	return r
}
func records() []Record {
	ref := "refs/heads/land/prod/" + task1 + "/1"
	return []Record{record("claimed", "task", task1, "attempt", attempt1, "A", A, "rule_version", 1), record("prepared", "R", R, "M0", M0, "committer_date", epoch.Format(time.RFC3339), "dry_run", false), record("branch_intent", "ref", ref, "R", R), record("branch_pushed", "ref", ref, "R", R), record("ci_kick", "try", 1), record("ci_rerun", "try", 2), record("ci_seen", "run", "ci-1", "status", "Succeeded", "R", R, "created_at", epoch.Add(2*time.Minute).Format(time.RFC3339)), record("main_intent", "R", R, "M0", M0), record("main_result", "outcome", "ok"), record("landed", "R", R), record("reverted", "R", R, "by", A)}
}
func TestLandingProtocolClosed(t *testing.T) {
	for _, r := range records() {
		if _, e := Parse(r); e != nil {
			t.Fatal(e)
		}
	}
	for _, mut := range []func(Record){func(r Record) { r["v"] = 2 }, func(r Record) { r["v"] = "1" }, func(r Record) { r["kind"] = "replaces" }, func(r Record) { r["extra"] = true }, func(r Record) { delete(r, "op") }, func(r Record) { r["A"] = "short" }, func(r Record) { r["task"] = "x" }} {
		r := records()[0]
		mut(r)
		if _, e := Parse(r); e == nil {
			t.Fatal("open protocol", r)
		}
	}
	if _, e := Parse("not object"); e == nil {
		t.Fatal("non-object accepted")
	}
}
