package stgrelease

import (
	"context"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func reviewFailedAttempts(t *testing.T, attempts int) (*Controller, *memoryLedger, *controlRuns) {
	t.Helper()
	c, l, r, _ := controlFixture(t, "Failed")
	r.workloads = workloads(dig("a"), dig("c"))
	pass(t, c)
	for i := 1; i <= attempts; i++ {
		pass(t, c)
		pass(t, c)
		if i < attempts {
			pass(t, c)
			l.clock.Step(30 * time.Minute)
			pass(t, c)
			reviewStartFailed(t, l)
		}
	}
	return c, l, r
}

func TestControlBudgetConsumesExistingOutcomes(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"evidence_only", "disposed", "withheld"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			c, l, r := reviewFailedAttempts(t, 3)
			want := "covered"
			switch mode {
			case "evidence_only":
				reviewExternal(t, l, r, false)
			case "disposed":
				reviewLanded(t, l, controlMember)
				reviewLanded(t, l, reviewOther)
				reviewLandingRecord(t, l, controlMember, "reverted", "R", controlMember.R, "by", reviewOther.R)
				want = "disposed"
			case "withheld":
				s := l.state(t)
				base := object(s.Batches[leaseBatch].Tries[3].Snapshot.Record, "base")
				block := store.StgBlockID(sha("a"), base.String("P"), "hard_floor", "floor")
				l.add(t, store.StgRecord{"v": "1", "scope": "block", "kind": "blocked", "block": block, "why": "hard_floor", "P": sha("a"), "base": map[string]any(base), "first_at": l.clock.Now().Format(time.RFC3339Nano), "evidence_version": "floor", "detail": map[string]any{"members": members(s.Batches[leaseBatch])}})
				l.add(t, store.StgRecord{"v": "1", "scope": "grant", "kind": "withheld", "grant": "floor", "members": members(s.Batches[leaseBatch]), "why": "hard_floor", "trigger_seq": l.state(t).LastSeq})
				l.clock.Step(time.Second)
				// A new deployment resolves the old hard-floor interval, while
				// the member's permanent withholding remains in force.
				e := l.state(t).Evidence["cluster/cicd/base-uid"].Record
				e["evidence"], e["run"], e["P"] = "cluster/cicd/floor-fixed", "floor-fixed", sha("a")
				e["deployed"].(map[string]any)["at"] = l.clock.Now().Format(time.RFC3339Nano)
				l.add(t, e, unblockRecord(block, "external_release", e["evidence"]))
				want = "released" // withholding is its independent durable fact.
			}
			for range 3 {
				pass(t, c)
			}
			s := l.state(t)
			if s.Members[controlMember].To != want || s.Batches[leaseBatch].Phase != "CLOSED" || s.Budget[store.StgBudgetKey{R: controlMember.R, Gen: 1}] != 3 {
				t.Fatal("existing outcome lost or budget refunded", mode, s.Members)
			}
			for _, block := range s.Blocks {
				if block.IsOpen() || block.Opened.Record.String("why") == "budget_exhausted" {
					t.Fatal("already disposed member created an exhaustion interval", mode)
				}
			}
			if mode == "withheld" && s.Withheld[controlMember].Seq == 0 {
				t.Fatal("withholding was lost")
			}
		})
	}
}

func TestControlCoveredLastDebitHandsOffMemberWithBudget(t *testing.T) {
	t.Parallel()
	c, l, r := reviewFailedAttempts(t, 2)
	s := l.state(t)
	b := s.Batches[leaseBatch]
	prior := b.Tries[b.Try]
	l.add(t, finish(s, leaseBatch, b, prior.Verify.Seq, false)...)
	l.clock.Step(30 * time.Minute)
	batch := "stg:bifrost-platform:2"
	ms := []any{}
	for _, m := range []store.StgMemberKey{controlMember, reviewOther} {
		v := memberValue(m)
		v["gen"] = 1
		ms = append(ms, v)
	}
	l.add(t, batchRecord("opened", batch, 0, "members", ms, "first_pending_at", map[string]any{controlMember.R: s.Members[controlMember].FirstPendingAt.Format(time.RFC3339Nano), reviewOther.R: l.clock.Now().Format(time.RFC3339Nano)}),
		batchRecord("p_selected", batch, 0, "P", sha("a"), "at", l.clock.Now().Format(time.RFC3339Nano)),
		batchRecord("ci_accepted", batch, 0, "P", sha("a"), "ci", b.CI["ci-uid"].Record["ci"]),
		batchRecord("snapshot", batch, 1, "inputs", prior.Snapshot.Record["inputs"], "base", prior.Snapshot.Record["base"], "set", []any{memberValue(controlMember), memberValue(reviewOther)}, "not_before", l.clock.Now().Format(time.RFC3339Nano)))
	reviewStartFailedBatch(t, l, batch, 2)
	pass(t, c)
	pass(t, c)
	reviewExternal(t, l, r, true)
	pass(t, c)
	s = l.state(t)
	if len(s.Blocks) != 0 || s.Members[controlMember].To != "covered" || s.Members[reviewOther].To != "released" || s.Batches[batch].Phase != "CLOSED" {
		t.Fatal("covered last debit blocked another member with budget", s.Members)
	}
	if s.Budget[store.StgBudgetKey{R: controlMember.R, Gen: 1}] != 3 || s.Budget[store.StgBudgetKey{R: reviewOther.R, Gen: 1}] != 1 {
		t.Fatal("fixture did not debit mixed remaining budgets")
	}
	m := memberValue(reviewOther)
	m["gen"] = 1
	if _, err := l.Commit(context.Background(), s.LastSeq, []store.StgRecord{batchRecord("opened", "stg:bifrost-platform:3", 0, "members", []any{m}, "first_pending_at", map[string]any{reviewOther.R: s.Members[reviewOther].FirstPendingAt.Format(time.RFC3339Nano)})}); err != nil {
		t.Fatal("remaining member cannot reenter", err)
	}
}
