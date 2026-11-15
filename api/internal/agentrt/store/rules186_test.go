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

// B2: a row whose columns disagree with its text is no rule; two signers of
// the same version: one wins.
func TestRuleRowsAndVersions(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	text := ruleText("plan-standing", 1, rules.Initial, since, until, "done_rules: true\nactive_on_lease: false\n")
	r, err := rules.Parse([]byte(text))
	ok(t, err)
	body, err := r.BodyJSON()
	ok(t, err)
	w.legacy(t, fmt.Sprintf(`INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, signature, effective_at, expires_at)
	  VALUES ('plan-standing', 1, %s, %s, 'initial', 'x', '%s', '2030-01-01T00:00:00Z')`, quote(string(body)), quote(text), since))
	if _, err := store.RuleInForce(ctx, w.rt, "plan-standing"); err == nil {
		t.Fatal("a rule whose expires_at column is not its text's was read")
	}
	s := signer{t, rptest.NewKey(t)}
	pool := w.db.Pool(t, w.db.Runtime)
	v1 := autoText(1, rules.Initial, until, 20, "blocked")
	sig := s.key.Sign(t, []byte(v1), rules.Namespace)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = store.SignRule(ctx, pool, store.Writer{Actor: "owner", Token: "admin"}, "card-auto-answer", []byte(v1), sig, s.key.Fingerprint)
		}(i)
	}
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) || (!errors.Is(errs[0], store.ErrInvalid) && !errors.Is(errs[1], store.ErrInvalid)) {
		t.Fatalf("two signers of the same version: %v, %v; want one taken and one refused", errs[0], errs[1])
	}
}
