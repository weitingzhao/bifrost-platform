package store_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// Regression tests for review 186.

// A1: a rule write waits for a claim in flight, which read the rules and
// candidates before it, and the claim leases on what it read.
func TestRuleWriteWaitsForAClaim(t *testing.T) {
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(s.sign(w, jobText(1, rules.Initial, candidate("exec:codex", "openai", "active")))))
	task := w.ready(t, store.Task{})
	pool := w.db.Pool(t, w.db.Runtime)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unhook := store.SetHook(func(point, _ string) {
		if point == "candidate" {
			once.Do(func() { close(entered); <-release })
		}
	})
	defer unhook()
	var freed sync.Once
	free := func() { freed.Do(func() { close(release) }) }
	defer free() // a failed wait still lets the held claim finish
	claimDone := make(chan claimed, 1)
	go func() {
		c, err := store.ClaimFor(ctx, pool, store.ClaimRequest{HostID: "h1", FreeSlots: 1, Tools: []store.Tool{codex}}, time.Hour)
		claimDone <- claimed{c, err}
	}()
	<-entered
	tightened := make(chan error, 1)
	go func() {
		_, err := store.TightenRule(ctx, pool, wr("thread:s1"), "job:implementer",
			[]byte(jobText(2, rules.Tighten, candidate("exec:codex", "openai", "demoted"))))
		tightened <- err
	}()
	w.waitFor(t, "SELECT pg_advisory_xact_lock($1)", time.Now().Add(10*time.Second))
	free()
	got := <-claimDone
	ok(t, got.err)
	if got.c == nil || got.c.TaskID != task {
		t.Fatalf("the claim in flight: %+v; want %s, on what it read", got.c, task)
	}
	ok(t, <-tightened)
	if c := candidates(t, w); !strings.Contains(c, "exec:codex/openai=demoted@2") {
		t.Fatalf("after the tightening: %s", c)
	}
}

// A2: a job or capability-facts rule takes effect when written, never later.
func TestJobRuleTakesEffectNow(t *testing.T) {
	w := newWorld(t)
	s := signer{t, rptest.NewKey(t)}
	later := ruleText("job:implementer", 1, rules.Initial, "2030-01-01T00:00:00Z", "2030-06-01T00:00:00Z",
		"tenant: dev\ntext: Implement.\ncandidates:\n"+candidate("exec:codex", "openai", "active"))
	if _, err := s.sign(w, later); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("a job rule effective in 2030: %v; want refused", err)
	}
}

// A2: a job whose rule has expired has no candidates; an only fact whose
// rule has expired grants nobody (core work then waits).
func TestExpiredRulesGrantNothing(t *testing.T) {
	w := newKernelWorld(t)
	first := w.ready(t, store.Task{})
	if c := w.claim(t, "h1", time.Hour, codex); c == nil || c.TaskID != first {
		t.Fatalf("before: %+v", c)
	}
	core := w.ready(t, store.Task{Core: true})
	// A later version of the capability-facts rule that has expired: the
	// only facts bound to the rule grant nobody.
	w.legacy(t, `INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, signature, effective_at, expires_at)
	  VALUES ('capability-facts', 2, '{}', 'expired', 'tighten', 'x', '2026-01-01', '2026-02-01')`)
	if c := w.claim(t, "h2", time.Hour, claude); c != nil {
		t.Fatalf("core work %s claimed under an expired only fact", c.TaskID)
	}
	plain := w.ready(t, store.Task{})
	w.legacy(t, `INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, effective_at, expires_at)
	  VALUES ('job:test', 2, '{}', 'expired', 'tighten', '2026-01-01', '2026-02-01')`)
	for _, h := range []string{"h3", "h4"} {
		if c := w.claim(t, h, time.Hour, codex, cursor); c != nil {
			t.Fatalf("%s claimed %s after its job rule expired (core %s, plain %s)", h, c.TaskID, core, plain)
		}
	}
}

