package store_test

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
)

// TestFixtureRule checks the still-parsed fixture rows against their bodies.
func TestFixtureRule(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	if b := inForce(t, w, "budget:dev", time.Now()); b == nil || b.Budget.Mission.MaxAttempts != 1000 || b.Budget.AttemptLimitSec != 86400 {
		t.Fatalf("the fixture budget reads as %+v", b)
	}
	if j := inForce(t, w, "job:test", time.Now()); j == nil || len(j.Job.Candidates) != 1 {
		t.Fatalf("the fixture job rule reads as %+v", j)
	}
	if f := inForce(t, w, "capability-facts", time.Now()); f == nil || len(f.Facts) != 4 {
		t.Fatalf("the fixture facts rule reads as %+v", f)
	}
	_, err := w.rt.Exec(ctx, agentrttest.CardAutoRule)
	ok(t, err)
	if c := inForce(t, w, "card-auto-answer", time.Now()); c == nil || c.CardAuto.AfterSeconds != 30*60 || len(c.CardAuto.Kinds) != 1 {
		t.Fatalf("the fixture card-auto-answer rule reads as %+v", c)
	}
}

func envelopeRule(version int, direction, hosts string) string {
	return ruleText("standing-envelope:dev", version, direction, since, until, `tenant: dev
max_action: branch
budget: {max_attempts: 40, max_wall_hours: 48}
executors: [exec:codex]
hosts: [`+hosts+`]
`)
}
