//bifrost:owner verification

package integration_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// The rules with real signatures: the store takes its signature check from the
// caller, and here that is the one the route gives it. They left agentrt/store
// on 2026-11-18, so that the store's tests do not depend on the delivery
// domain; the store's own tests use a stand-in check.

// autoText is a card-auto-answer rule: the sample these tests sign, tighten
// and propose. It stands where the standing envelope stood until the
// dispatch kinds were retired (2026-11-15). strictness is how tight it is:
// the rule waits strictness*10 seconds longer than an hour less, so a higher
// number is a tighter rule; kinds are the cards it answers, and fewer is
// tighter.
func autoText(version int, direction, expires string, strictness int, kinds string) string {
	return ruleText("card-auto-answer", version, direction, since, expires, fmt.Sprintf("after_seconds: %d\nkinds: [%s]\n", 3000+strictness*10, kinds))
}

// signer signs rule texts with a key the test compiles in as the anchor.
type signer struct {
	t   *testing.T
	key rptest.Key
}

func (s signer) sign(w *world, text string) (map[string]any, error) {
	ctx := ctxOf(s.t)
	return store.SignRule(ctx, w.rt, actor.Writer{Actor: "owner", Token: "admin"}, idOf(text), []byte(text),
		s.key.Sign(s.t, []byte(text), rules.Namespace), s.key.Fingerprint, releasepolicy.VerifyAnchored)
}

func idOf(text string) string {
	first, _, _ := strings.Cut(text, "\n")
	return strings.TrimPrefix(first, "rule_id: ")
}

func inForce(t *testing.T, w *world, id string, at time.Time) *store.StoredRule {
	t.Helper()
	ctx := ctxOf(t)
	var r *store.StoredRule
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		r, err = store.CurrentRule(ctx, tx, id, at)
		return err
	}))
	return r
}

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// signCardAutoSeconds signs a card-auto-answer rule that waits seconds and
// answers kinds, on db.
func signCardAutoSeconds(t *testing.T, db eventlog.DB, seconds, kinds string) {
	t.Helper()
	text := ruleText(rules.KindCardAuto, 1, rules.Initial, since, until, "after_seconds: "+seconds+"\nkinds: "+kinds+"\n")
	key := rptest.NewKey(t)
	_, err := store.SignRule(ctx, db, actor.Writer{Actor: "owner", Token: "admin"}, rules.KindCardAuto, []byte(text),
		key.Sign(t, []byte(text), rules.Namespace), key.Fingerprint, releasepolicy.VerifyAnchored)
	ok(t, err)
}

// Live plan cards are asked ByRule only when an in-force rule lists their kind.
func TestCardsAreAskedByRuleWhenTheRuleAnswersThem(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	var s seenCards
	propose := func(key string) string {
		t.Helper()
		out, err := planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), key, root, "rename", []planstore.PlanChange{{Op: "retitle", Node: item, Title: key}}, s.ask)
		ok(t, err)
		id, _ := out["question_id"].(string)
		if id == "" {
			t.Fatalf("no card: %v", out)
		}
		return id
	}
	before := propose("before")
	signCardAutoSeconds(t, w.rt, "5", "[blocked]")
	unlisted := propose("unlisted")
	text := ruleText(rules.KindCardAuto, 2, rules.Loosen, since, until, "after_seconds: 5\nkinds: [blocked, plan_change]\n")
	key := rptest.NewKey(t)
	_, err := store.SignRule(ctx, w.rt, actor.Writer{Actor: "owner", Token: "admin"}, rules.KindCardAuto, []byte(text), key.Sign(t, []byte(text), rules.Namespace), key.Fingerprint, releasepolicy.VerifyAnchored)
	ok(t, err)
	listed := propose("listed")
	for id, want := range map[string]bool{before: false, unlisted: false, listed: true} {
		if got := s.asked[id].ByRule; got != want {
			t.Errorf("card %s asked ByRule %v; want %v", id, got, want)
		}
	}
}