// A3: a mission's budget: attempts, wall time, its tenant's budget rule, and
// the rule's hard limit on an attempt.
func TestMissionBudget(t *testing.T) {
	for name, c := range map[string]struct {
		setup  string
		claims int
	}{
		"one attempt":            {`UPDATE agentrt.mission SET budget_json = '{"max_attempts": 1, "max_wall_hours": 48}' WHERE mission_id = 'M-1'`, 1},
		"its hours are spent":    {`UPDATE agentrt.mission SET created_at = clock_timestamp() - interval '49 hours', budget_json = '{"max_attempts": 9, "max_wall_hours": 48}' WHERE mission_id = 'M-1'`, 0},
		"no budget rule":         {`UPDATE agentrt.mission SET tenant = 'business' WHERE mission_id = 'M-1'`, 0},
		"the rule's own default": {`SELECT 1`, 2},
	} {
		t.Run(name, func(t *testing.T) {
			w := newKernelWorld(t)
			w.ready(t, store.Task{})
			w.ready(t, store.Task{})
			w.legacy(t, c.setup)
			n := 0
			for _, h := range []string{"h1", "h2"} {
				if w.claim(t, h, time.Hour, codex) != nil {
					n++
				}
			}
			if n != c.claims {
				t.Fatalf("%d claims; want %d", n, c.claims)
			}
		})
	}
	w := newKernelWorld(t)
	w.ready(t, store.Task{})
	_, err := store.TightenRule(ctx, w.rt, wr("thread:s1"), "budget:dev", []byte(ruleText("budget:dev", 2, rules.Tighten, since, until,
		"tenant: dev\nmission: {max_attempts: 1000, max_wall_hours: 720}\nattempt_hard_limit_s: 60\n")))
	ok(t, err)
	if c := w.claim(t, "h1", time.Hour, codex); c == nil || c.HardLimit != time.Minute {
		t.Fatalf("hard limit under a 60 s rule: %+v", c)
	}
}

// A3: two hosts reach the last attempt a mission's budget allows at once:
// the second waits for the first and then finds none left.
func TestLastAttemptGoesToOneHost(t *testing.T) {
	w := newKernelWorld(t)
	w.ready(t, store.Task{})
	w.ready(t, store.Task{})
	w.legacy(t, `UPDATE agentrt.mission SET budget_json = '{"max_attempts": 1, "max_wall_hours": 48}' WHERE mission_id = 'M-1'`)
	pool := w.db.Pool(t, w.db.Runtime)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unhook := store.SetHook(func(point, _ string) {
		if point == "taken" {
			once.Do(func() { close(entered); <-release })
		}
	})
	defer unhook()
	var freed sync.Once
	free := func() { freed.Do(func() { close(release) }) }
	defer free()
	claim := func(host string, out chan<- claimed) {
		c, err := store.ClaimFor(ctx, pool, store.ClaimRequest{HostID: host, FreeSlots: 1, Tools: []store.Tool{codex}}, time.Hour)
		out <- claimed{c, err}
	}
	a, b := make(chan claimed, 1), make(chan claimed, 1)
	go claim("h1", a)
	<-entered
	go claim("h2", b)
	w.waitFor(t, "COALESCE(envelope_rule_id, '') FROM agentrt.mission", time.Now().Add(10*time.Second))
	free()
	ga, gb := <-a, <-b
	ok(t, ga.err)
	ok(t, gb.err)
	if ga.c == nil || gb.c != nil {
		t.Fatalf("h1 %+v, h2 %+v; want h1 alone", ga.c, gb.c)
	}
}

