package store_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// ruleText is a rule's text with its header.
func ruleText(id string, version int, direction, effective, expires, body string) string {
	return fmt.Sprintf("rule_id: %s\nrule_version: %d\ndirection: %s\neffective_at: %s\nexpires_at: %s\n%s",
		id, version, direction, effective, expires, body)
}

const (
	since = "2026-10-01T00:00:00Z"
	until = "2027-01-15T00:00:00Z"
)

// autoText is a card-auto-answer rule: the sample these tests sign, tighten
// and propose. It stands where the standing envelope stood until the
// dispatch kinds were retired (2026-11-15). strictness is how tight it is:
// the rule waits strictness*10 seconds longer than an hour less, so a higher
// number is a tighter rule; kinds are the cards it answers, and fewer is
// tighter.
func autoText(version int, direction, expires string, strictness int, kinds string) string {
	return ruleText("card-auto-answer", version, direction, since, expires, fmt.Sprintf("after_seconds: %d\nkinds: [%s]\n", 3000+strictness*10, kinds))
}

// testKey stands for a signing key. Real signatures are what the caller's
// check is about, and their tests are in agentrt/integration (TestRuleSet);
// here a signature is what checkSignature recomputes, so that it still fails
// unless the store hands the check this text, rules.Namespace and the anchor.
type testKey struct{ Fingerprint string }

var testKeys atomic.Int64

func newKey() testKey { return testKey{fmt.Sprintf("SHA256:test-key-%d", testKeys.Add(1))} }

func signatureOf(text []byte, namespace, anchor string) string {
	return fmt.Sprintf("sig:%s:%s:%x", namespace, anchor, sha256.Sum256(text))
}

func (k testKey) Sign(text []byte, namespace string) string {
	return signatureOf(text, namespace, k.Fingerprint)
}

// checkSignature is the store.SignatureCheck of these tests.
func checkSignature(armored, message []byte, namespace, anchor string) error {
	if anchor == "" || string(armored) != signatureOf(message, namespace, anchor) {
		return errors.New("not signed by the anchor")
	}
	return nil
}

// signer signs rule texts with a key the test compiles in as the anchor.
type signer struct {
	t   *testing.T
	key testKey
}

func (s signer) sign(w *world, text string) (map[string]any, error) {
	ctx := ctxOf(s.t)
	return store.SignRule(ctx, w.rt, actor.Writer{Actor: "owner", Token: "admin"}, idOf(text), []byte(text),
		s.key.Sign([]byte(text), rules.Namespace), s.key.Fingerprint, checkSignature)
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

func firstErr(_ map[string]any, err error) error { return err }

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// Check and proposals write nothing that takes effect; a proposal keeps the
// text and says whether the Owner must sign it.
func TestRuleCheckAndProposal(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	s := signer{t, newKey()}
	ok(t, firstErr(s.sign(w, autoText(1, rules.Initial, until, 20, "blocked"))))
	tighter := autoText(2, rules.Tighten, until, 40, "blocked")
	looser := autoText(2, rules.Loosen, until, 0, "blocked")
	for text, want := range map[string]bool{tighter: true, looser: false} {
		out, err := store.CheckRule(ctx, w.rt, "card-auto-answer", []byte(text))
		ok(t, err)
		if out["tightens"] != want || out["needs_signature"] == want || out["previous_version"] != 1 {
			t.Errorf("check: %v; want tightens %v", out, want)
		}
	}
	out, err := store.ProposeRule(ctx, w.rt, wr("thread:s1"), "r1", "card-auto-answer", []byte(looser), "cards should be answered sooner")
	ok(t, err)
	if out["needs_signature"] != true || out["text_artifact_id"] == "" {
		t.Fatalf("proposal: %v", out)
	}
	if r := inForce(t, w, "card-auto-answer", at("2026-11-01T00:00:00Z")); r == nil || r.Version != 1 {
		t.Fatalf("a proposal took effect: %+v", r)
	}
	if _, err := store.CheckRule(ctx, w.rt, "plan-standing", []byte(looser)); !errors.Is(err, eventlog.ErrInvalid) {
		t.Errorf("a text for another rule id: %v; want refused", err)
	}
}

// Origin: 2026-11-18, LANE-BIZ-ALIGN: SignRule stopped importing the release
// policy's verifier and takes the check from its caller, so a caller can now
// pass none. Nothing has then checked the signature, and a rule taken that way
// would be an unsigned loosening. The second call is the positive control: the
// same text and signature are taken once a check is given.
func TestSignRuleWithoutACheckIsRefused(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	key := newKey()
	text := autoText(1, rules.Initial, until, 20, "blocked")
	sig := key.Sign([]byte(text), rules.Namespace)
	owner := actor.Writer{Actor: "owner", Token: "admin"}
	before := w.footprint(t)
	if _, err := store.SignRule(ctx, w.rt, owner, "card-auto-answer", []byte(text), sig, key.Fingerprint, nil); !errors.Is(err, store.ErrUnsigned) {
		t.Fatalf("no signature check: %v; want refused as unsigned", err)
	}
	if after := w.footprint(t); after != before {
		t.Fatalf("the refusal left something: %s -> %s", before, after)
	}
	if r := inForce(t, w, "card-auto-answer", at("2026-11-01T00:00:00Z")); r != nil {
		t.Fatalf("in force after the refusal: %+v", r)
	}
	ok(t, firstErr(store.SignRule(ctx, w.rt, owner, "card-auto-answer", []byte(text), sig, key.Fingerprint, checkSignature)))
}
