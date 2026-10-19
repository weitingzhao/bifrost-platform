package store_test

import (
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// Regression tests for review 187.

// R1: a control holding its writer's lock and about to take a host row, a
// rule write by the same writer, and a claim on that host: the rule write
// waits for the writer's lock without holding the capability lock, so the
// claim finishes, then the control, then the rule write. No deadlock.
//
// Serial: it holds the package's test hook (SetHook).
func TestControlRuleWriteAndClaim(t *testing.T) {
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(s.sign(w, jobText(1, rules.Initial, candidate("exec:codex", "openai", "active")))))
	task := w.ready(t, store.Task{})
	pool := w.db.Pool(t, w.db.Runtime)
	owner := store.Writer{Actor: "owner", Token: "admin"}
	entered, release := make(chan struct{}), make(chan struct{})
	var once, freed sync.Once
	unhook := store.SetHook(func(point, _ string) {
		if point == "control writer" {
			once.Do(func() { close(entered); <-release })
		}
	})
	defer unhook()
	free := func() { freed.Do(func() { close(release) }) }
	defer free()
	controlled := make(chan error, 1)
	go func() {
		_, err := store.RequestControl(ctx, pool, owner, "drain-h1", "host", "h1", "drain", "test")
		controlled <- err
	}()
	<-entered
	tightened := make(chan error, 1)
	go func() {
		_, err := store.TightenRule(ctx, pool, owner, "job:implementer", []byte(jobText(2, rules.Tighten, candidate("exec:codex", "openai", "demoted"))))
		tightened <- err
	}()
	w.waitFor(t, "pg_advisory_xact_lock(742107", time.Now().Add(10*time.Second))
	claimDone := make(chan claimed, 1)
	go func() {
		c, err := store.ClaimFor(ctx, pool, store.ClaimRequest{HostID: "h1", FreeSlots: 1, Tools: []store.Tool{codex}}, time.Hour)
		claimDone <- claimed{c, err}
	}()
	var got claimed
	select {
	case got = <-claimDone:
	case <-time.After(5 * time.Second):
		free()
		got = <-claimDone
	}
	free()
	ok(t, got.err)
	if got.c == nil || got.c.TaskID != task {
		t.Fatalf("the claim: %+v; want %s while the rule write waits for its writer", got.c, task)
	}
	ok(t, <-controlled)
	ok(t, <-tightened)
}

// R2: a job rule or a capability-facts rule whose stored text does not read
// as its row says grants nothing at the claim, whatever its dates.
func TestUnreadableRulesGrantNothing(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	core := w.ready(t, store.Task{Core: true})
	// A later capability-facts version whose body_json is not its text's.
	bad := ruleText("capability-facts", 2, rules.Tighten, since, until,
		"facts:\n  - {executor: exec:claude, kind: only, subject: author_core, scope: {core: true}}\n")
	w.legacy(t, `INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, effective_at, expires_at)
	  VALUES ('capability-facts', 2, '{"facts":[]}', `+quote(bad)+`, 'tighten', '`+since+`', '`+until+`')`)
	if c := w.claim(t, "h1", time.Hour, claude); c != nil {
		t.Fatalf("core work %s claimed under an unreadable facts rule", c.TaskID)
	}
	w.ready(t, store.Task{})
	// A later job:test version with a field the parser does not know.
	odd := ruleText("job:test", 2, rules.Tighten, since, until,
		"tenant: dev\ntext: Test fixture candidates.\ncandidates:\n  - {executor: exec:codex, provider: openai, model_pattern: \"*\", status: active}\nnote: x\n")
	w.legacy(t, `INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, effective_at, expires_at)
	  VALUES ('job:test', 2, '{}', `+quote(odd)+`, 'tighten', '`+since+`', '`+until+`')`)
	if c := w.claim(t, "h2", time.Hour, codex, cursor); c != nil {
		t.Fatalf("%s claimed under an unreadable job rule (core %s)", c.TaskID, core)
	}
}

// R3: until the Owner signs capability-facts, the seed's only facts grant
// nobody: core work waits; once he signs, it is claimed.
func TestCoreWaitsForTheSignedFacts(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	ok(t, store.RegisterHost(ctx, w.rt, "owner", "h1", "dev"))
	ok(t, store.RegisterHost(ctx, w.rt, "owner", "h2", "dev"))
	_, err := w.rt.Exec(ctx, agentrttest.DispatchRules)
	ok(t, err)
	w.jobs(t, "active", "*", claude)
	core := w.ready(t, store.Task{Core: true})
	if c := w.claim(t, "h1", time.Hour, claude); c != nil {
		t.Fatalf("core work %s claimed on the seed's unsigned facts", c.TaskID)
	}
	s := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(s.sign(w, ruleText("capability-facts", 1, rules.Initial, since, until,
		"facts:\n  - {executor: exec:claude, kind: only, subject: author_core, scope: {core: true}}\n"))))
	// The controller's reroute pass decides the task's route again.
	_, err = store.Reroute(ctx, w.rt)
	ok(t, err)
	if c := w.claim(t, "h2", time.Hour, claude); c == nil || c.TaskID != core {
		t.Fatalf("after the Owner signed: %+v; want %s", c, core)
	}
}
