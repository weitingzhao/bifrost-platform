package store_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

const concurrencyHead = "rule_id: concurrency\nrule_version: %d\ndirection: %s\neffective_at: 2026-01-01T00:00:00Z\nexpires_at: 2100-01-01T00:00:00Z\n"

// Without a concurrency rule in force nothing is dispatched (proposal §2:
// the strictest default is 0); a tightening to 0 stops dispatch at once.
func TestConcurrencyRule(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	ok(t, store.RegisterHost(ctx, w.rt, "owner", "h1", "dev"))
	w.jobs(t, "active", "*", codex)
	_, err := w.rt.Exec(ctx, agentrttest.DevBudgetRule)
	ok(t, err)
	first := w.ready(t, store.Task{})
	if c := w.claim(t, "h1", time.Hour, codex); c != nil {
		t.Fatalf("claimed %s without a concurrency rule", c.TaskID)
	}
	_, err = w.rt.Exec(ctx, agentrttest.ConcurrencyRule)
	ok(t, err)
	if c := w.claim(t, "h1", time.Hour, codex); c == nil || c.TaskID != first {
		t.Fatalf("with one attempt a host: %+v", c)
	}
	ok(t, store.RegisterHost(ctx, w.rt, "owner", "h2", "dev"))
	w.ready(t, store.Task{})
	_, err = store.TightenRule(ctx, w.rt, wr("thread:s1"), "concurrency",
		[]byte(fmt.Sprintf(concurrencyHead, 2, rules.Tighten)+"per_host: 0\n"))
	ok(t, err)
	if c := w.claim(t, "h2", time.Hour, codex); c != nil {
		t.Fatalf("claimed %s after concurrency was tightened to 0", c.TaskID)
	}
}

// TestFixtureRule: the concurrency row the tests insert reads as the rule it
// says (its text and body_json agree), so a fixture cannot drift from the
// parser.
func TestFixtureRule(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	r := inForce(t, w, "concurrency", time.Now())
	if r == nil || *r.PerHost != 1 {
		t.Fatalf("the fixture rule reads as %+v", r)
	}
	if b := inForce(t, w, "budget:dev", time.Now()); b == nil || b.Budget.Mission.MaxAttempts != 1000 || b.Budget.AttemptLimitSec != 86400 {
		t.Fatalf("the fixture budget reads as %+v", b)
	}
	if j := inForce(t, w, "job:test", time.Now()); j == nil || len(j.Job.Candidates) != 1 {
		t.Fatalf("the fixture job rule reads as %+v", j)
	}
	if f := inForce(t, w, "capability-facts", time.Now()); f == nil || len(f.Facts) != 4 {
		t.Fatalf("the fixture facts rule reads as %+v", f)
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

// A mission inside the standing envelope in force starts at once, its
// budget the budget rule's when it states none; one outside it waits and
// says why. Its tasks go only to the envelope's hosts and executors.
func TestEnvelopeStartsMission(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(s.sign(w, envelopeRule(1, rules.Initial, "h1, h2"))))
	ok(t, firstErr(s.sign(w, ruleText("budget:dev", 2, rules.Tighten, since, until,
		"tenant: dev\nmission: {max_attempts: 30, max_wall_hours: 24}\nattempt_hard_limit_s: 3600\n"))))
	propose := func(key string, p store.MissionProposal) map[string]any {
		t.Helper()
		p.Title, p.Goal, p.Success = "m", "g", []string{"done"}
		out, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), key, p, nil)
		ok(t, err)
		return out
	}
	inside := propose("a", store.MissionProposal{})
	env, _ := inside["envelope"].(map[string]any)
	if inside["state"] != "active" || env["rule_id"] != "standing-envelope:dev" || fmt.Sprint(env["rule_version"]) != "1" {
		t.Fatalf("inside the envelope: %v", inside)
	}
	var budget string
	ok(t, w.rt.QueryRow(ctx, `SELECT budget_json::text || ' ' || envelope_rule_id || ' v' || envelope_rule_version
	  FROM agentrt.mission WHERE mission_id = $1`, inside["mission_id"]).Scan(&budget))
	if budget != `{"max_attempts": 30, "max_wall_hours": 24} standing-envelope:dev v1` {
		t.Fatalf("the mission's budget and envelope: %s", budget)
	}
	for key, c := range map[string]struct {
		p   store.MissionProposal
		why string
	}{
		"b": {store.MissionProposal{MaxAttempts: 41, MaxWallHours: 10}, "its budget (41 attempts, 10 hours) is above the envelope's (40, 48)"},
		"c": {store.MissionProposal{MaxAttempts: 10, MaxWallHours: 49}, "its budget (10 attempts, 49 hours) is above the envelope's (40, 48)"},
		"d": {store.MissionProposal{Tenant: "business"}, store.EnvelopePending},
	} {
		if out := propose(key, c.p); out["state"] != "proposed" || out["waiting"] != c.why {
			t.Errorf("%s: %v; want proposed because %s", key, out, c.why)
		}
	}
	if out := propose("e", store.MissionProposal{MaxAction: "none", MaxAttempts: 5, MaxWallHours: 5}); out["state"] != "active" {
		t.Errorf("a smaller mission inside the envelope: %v", out)
	}
	// Its tasks: not on h3 (not the envelope's), not by claude (not its
	// executor); by codex on h1.
	mission := inside["mission_id"].(string)
	task := w.ready(t, store.Task{MissionID: mission, OffPlanReason: "envelope test"})
	if c := w.claim(t, "h3", time.Hour, codex); c != nil {
		t.Fatalf("h3, outside the envelope, claimed %s", c.TaskID)
	}
	if c := w.claim(t, "h1", time.Hour, claude); c != nil {
		t.Fatalf("claude, outside the envelope, claimed %s", c.TaskID)
	}
	// Tightened to h2 alone, h1 may no longer work it.
	_, err := store.TightenRule(ctx, w.rt, wr("thread:s1"), "standing-envelope:dev", []byte(envelopeRule(2, rules.Tighten, "h2")))
	ok(t, err)
	if c := w.claim(t, "h1", time.Hour, codex); c != nil {
		t.Fatalf("h1 claimed %s after the envelope dropped it", c.TaskID)
	}
	if c := w.claim(t, "h2", time.Hour, codex); c == nil || c.TaskID != task {
		t.Fatalf("h2 with codex: %+v; want %s", c, task)
	}
	// A mission started without the envelope is not held to it.
	plain := w.ready(t, store.Task{})
	if c := w.claim(t, "h3", time.Hour, claude); c == nil || c.TaskID != plain {
		t.Fatalf("a task of M-1 on h3 by claude: %+v; want %s", c, plain)
	}
	if _, err := store.TightenRule(ctx, w.rt, wr("thread:s1"), "standing-envelope:dev", []byte(envelopeRule(3, rules.Tighten, "h2, h3"))); !errors.Is(err, store.ErrUnsigned) {
		t.Fatalf("adding a host back unsigned: %v", err)
	}
}
