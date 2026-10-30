package stgrelease

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// All attempts spend through started and obey the real Fold's backoff.
// Only the final terminal status differs between failed-other and superseded.
func replacementBudgetFixture(t *testing.T, attempts int, status string, all bool) (*Controller, *memoryLedger, *controlRuns) {
	t.Helper()
	first := "Failed"
	if attempts == 1 {
		first = status
	}
	c, l, r, _ := controlFixture(t, first)
	reviewSecondMember(t, l)
	r.workloads = workloads(dig("a"), dig("c"))
	pass(t, c)
	for i := 1; i < attempts; i++ {
		pass(t, c) // failed verify
		pass(t, c) // unchanged effect
		pass(t, c) // retry
		l.clock.Step(30 * time.Minute)
		pass(t, c) // snapshot
		next := "Failed"
		if i == attempts-1 {
			next = status
		}
		reviewStartTerminalBatch(t, l, leaseBatch, 1, next)
	}
	if status == "Failed" {
		pass(t, c) // immutable run_failed before external evidence arrives
	}
	reviewExternal(t, l, r, false)
	if all {
		l.effective(t, reviewOther, sha("9"))
	}
	pass(t, c) // failed: effect other; successful: verify superseded
	want := "failed"
	if status == "Succeeded" {
		want = "superseded"
	}
	s := l.state(t)
	if got := s.Batches[leaseBatch].Tries[int64(attempts)].Verify.Record.String("outcome"); got != want {
		t.Fatalf("fixture verification=%s, want %s", got, want)
	}
	return c, l, r
}

// Exercise the existing recorded-answer contract through Parse/Fold, not a
// live B5-g card handler or an Owner RPC. Only the uncovered member is eligible.
func replacementBudgetDecision(t *testing.T, c *Controller, l *memoryLedger, choice string) {
	t.Helper()
	s := l.state(t)
	var trigger store.StgEvent
	for _, block := range s.Blocks {
		if block.IsOpen() && block.Opened.Record.String("why") == "budget_exhausted" {
			trigger = block.Opened
		}
	}
	if trigger.Seq == 0 || s.Members[reviewOther].Disposition.Record.Int("ref") != trigger.Seq {
		t.Fatal("exhausted member does not reference the actual budget interval")
	}
	if s.Members[controlMember].To != "covered" || s.Members[reviewOther].To != "exhausted" {
		t.Fatal("budget answer has no precise remaining member")
	}
	ms := []any{memberValue(reviewOther)}
	q := "replacement-budget"
	decision := store.StgRecord{"v": "1", "scope": "grant", "kind": "granted", "grant": q, "members": ms, "gen": 2, "trigger_seq": trigger.Seq, "question_id": q}
	if choice == "do_not_release" {
		decision["kind"], decision["why"] = "withheld", "owner"
		delete(decision, "gen")
	}
	if ok, err := l.Commit(context.Background(), s.LastSeq, []store.StgRecord{decision}); err == nil || ok {
		t.Fatal("budget decision accepted without an applied answer")
	}
	answer := map[string]any{"family": "stg", "kind": "stg_failed", "question_id": q, "trigger_seq": trigger.Seq, "block": trigger.Record.String("block"), "outcome": "applied", "choice": choice, "channel": "console", "by": "owner", "members": ms}
	l.entries = append(l.entries, store.StgEntry{Seq: s.LastSeq + 1, At: l.clock.Now(), Event: store.Event{Stream: store.StgReleaseStream, Type: "note", Actor: "controller", Body: map[string]any{"text": "applied STG card fixture", "landing_card_outcome": answer}}})
	_ = l.state(t)
	l.add(t, decision)
	pass(t, c) // Consume the matching grant/withholding and close its interval.
	s = l.state(t)
	if s.Blocks[trigger.Record.String("block")].IsOpen() || s.Members[controlMember].To != "covered" || s.Members[controlMember].Gen != 1 {
		t.Fatal("decision failed to resolve its interval or revived a covered member")
	}
	for _, m := range []store.StgMemberKey{controlMember, reviewOther} {
		if s.Budget[store.StgBudgetKey{R: m.R, Gen: 1}] != 3 {
			t.Fatal("decision refunded the old budget")
		}
	}
	if ok, err := l.Commit(context.Background(), s.LastSeq, []store.StgRecord{decision}); err == nil || ok {
		t.Fatal("replayed decision added another generation")
	}
	seq := s.LastSeq
	for range 5 {
		pass(t, c)
	}
	if l.state(t).LastSeq != seq {
		t.Fatal("resolved interval replay appended records")
	}
	m := memberValue(reviewOther)
	m["gen"] = 2
	if choice == "do_not_release" {
		m["gen"] = 1
		if s.Withheld[reviewOther].Seq == 0 {
			t.Fatal("Owner withholding was not retained")
		}
	} else if s.Members[reviewOther].Gen != 2 || s.Budget[store.StgBudgetKey{R: reviewOther.R, Gen: 2}] != 0 {
		t.Fatal("grant did not give the remaining member a fresh generation")
	}
	ok, err := l.Commit(context.Background(), seq, []store.StgRecord{batchRecord("opened", "stg:bifrost-platform:2", 0, "members", []any{m}, "first_pending_at", map[string]any{reviewOther.R: s.Members[reviewOther].FirstPendingAt.Format(time.RFC3339Nano)})})
	if choice == "retry_release" && (err != nil || !ok) {
		t.Fatal("authorized remaining member cannot reenter", err)
	}
	if choice == "do_not_release" && (err == nil || ok) {
		t.Fatal("withheld member reentered")
	}
}

func TestControlReplacementBudgetCAS(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"Failed", "Succeeded"} {
		for _, lateCover := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/late_cover=%t", status, lateCover), func(t *testing.T) {
				t.Parallel()
				c, l, _ := replacementBudgetFixture(t, 3, status, false)
				before := l.state(t)
				c.Ledger = &reviewLateCoverLedger{memoryLedger: l, beforeCommit: func() {
					p := sha("7") // Advances the stream without covering external P.
					if lateCover {
						p = sha("9")
					}
					l.effective(t, reviewOther, p)
				}}
				pass(t, c)
				s := l.state(t)
				if s.LastSeq != before.LastSeq+1 || s.Batches[leaseBatch].Phase == "CLOSED" || len(s.Blocks) != 0 || len(s.Covers) != 0 || len(s.DeploymentChecks) != len(before.DeploymentChecks) {
					t.Fatal("stale CAS partially committed the check, covers, block or settlement")
				}
				pass(t, c)
				s = l.state(t)
				want, blocks := "exhausted", 1
				if lateCover {
					want, blocks = "covered", 0
				}
				if s.Members[reviewOther].To != want || len(s.Blocks) != blocks || s.Batches[leaseBatch].Phase != "CLOSED" || len(s.DeploymentChecks) != 1 {
					t.Fatal("CAS retry did not use current remaining members", s.Members, s.Blocks)
				}
				seq := s.LastSeq
				for range 100 {
					pass(t, c)
				}
				if l.state(t).LastSeq != seq {
					t.Fatal("CAS replay duplicated the settlement")
				}
				if !lateCover {
					replacementBudgetDecision(t, c, l, "do_not_release")
				}
			})
		}
	}
}