// A4: tightening the standing envelope reaches missions already started
// under it: an effect above its max_action, or a mission budget above its
// own, is no longer dispatched.
func TestEnvelopeTighteningReachesStartedMissions(t *testing.T) {
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(s.sign(w, envelopeRule(1, rules.Initial, "h1, h2, h3"))))
	out, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "m", store.MissionProposal{Title: "m", Goal: "g", Success: []string{"done"},
		MaxAttempts: 10, MaxWallHours: 10})
	ok(t, err)
	mission := out["mission_id"].(string)
	if out["state"] != "active" {
		t.Fatalf("proposal: %v", out)
	}
	tighter := func(version int, maxAction string, attempts int) string {
		return ruleText("standing-envelope:dev", version, rules.Tighten, since, until, fmt.Sprintf(`tenant: dev
max_action: %s
budget: {max_attempts: %d, max_wall_hours: 48}
executors: [exec:codex]
hosts: [h1, h2, h3]
`, maxAction, attempts))
	}
	_, err = store.TightenRule(ctx, w.rt, wr("thread:s1"), "standing-envelope:dev", []byte(tighter(2, "none", 40)))
	ok(t, err)
	branch := w.ready(t, store.Task{MissionID: mission, OffPlanReason: "x", Effect: "branch"})
	if c := w.claim(t, "h1", time.Hour, codex); c != nil {
		t.Fatalf("a branch task %s claimed under an envelope of none", c.TaskID)
	}
	none := w.ready(t, store.Task{MissionID: mission, OffPlanReason: "x", Effect: "none"})
	if c := w.claim(t, "h2", time.Hour, codex); c == nil || c.TaskID != none {
		t.Fatalf("a task of no effect: %+v; want %s (branch %s waits)", c, none, branch)
	}
	_, err = store.TightenRule(ctx, w.rt, wr("thread:s1"), "standing-envelope:dev", []byte(tighter(3, "none", 5)))
	ok(t, err)
	w.ready(t, store.Task{MissionID: mission, OffPlanReason: "x", Effect: "none"})
	if c := w.claim(t, "h3", time.Hour, codex); c != nil {
		t.Fatalf("%s claimed though the mission's budget (10) is now above the envelope's (5)", c.TaskID)
	}
}

// A5: a signed rule marked tighten does not raise a candidate above the
// table either.
func TestSignedTighteningDoesNotRaise(t *testing.T) {
	w := newWorld(t)
	s := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(s.sign(w, jobText(1, rules.Initial, candidate("exec:codex", "openai", "active")))))
	w.legacy(t, `UPDATE agentrt.job_candidate SET status = 'demoted' WHERE executor_actor_id = 'exec:codex'`)
	ok(t, firstErr(s.sign(w, jobText(2, rules.Tighten, candidate("exec:codex", "openai", "probation")))))
	if got := candidates(t, w); got != "exec:codex/openai=demoted@2" {
		t.Fatalf("after a signed tightening: %s; want still demoted", got)
	}
}

// B2: a row whose columns disagree with its text is no rule; two signers of
// the same next version: one wins.
func TestRuleRowsAndVersions(t *testing.T) {
	w := newKernelWorld(t)
	text := ruleText("routing:dev", 1, rules.Initial, since, until, "tenant: dev\norder: [capability, quota]\nquota_stop_percent: 90\n")
	r, err := rules.Parse([]byte(text))
	ok(t, err)
	body, err := r.BodyJSON()
	ok(t, err)
	w.legacy(t, fmt.Sprintf(`INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, signature, effective_at, expires_at)
	  VALUES ('routing:dev', 1, %s, %s, 'initial', 'x', '%s', '2030-01-01T00:00:00Z')`, quote(string(body)), quote(text), since))
	if _, err := store.RuleInForce(ctx, w.rt, "routing:dev"); err == nil {
		t.Fatal("a rule whose expires_at column is not its text's was read")
	}
	s := signer{t, rptest.NewKey(t)}
	pool := w.db.Pool(t, w.db.Runtime)
	v2 := ruleText("budget:dev", 2, rules.Loosen, since, until, "tenant: dev\nmission: {max_attempts: 50, max_wall_hours: 50}\nattempt_hard_limit_s: 600\n")
	sig := s.key.Sign(t, []byte(v2), rules.Namespace)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = store.SignRule(ctx, pool, store.Writer{Actor: "owner", Token: "admin"}, "budget:dev", []byte(v2), sig, s.key.Fingerprint)
		}(i)
	}
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) || (!errors.Is(errs[0], store.ErrInvalid) && !errors.Is(errs[1], store.ErrInvalid)) {
		t.Fatalf("two signers of version 2: %v, %v; want one taken and one refused", errs[0], errs[1])
	}
}