// T35: the rule set. v4's three (a loosening without a signature is refused,
// a tightening takes effect at once, an expired rule gives way to the
// strictest default) and v5's (body_json other than the text says, marked
// tighten but loosening, signed in the release policy's namespace, a field
// the parser does not know, no trust anchor).
func TestRuleSet(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	now := at("2026-11-01T00:00:00Z")
	if r := inForce(t, w, "card-auto-answer", now); r != nil {
		t.Fatalf("a rule in force before any was signed: %+v", r)
	}
	v1 := autoText(1, rules.Initial, until, 20, "blocked, mission")
	out, err := s.sign(w, v1)
	ok(t, err)
	if out["rule_version"] != 1 || out["signed"] != true {
		t.Fatalf("signed %v", out)
	}
	if r := inForce(t, w, "card-auto-answer", now); r == nil || r.CardAuto.AfterSeconds != 3200 || r.SignedBy != s.key.Fingerprint {
		t.Fatalf("in force after signing: %+v", r)
	}
	tighten := func(text string) error {
		_, err := store.TightenRule(ctx, w.rt, wr("thread:s1"), idOf(text), []byte(text))
		return err
	}
	// A loosening without a signature, whatever it calls itself.
	for name, text := range map[string]string{
		"marked tighten, a shorter wait": autoText(2, rules.Tighten, until, 19, "blocked, mission"),
		"marked tighten, a new kind":     autoText(2, rules.Tighten, until, 20, "blocked, plan_change"),
		"marked tighten, expiring later": autoText(2, rules.Tighten, "2027-03-01T00:00:00Z", 20, "blocked"),
		"marked loosen":                  autoText(2, rules.Loosen, until, 40, "blocked"),
	} {
		if err := tighten(text); !errors.Is(err, store.ErrUnsigned) {
			t.Errorf("%s: %v; want refused as unsigned", name, err)
		}
	}
	// Signed, the label still has to be true.
	if _, err := s.sign(w, autoText(2, rules.Tighten, until, 19, "blocked")); !errors.Is(err, eventlog.ErrInvalid) {
		t.Errorf("signed, marked tighten, loosening: %v; want refused", err)
	}
	// A tightening takes effect at once, unsigned; it expires sooner, and
	// once it has, the looser version before it does not come back.
	ok(t, tighten(autoText(2, rules.Tighten, "2026-12-15T00:00:00Z", 40, "blocked")))
	if r := inForce(t, w, "card-auto-answer", now); r == nil || r.Version != 2 || r.CardAuto.AfterSeconds != 3400 || r.SignedBy != "" {
		t.Fatalf("in force after the tightening: %+v", r)
	}
	if r := inForce(t, w, "card-auto-answer", at("2026-12-20T00:00:00Z")); r != nil {
		t.Fatalf("after the tightening expired, in force: v%d; want none (the strictest default)", r.Version)
	}
	// Versions follow one another.
	for name, text := range map[string]string{
		"a version skipped": autoText(4, rules.Tighten, "2026-12-15T00:00:00Z", 50, "blocked"),
		"a version again":   autoText(2, rules.Tighten, "2026-12-15T00:00:00Z", 50, "blocked"),
		"initial again":     autoText(1, rules.Initial, until, 50, "blocked"),
	} {
		if err := tighten(text); err == nil {
			t.Errorf("%s: taken", name)
		}
	}
	// Signatures that are not the Owner's for a rule.
	v3 := autoText(3, rules.Loosen, until, 20, "blocked")
	other := rptest.NewKey(t)
	for name, c := range map[string]struct{ sig, anchor string }{
		"the release policy's namespace": {s.key.Sign(t, []byte(v3), releasepolicy.NamespacePolicy), s.key.Fingerprint},
		"the unfreeze namespace":         {s.key.Sign(t, []byte(v3), releasepolicy.NamespaceUnfreeze), s.key.Fingerprint},
		"another key":                    {other.Sign(t, []byte(v3), rules.Namespace), s.key.Fingerprint},
		"no trust anchor":                {s.key.Sign(t, []byte(v3), rules.Namespace), ""},
		"over other text":                {s.key.Sign(t, []byte(v3+"\n"), rules.Namespace), s.key.Fingerprint},
		"no signature":                   {"", s.key.Fingerprint},
	} {
		if _, err := store.SignRule(ctx, w.rt, actor.Writer{Actor: "owner", Token: "admin"}, "card-auto-answer", []byte(v3), c.sig, c.anchor, releasepolicy.VerifyAnchored); !errors.Is(err, store.ErrUnsigned) {
			t.Errorf("%s: %v; want refused as unsigned", name, err)
		}
	}
	// A field the parser does not know, signed or not.
	odd := autoText(3, rules.Loosen, until, 20, "blocked") + "approve_releases: true\n"
	if _, err := s.sign(w, odd); !errors.Is(err, eventlog.ErrInvalid) {
		t.Errorf("an unknown field: %v; want refused", err)
	}
	// A kind of card the parser does not know.
	if _, err := s.sign(w, autoText(3, rules.Loosen, until, 20, "blocked, no_such_kind")); !errors.Is(err, eventlog.ErrInvalid) {
		t.Errorf("an unknown kind: %v; want refused", err)
	}
	ok(t, firstErr(s.sign(w, v3)))
	if r := inForce(t, w, "card-auto-answer", now); r == nil || r.Version != 3 || r.Direction != rules.Loosen {
		t.Fatalf("in force after the signed loosening: %+v", r)
	}
	// A row whose body_json is not what its text says is no rule.
	bad := autoText(4, rules.Tighten, until, 30, "blocked")
	w.legacy(t, fmt.Sprintf(`INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, effective_at, expires_at)
	  VALUES ('card-auto-answer', 4, '{"after_seconds":1}', %s, 'tighten', '%s', '%s')`, quote(bad), since, until))
	if err := eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.CurrentRule(ctx, tx, "card-auto-answer", now)
		return err
	}); err == nil {
		t.Fatal("a rule whose body_json disagrees with its text was read")
	}
}

func firstErr(_ map[string]any, err error) error { return err }

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
