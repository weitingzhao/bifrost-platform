package store

import (
	"reflect"
	"testing"
)

func TestStgGateRefusalRecordsMatchProtocol(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, why string
		change    func(*stgPolicyFixture)
	}{
		{"missing baseline", "baseline_unknown", func(x *stgPolicyFixture) { x.s.Evidence = map[string]StgEvent{} }},
		{"unreadable baseline", "baseline_unknown", func(x *stgPolicyFixture) { x.o.BaseP = "" }},
		{"ambiguous baseline", "baseline_unknown", func(x *stgPolicyFixture) {
			base := x.s.Evidence["cluster/cicd/base-uid"]
			r := stgClone(base.Record)
			r["evidence"], r["P"] = "cluster/cicd/conflicting", stgSHA("f")
			x.s.Evidence["cluster/cicd/conflicting"] = StgEvent{Seq: base.Seq + 1, At: base.At, Record: r}
		}},
		{"hard floor", "hard_floor", func(x *stgPolicyFixture) { x.o.Commits[0].Changes[0].NewPath = ".github/workflows/ci.yaml" }},
		{"merge", "non_linear", func(x *stgPolicyFixture) { x.o.Commits[0].Parents = append(x.o.Commits[0].Parents, stgSHA("f")) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			x := stgPolicy(t)
			if d := x.decide(false); !d.Allowed {
				t.Fatal("positive control", d)
			}
			tc.change(x)
			d := x.decide(false)
			if d.Allowed || d.Why != tc.why {
				t.Fatal("wrong refusal", d)
			}
			snap := x.s.Batches[stgTestBatch].Tries[1].Snapshot.Record
			before := stgClone(snap)
			r, err := stgRefusalRecord(x.s, stgTestBatch, x.o, x.f, x.at, d)
			if err != nil {
				t.Fatal("production refusal construction failed", err)
			}
			if _, err := ParseStgRelease(r); err != nil {
				t.Fatal("production refusal violates the storage protocol", err)
			}
			if !reflect.DeepEqual(map[string]any(snap), before) {
				t.Fatal("refusal changed the frozen snapshot")
			}
			if tc.why == "baseline_unknown" {
				if !stgKeys(r.object("base"), "P") || r.object("base").String("P") != "" {
					t.Fatal("unknown baseline retained trusted content", r)
				}
			} else if !stgSame(r["base"], snap["base"]) {
				t.Fatal("known baseline lost evidence or digests", r)
			}
			block := x.h.add(r)
			if got := x.h.state().Blocks[r.String("block")]; !got.IsOpen() || got.Opened.Seq != block.Seq {
				t.Fatal("production block did not replay", got)
			}
			if tc.why == "hard_floor" {
				x.h.add(stgRecord("withheld", "grant", "hard-floor:"+r.String("block"), "members", stgMemberValues(d.HardFloor), "why", "hard_floor", "trigger_seq", block.Seq))
				if x.h.state().Withheld[d.HardFloor[0]].Record.String("why") != "hard_floor" {
					t.Fatal("hard-floor refusal did not authorize withholding")
				}
			}
		})
	}
}
