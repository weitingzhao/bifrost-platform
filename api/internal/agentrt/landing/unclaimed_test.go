package landing

import (
	"fmt"
	"testing"
	"time"
)

// TD-447 4 (CUT §7.7): a root never claimed ends in one event, land:<root>:1
// born failed, with the claim's refusal (stale, result, in_main, reverted) or
// its window (bound). Nothing else may be born that way, and the new reasons
// belong to that birth only.
func TestNeverClaimedRootEndsInOneEvent(t *testing.T) {
	first := fmt.Sprintf("land:%s:1", task1)
	for _, tc := range []struct {
		name, phase, why, op string
		v                    any
		want                 bool
	}{
		{"stale", "empty", "stale", first, Version, true},
		{"result", "empty", "result", first, Version, true},
		{"in_main", "empty", "in_main", first, Version, true},
		{"reverted", "empty", "reverted", first, Version, true},
		{"bound", "empty", "bound", first, Version, true},
		{"old version", "empty", "stale", first, 1, false},
		{"other reason", "empty", "ci_red", first, Version, false},
		{"declined", "empty", "declined", first, Version, false},
		{"second number", "empty", "stale", fmt.Sprintf("land:%s:2", task1), Version, false},
		{"zero padded", "empty", "stale", fmt.Sprintf("land:%s:01", task1), Version, false},
		{"bad task", "empty", "stale", "land:t_bad:1", Version, false},
		{"not land", "empty", "stale", fmt.Sprintf("stg:%s:1", task1), Version, false},
		{"root has an operation", "claimed", "stale", first, Version, false},
		{"root has failed", "failed", "result", first, Version, false},
		{"root discarded", "discarded", "in_main", first, Version, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := mustFold(t, phaseRecords(tc.phase, true))
			before := len(s.Operations)
			r := v11("failed", "why", tc.why)
			r["v"], r["op"] = tc.v, tc.op
			at := epoch.Add(7 * time.Hour)
			err := s.Apply(Entry{At: at, Record: r})
			if (err == nil) != tc.want {
				t.Fatalf("err=%v want accepted=%v", err, tc.want)
			}
			if !tc.want {
				if len(s.Operations) != before {
					t.Fatal("a refused record left an operation behind")
				}
				return
			}
			o := s.Operations[tc.op]
			if o.Task != task1 || o.ClaimNumber != 1 || !o.ClaimedAt.Equal(at) || !o.LastAt.Equal(at) || o.Phase != "failed" || o.Outcome != tc.why ||
				o.Retried || o.Attempt != "" || o.A != "" || o.Ref != "" || o.Active() || !o.Terminal() || !o.BlocksClaim() {
				t.Fatalf("born operation: %+v", o)
			}
			if s.LatestOperation(task1) != tc.op || s.RootPending(task1) || s.AwaitsNext(task1) {
				t.Fatal("the record does not end the root")
			}
			if f, n := RootBudget(s, task1); n != 1 || !f.Equal(at) {
				t.Fatalf("budget first=%v n=%d", f, n)
			}
			if s.Apply(Entry{At: at, Record: r}) == nil {
				t.Fatal("a second birth was accepted")
			}
		})
	}
}

// The new reasons never end an operation that ran; parked keeps its reasons.
func TestClaimRefusalReasonsOnlyAtBirth(t *testing.T) {
	for _, why := range ClaimRefusals {
		s := mustFold(t, phaseRecords("branch_pushed", true))
		if s.Apply(Entry{At: epoch.Add(time.Hour), Record: v11("failed", "why", why)}) == nil {
			t.Fatalf("failed{%s} accepted on a running operation", why)
		}
		p := record("parked", "why", why, "retry_at", epoch.Add(time.Hour).Format(time.RFC3339))
		if _, err := Parse(p); err == nil {
			t.Fatalf("parked{%s} parsed", why)
		}
		if _, err := Parse(v11("stg_failed", "why", why)); err == nil {
			t.Fatalf("stg_failed{%s} parsed", why)
		}
	}
}

// A born stale or bound takes the Owner's retry, and k=2 is a real claim that
// uses it; result, in_main and reverted take no retry.
func TestNeverClaimedRetryThenClaim(t *testing.T) {
	first := fmt.Sprintf("land:%s:1", task1)
	for _, why := range []string{"stale", "bound", "result", "in_main", "reverted"} {
		t.Run(why, func(t *testing.T) {
			s := State{}
			born := v11("failed", "why", why)
			born["op"] = first
			if err := s.Apply(Entry{At: epoch, Record: born}); err != nil {
				t.Fatal(err)
			}
			retry := v11("retry", "root", task1, "after_op", first, "reason", "again", "by", "owner")
			retry["op"] = first
			err := s.Apply(Entry{At: epoch.Add(time.Hour), Record: retry})
			if retryable := why == "stale" || why == "bound"; (err == nil) != retryable {
				t.Fatalf("retry err=%v, retryable=%v", err, retryable)
			}
			if err != nil {
				return
			}
			if !s.RootPending(task1) || !s.AwaitsNext(task1) {
				t.Fatal("a retried birth is not pending")
			}
			second := fmt.Sprintf("land:%s:2", task1)
			c := v11("claimed", "task", task1, "attempt", attempt1, "A", A, "rule_version", 1)
			c["op"] = second
			if err := s.Apply(Entry{At: epoch.Add(2 * time.Hour), Record: c}); err != nil {
				t.Fatal(err)
			}
			o := s.Operations[second]
			if o.ClaimNumber != 2 || o.Attempt != attempt1 || s.LatestOperation(task1) != second || s.Retries[task1].ConsumedBy != second {
				t.Fatalf("k=2: %+v retries=%+v", o, s.Retries)
			}
			if f, n := RootBudget(s, task1); n != 2 || !f.Equal(epoch.Add(time.Hour)) {
				t.Fatalf("budget first=%v n=%d (retry restarts the window, the count stays)", f, n)
			}
		})
	}
}
