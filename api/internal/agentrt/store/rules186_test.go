package store_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// Regression tests for review 186.

// A2: a job or capability-facts rule takes effect when written, never later.
func TestJobRuleTakesEffectNow(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	s := signer{t, rptest.NewKey(t)}
	later := ruleText("job:implementer", 1, rules.Initial, "2030-01-01T00:00:00Z", "2030-06-01T00:00:00Z",
		"tenant: dev\ntext: Implement.\ncandidates:\n"+candidate("exec:codex", "openai", "active"))
	if _, err := s.sign(w, later); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("a job rule effective in 2030: %v; want refused", err)
	}
}

// A5: a signed rule marked tighten does not raise a candidate above the
// table either.
func TestSignedTighteningDoesNotRaise(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
