package stgrelease

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// Exercise the real Parse -> Fold ledger and Controller.Pass without a DB.
func reviewExternal(t *testing.T, l *memoryLedger, r *controlRuns, withCover bool) {
	t.Helper()
	l.clock.Step(time.Second)
	r.workloads = workloads(dig("8"), dig("9"))
	l.add(t, store.StgRecord{"v": "1", "scope": "ext", "kind": "evidence", "evidence": "cluster/cicd/review-external", "source": "external", "run": "review-external", "P": sha("9"), "U": sha("b"), "infra": sha("c"), "content": map[string]any{"api_digest": dig("8"), "console_digest": dig("9")}, "deployed": map[string]any{"pin_sync_ok": true, "workloads": r.workloads, "at": l.clock.Now().Format(time.RFC3339Nano)}, "health": map[string]any{"status": "ok"}})
	l.effective(t, controlMember, sha("9"))
	if withCover {
		l.add(t, store.StgRecord{"v": "1", "scope": "ext", "kind": "covers", "evidence": "cluster/cicd/review-external", "root": controlMember.Root, "op": controlMember.Op, "R": controlMember.R})
	}
	l.clock.Step(time.Second)
}

func TestControlReviewFailedRunConsumesKnownExternal(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"Succeeded", "Failed"} {
		for _, withCover := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cover=%t", status, withCover), func(t *testing.T) {
				t.Parallel()
				c, l, r, _ := controlFixture(t, status)
				pass(t, c) // archive the original run first
				reviewExternal(t, l, r, withCover)
				for range 4 {
					pass(t, c)
				}
				s := l.state(t)
				if s.Members[controlMember].To != "covered" || s.Batches[leaseBatch].Phase != "CLOSED" || len(s.Blocks) != 0 {
					t.Fatalf("known current external deployment was not consumed: member=%s phase=%s blocks=%d", s.Members[controlMember].To, s.Batches[leaseBatch].Phase, len(s.Blocks))
				}
			})
		}
	}
}

func reviewStartFailed(t *testing.T, l *memoryLedger) {
	t.Helper()
	reviewStartFailedBatch(t, l, leaseBatch, 1)
}

func reviewStartFailedBatch(t *testing.T, l *memoryLedger, batch string, number int64) {
	t.Helper()
	reviewStartTerminalBatch(t, l, batch, number, "Failed")
}

func reviewStartTerminalBatch(t *testing.T, l *memoryLedger, batch string, number int64, status string) {
	t.Helper()
	s := l.state(t)
	b := s.Batches[batch]
	n := s.LastSeq + 1
	in := delivery.NamedDeliverInput{Batch: number, Try: b.Try, IntentSeq: n, P: sha("a"), U: sha("b"), Infra: sha("c")}
	id, err := in.Identity()
	if err != nil {
		t.Fatal(err)
	}
	convert := func(m map[string]string) map[string]any {
		o := map[string]any{}
		for k, v := range m {
			o[k] = v
		}
		return o
	}
	uid := fmt.Sprintf("review-batch-%d-try-%d", number, b.Try)
	// Failed tries need the same authenticated clone evidence as successful
	// ones before the controller can classify them as retryable.
	raw := controlObservationUID(t, in, status, uid).Raw
	spend := []any{}
	for _, value := range members(b) {
		m := store.StgRecord(value.(map[string]any))
		spend = append(spend, map[string]any{"R": m.String("R"), "gen": 1, "n": s.Budget[store.StgBudgetKey{R: m.String("R"), Gen: 1}] + 1})
	}
	l.add(t,
		batchRecord("started", batch, b.Try, "intent_seq", n, "run", id.Name, "labels", convert(id.Labels), "annotations", convert(id.Annotations), "fingerprint", id.Fingerprint, "spend", spend, "lease", map[string]any{"who": id.Who, "gen": 1}, "deadline", l.clock.Now().Add(2*time.Minute).Format(time.RFC3339Nano)),
		batchRecord("bound", batch, b.Try, "uid", uid),
		batchRecord("run_evidence", batch, b.Try, "uid", uid, "status", status, "raw", raw, "at", l.clock.Now().Format(time.RFC3339Nano)),
	)
}

func TestControlReviewThirdFailureAlreadyCoveredMustNotBlock(t *testing.T) {
	t.Parallel()
	for _, attempts := range []int{1, 3} {
		t.Run(fmt.Sprint(attempts), func(t *testing.T) {
			t.Parallel()
			c, l, r, _ := controlFixture(t, "Failed")
			r.workloads = workloads(dig("a"), dig("c"))
			pass(t, c)
			for i := 1; i < attempts; i++ {
				pass(t, c) // verify failed
				pass(t, c) // effect unchanged
				pass(t, c) // retry
				l.clock.Step(30 * time.Minute)
				pass(t, c) // next snapshot
				reviewStartFailed(t, l)
			}
			pass(t, c)                    // final verify failed
			pass(t, c)                    // final effect unchanged
			reviewExternal(t, l, r, true) // B5-f covers before conclusion
			pass(t, c)
			s := l.state(t)
			if s.Members[controlMember].To != "covered" || s.Batches[leaseBatch].Phase != "CLOSED" {
				t.Fatal("coverage did not settle the member")
			}
			for id, block := range s.Blocks {
				if block.IsOpen() {
					t.Fatalf("all members covered, but repository has unresolvable %s block %s", block.Opened.Record.String("why"), id)
				}
			}
		})
	}
}

func TestControlReviewPrepareKnownInvalidityDoesNotWaitForever(t *testing.T) {
	t.Parallel()
	c, l, _, _ := controlFixture(t, "Succeeded")
	l.entries = l.entries[:4] // OPEN, selected P, accepted CI
	l.effective(t, controlMember, sha("a"))
	s := l.state(t)
	wait, err := c.prepareMembers(context.Background(), s, s.Batches[leaseBatch], sha("a"))
	if err != nil || wait {
		t.Fatalf("positive control: wait=%v err=%v", wait, err)
	}
	// A non-landing revert still contains R in history, but R is ineffective.
	last := l.entries[len(l.entries)-1].Event.Body["landing_main"].(map[string]any)
	last["validity"], last["reverted_by"] = "reverted", sha("9")
	s = l.state(t)
	for range 100 {
		wait, err = c.prepareMembers(context.Background(), s, s.Batches[leaseBatch], sha("a"))
		if err != nil {
			t.Fatal(err)
		}
	}
	if wait {
		t.Fatal("definite manual reversion remains waiting; complete-range gate never receives the non-landing commit")
	}
}
