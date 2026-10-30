package stgrelease

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The same terminal wrong-input archive and coverage are scheduled in both
// orders. All ledger writes pass the production parser and fold.
func TestReview760ExternalMustNotEraseWrongInput(t *testing.T) {
	t.Parallel()
	for _, first := range []bool{false, true} {
		t.Run(fmt.Sprintf("external_first=%v", first), func(t *testing.T) {
			t.Parallel()
			c, l, r, _ := controlFixture(t, "Succeeded")
			for _, v := range r.obs.Raw["child_refs"].([]any) {
				ref := v.(map[string]any)
				if ref["pipelineTaskName"] == "clone-ui" {
					ref["task_run"].(map[string]any)["status"].(map[string]any)["results"] = []any{map[string]any{"name": "commit", "value": sha("9")}}
				}
			}
			pass(t, c) // only durable run_evidence; no verify yet
			s := l.state(t)
			tr := s.Batches[leaseBatch].Tries[1]
			archived, err := restored(tr)
			if err != nil || archived.CheckInputs(expected(tr)) == nil {
				t.Fatal("fixture did not reach wrong-input evidence", err)
			}
			p := sha("2")
			l.effective(t, controlMember, p)
			l.add(t, store.StgRecord{"v": "1", "kind": "evidence", "scope": "ext", "evidence": "cluster/cicd/manual-new", "source": "external", "run": "manual-new", "P": p, "U": sha("b"), "infra": sha("c"), "content": map[string]any{"api_digest": dig("d"), "console_digest": dig("f")}, "deployed": map[string]any{"pin_sync_ok": true, "workloads": workloads(dig("d"), dig("f")), "at": l.clock.Now().Format(time.RFC3339Nano)}, "health": map[string]any{"status": "ok"}})
			l.add(t, store.StgRecord{"v": "1", "kind": "covers", "scope": "ext", "evidence": "cluster/cicd/manual-new", "root": controlMember.Root, "op": controlMember.Op, "R": controlMember.R})
			c.Runs = &externalRuns{controlRuns: r, listed: []*unstructured.Unstructured{}}
			if !first {
				pass(t, c) // same archive is now classified wrong_input
			}
			externalPass(t, c)
			for range 3 {
				pass(t, c)
			}
			s = l.state(t)
			tr = s.Batches[leaseBatch].Tries[1]
			if tr.Verify.Record.String("why") != "wrong_input" || len(s.Blocks) != 1 {
				t.Fatalf("terminal wrong input erased: phase=%s verify=%v blocks=%d member=%s", s.Batches[leaseBatch].Phase, tr.Verify.Record, len(s.Blocks), s.Members[controlMember].To)
			}
			for id, block := range s.Blocks {
				r := block.Opened.Record
				if !block.IsOpen() || r.String("why") != "wrong_input" || block.Opened.Seq <= tr.Verify.Seq || id != store.StgBlockID(r.String("P"), object(r, "base").String("P"), "wrong_input", r.String("evidence_version")) {
					t.Fatal("wrong_input did not retain its verified blocking interval", block)
				}
			}
			if s.Members[controlMember].To != "covered" || tr.Retry.Seq != 0 || r.creates != 0 {
				t.Fatal("coverage lost or wrong input retried")
			}
			for range 10 {
				l.clock.Step(time.Second)
				externalPass(t, c)
				pass(t, c)
			}
			if !reflect.DeepEqual(s, l.state(t)) {
				t.Fatal("repeat scheduling changed wrong_input identity or first_at")
			}
		})
	}
}

func TestReview760RuleRefusalWithoutBaseline(t *testing.T) {
	t.Parallel()
	for _, known := range []bool{false, true} {
		t.Run(fmt.Sprintf("known_base=%v", known), func(t *testing.T) {
			t.Parallel()
			c, l, _, _ := controlFixture(t, "Succeeded")
			l.entries = l.entries[:3] // baseline + opened + p_selected, before snapshot
			if !known {
				l.entries = l.entries[1:]
				for i := range l.entries {
					l.entries[i].Seq = int64(i + 1)
				}
			}
			_ = l.state(t)
			p := c.Policy()
			rule := *p.Rule
			rule.Expires = l.clock.Now().Add(-time.Second)
			p.Rule = &rule
			c.Policy = func() WindowPolicy { return p }
			if err := c.Pass(context.Background()); err != nil {
				t.Fatalf("production rule refusal violates storage protocol: %v", err)
			}
			s := l.state(t)
			if len(s.Blocks) != 1 || s.Batches[leaseBatch].Phase != "CLOSED" {
				t.Fatalf("rule refusal not durable: blocks=%d phase=%s", len(s.Blocks), s.Batches[leaseBatch].Phase)
			}
		})
	}
}

func TestReview760PartialExternalCoverageRetainsBudgetBlock(t *testing.T) {
	t.Parallel()
	for _, externalFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("external_first=%v", externalFirst), func(t *testing.T) {
			t.Parallel()
			c, l, r, _ := controlFixture(t, "Failed")
			l.entries = l.entries[:5] // no started/debits yet
			second := store.StgMemberKey{Root: "t_01ARZ3NDEKTSV4RRFFQ69G5FAW", Op: "land:two:1", R: sha("b")}
			opened := l.entries[1].Event.Body["stg_release"].(map[string]any)
			member := memberValue(second)
			member["gen"] = 1
			opened["members"] = append(opened["members"].([]any), member)
			opened["first_pending_at"].(map[string]any)[second.R] = l.clock.Now().Format(time.RFC3339Nano)
			snap := l.entries[4].Event.Body["stg_release"].(map[string]any)
			snap["set"] = append(snap["set"].([]any), memberValue(second))
			_ = l.state(t)
			r.workloads = workloads(dig("a"), dig("c"))
			reviewStartFailed(t, l)
			for i := 1; i < 3; i++ {
				pass(t, c) // verify
				pass(t, c) // effect
				pass(t, c) // retry
				l.clock.Step(30 * time.Minute)
				pass(t, c) // snapshot
				reviewStartFailed(t, l)
			}
			pass(t, c)
			pass(t, c)
			reviewExternal(t, l, r, true) // only first member is covered
			if externalFirst {
				c.Runs = &externalRuns{controlRuns: r}
				externalPass(t, c)
				pass(t, c)
			} else {
				pass(t, c)
			}
			s := l.state(t)
			if s.Budget[store.StgBudgetKey{R: second.R, Gen: 1}] != 3 || s.Members[second].To != "exhausted" || s.Members[controlMember].To != "covered" || s.Batches[leaseBatch].Phase != "CLOSED" {
				t.Fatal("fixture failed to reach mixed covered/exhausted settlement")
			}
			for _, b := range s.Blocks {
				if b.IsOpen() && b.Opened.Record.String("why") == "budget_exhausted" {
					return
				}
			}
			t.Fatal("exhausted member has no budget_exhausted interval: Owner retry/withhold has no valid trigger")
		})
	}
}
