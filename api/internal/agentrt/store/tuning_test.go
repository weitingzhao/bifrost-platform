package store_test

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// TestTuningRuleInForce: without a tuning rule the defaults hold; once one
// is signed a claim leases for its lease_ttl_s and a heartbeat renews for it;
// a version that only lowers a value still needs the Owner's signature.
func TestTuningRuleInForce(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	tune, err := store.ReadTuning(ctx, w.rt)
	ok(t, err)
	if tune != nil || tune.Duration(rules.TuneLeaseTTL) != store.LeaseTTL {
		t.Fatalf("a tuning rule before any was signed: %+v", tune)
	}
	s := signer{t, rptest.NewKey(t)}
	_, err = s.sign(w, ruleText("tuning", 1, rules.Initial, since, until, "values: {lease_ttl_s: 600, threads_per_day: 50}\n"))
	ok(t, err)
	tune, err = store.ReadTuning(ctx, w.rt)
	ok(t, err)
	if tune.Duration(rules.TuneLeaseTTL) != 10*time.Minute || tune.Int(rules.TuneThreadsPerDay) != 50 || tune.Int(rules.TuneAssessPerPass) != store.AssessPerPass {
		t.Fatalf("read %+v", tune)
	}

	id := w.ready(t, store.Task{})
	c, err := store.ClaimNext(ctx, w.rt, store.ClaimRequest{HostID: "h1", FreeSlots: 1, Tools: []store.Tool{codex, claude, cursor}})
	ok(t, err)
	if c == nil || c.TaskID != id {
		t.Fatalf("claim = %+v; want %s", c, id)
	}
	var now time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now))
	if left := c.LeaseExpiresAt.Sub(now); left < 9*time.Minute || left > 10*time.Minute {
		t.Fatalf("the lease has %v left; the rule's lease_ttl_s is 600", left)
	}
	b, err := store.Heartbeat(ctx, w.rt, fence(c, "h1"))
	ok(t, err)
	ok(t, w.rt.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now))
	if left := b.LeaseExpiresAt.Sub(now); left < 9*time.Minute || left > 10*time.Minute {
		t.Fatalf("the heartbeat renewed for %v; the rule's lease_ttl_s is 600", left)
	}

	lower := ruleText("tuning", 2, rules.Tighten, since, until, "values: {lease_ttl_s: 300, threads_per_day: 50}\n")
	if _, err := store.TightenRule(ctx, w.rt, wr("thread:s1"), "tuning", []byte(lower)); err == nil {
		t.Fatal("an unsigned tuning rule took effect")
	}
	tune, err = store.ReadTuning(ctx, w.rt)
	ok(t, err)
	if tune.Duration(rules.TuneLeaseTTL) != 10*time.Minute {
		t.Fatalf("after the refused version: %+v", tune)
	}
}
