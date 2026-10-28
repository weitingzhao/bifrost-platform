package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// CUT §9 (review 646 §1, N652-2): a locked decision is taken at the clock
// read once its locks are held, not at the time its pass began. A claim reads
// Git for minutes between its two locked reads; what took effect or ran out
// meanwhile decides it.

// Serial: FreezeEventClock. A rule that takes effect while the claim reads
// Git refuses it at the second read. On 6ea51e0e the second read selected
// the rule as of the pass's start and claimed.
func TestClaimJudgesTheRuleInForceAtItsSecondRead(t *testing.T) {
	w, task, src := goalDrill(t, "M-3")
	t0 := time.Now().UTC().Truncate(time.Second)
	defer store.FreezeEventClock(t0)()
	result := src.Result
	tightened := false
	src.Result = func(c context.Context, repo, task, attempt, base, a string) (bool, []rules.LandingChange, error) {
		if !tightened {
			tightened = true
			body := strings.Replace(landingRuleBody, "deny: []", "deny: [\"console/src/components/**\"]", 1)
			effective := t0.Add(time.Minute).Format(time.RFC3339)
			_, err := store.TightenRule(ctx, w.rt, store.Writer{Actor: "owner", Token: "admin"}, "landing:dev", []byte(ruleText("landing:dev", 2, "tighten", effective, until, body)))
			ok(t, err)
			store.FreezeEventClock(t0.Add(2 * time.Minute))
		}
		return result(c, repo, task, attempt, base, a)
	}
	op, d, err := store.ClaimLandingDrill(ctx, w.rt, src, task, t0)
	ok(t, err)
	if !tightened {
		t.Fatal("fixture: the claim did not read Git")
	}
	if op != "" || d.Allowed || d.Reason != "rule" {
		t.Fatalf("claimed under the rule of the pass's start: op=%q %+v", op, d)
	}
	if n := len(landingState(t, w, landingRepoStream).Operations); n != 0 {
		t.Fatalf("a rule refusal recovers: nothing is recorded, got %d operations", n)
	}
}

// Serial: FreezeEventClock. A root whose claim window runs out while the
// claim reads Git is not claimed: the second read finds the window over and
// records failed{bound} (N652-2). On 6ea51e0e it wrote claimed after the
// window.
func TestClaimWindowEndingDuringTheReadIsBound(t *testing.T) {
	w, task, src := goalDrill(t, "M-3")
	var accepted time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT max(at) FROM agentrt.event WHERE task_id = $1
	  AND event_type = 'task_state_changed' AND body_json->>'to' = 'accepted'`, task).Scan(&accepted))
	start := accepted.Add(6*time.Hour - time.Second)
	defer store.FreezeEventClock(start)()
	result := src.Result
	read := false
	src.Result = func(c context.Context, repo, task, attempt, base, a string) (bool, []rules.LandingChange, error) {
		read = true
		store.FreezeEventClock(accepted.Add(6*time.Hour + time.Second))
		return result(c, repo, task, attempt, base, a)
	}
	op, d, err := store.ClaimLandingDrill(ctx, w.rt, src, task, start)
	ok(t, err)
	if !read {
		t.Fatal("fixture: the window ended before the read")
	}
	if op != "" || d.Reason != "bound" {
		t.Fatalf("claimed after its window: op=%q %+v", op, d)
	}
	s := landingState(t, w, landingRepoStream)
	o, exists := s.Operations[s.LatestOperation(task)]
	if !exists || o.Phase != "failed" || o.Outcome != "bound" {
		t.Fatalf("the window's end was not recorded: %+v", o)
	}
}

// Serial: FreezeEventClock. A decision is taken at the event clock, the
// clock its records are written with, whatever the pass's now says (CUT §9
// v2, review 656 B-R2).
func TestDecisionAtIsTheEventClock(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	restore := store.FreezeEventClock(now.Add(-time.Minute))
	defer restore()
	if got := store.DecisionAt(); !got.Equal(now.Add(-time.Minute)) {
		t.Fatal("not the clock", got)
	}
	store.FreezeEventClock(now.Add(time.Minute))
	if got := store.DecisionAt(); !got.Equal(now.Add(time.Minute)) {
		t.Fatal("not the clock", got)
	}
}
