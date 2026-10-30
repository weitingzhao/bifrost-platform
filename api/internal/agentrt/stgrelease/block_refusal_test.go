package stgrelease

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestControlRuleRefusalBaselineMatrix(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"OPEN", "SNAP"} {
		for _, baseline := range []string{"known", "missing", "ambiguous"} {
			// A snapshot cannot be appended without trusted evidence. Its
			// reachable unknown case is a later contradictory observation.
			if phase == "SNAP" && baseline == "missing" {
				continue
			}
			for _, expired := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/expired=%t", phase, baseline, expired), func(t *testing.T) {
					t.Parallel()
					c, l, _, _ := controlFixture(t, "Succeeded")
					n := 3
					if phase == "SNAP" {
						n = 5
					}
					l.entries = l.entries[:n]
					if baseline == "missing" {
						l.entries = l.entries[1:]
						for i := range l.entries {
							l.entries[i].Seq = int64(i + 1)
						}
					} else if baseline == "ambiguous" {
						r := store.StgRecord{}
						for k, v := range l.entries[0].Event.Body["stg_release"].(map[string]any) {
							r[k] = v
						}
						r["evidence"], r["P"] = "cluster/cicd/conflicting", sha("f")
						l.add(t, r)
					}
					before := l.state(t)
					p := c.Policy()
					p.Live = false // observation/settlement work even with sending off
					rule := *p.Rule
					if expired {
						rule.Expires = l.clock.Now().Add(-time.Second)
					}
					p.Rule = &rule
					c.Policy = func() WindowPolicy { return p }
					pass(t, c)
					s := l.state(t)
					if !expired {
						if !reflect.DeepEqual(before, s) {
							t.Fatal("valid rule produced refusal")
						}
						return
					}
					want := "rule"
					if baseline != "known" {
						want = "baseline_unknown"
					}
					if len(s.Blocks) != 1 || s.Batches[leaseBatch].Phase != "CLOSED" {
						t.Fatal("refusal did not settle", s.Blocks)
					}
					for id, block := range s.Blocks {
						r := block.Opened.Record
						if !block.IsOpen() || r.String("why") != want || id != store.StgBlockID(r.String("P"), object(r, "base").String("P"), want, r.String("evidence_version")) {
							t.Fatal("wrong refusal identity", r)
						}
						if want == "baseline_unknown" && object(r, "detail").String("refusal") != "rule" {
							t.Fatal("original refusal lost", r)
						}
						if s.Members[controlMember].Disposition.Record.Int("ref") != block.Opened.Seq {
							t.Fatal("member references wrong interval")
						}
					}
					for range 10 {
						l.clock.Step(time.Second)
						pass(t, c)
					}
					if !reflect.DeepEqual(s, l.state(t)) {
						t.Fatal("repeated refusal changed identity or first_at")
					}
				})
			}
		}
	}
}

// Every controller blocker passes the same constructor and the real parser /
// Fold, both with a complete baseline and before any baseline is known.
func TestControlAllBlockReasonsMatchProtocol(t *testing.T) {
	t.Parallel()
	for _, why := range []string{"rule", "ci_red", "ci_results", "ci_missing", "baseline_unknown", "wrong_input", "baseline_moved", "budget_exhausted"} {
		for _, known := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/known=%t", why, known), func(t *testing.T) {
				t.Parallel()
				c, l, _, _ := controlFixture(t, "Succeeded")
				l.entries = l.entries[:5]
				s := l.state(t)
				base := object(s.Batches[leaseBatch].Tries[1].Snapshot.Record, "base")
				if !known {
					base = nil
				}
				if err := c.blockAndFinish(context.Background(), s, leaseBatch, s.Batches[leaseBatch], why, base, nil); err != nil {
					t.Fatal(err)
				}
				s = l.state(t)
				want := why
				if !known {
					want = "baseline_unknown"
				}
				if len(s.Blocks) != 1 {
					t.Fatal("no interval")
				}
				for _, b := range s.Blocks {
					if !b.IsOpen() || b.Opened.Record.String("why") != want {
						t.Fatal("wrong normalized reason", b)
					}
				}
			})
		}
	}
}
